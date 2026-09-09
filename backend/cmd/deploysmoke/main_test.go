package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSmokeConfigFromEnv_Defaults(t *testing.T) {
	cfg := smokeConfigFromEnv(func(string) string { return "" })
	if cfg != defaultSmokeConfig {
		t.Errorf("smokeConfigFromEnv with no overrides = %+v, want %+v", cfg, defaultSmokeConfig)
	}
}

func TestSmokeConfigFromEnv_Override(t *testing.T) {
	cfg := smokeConfigFromEnv(func(k string) string {
		if k == "DEPLOYSMOKE_BASE_URL" {
			return "http://example.test:9000/"
		}
		return ""
	})
	if cfg.baseURL != "http://example.test:9000" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", cfg.baseURL)
	}
}

func TestEmbeddedPNGIsDecodable(t *testing.T) {
	img, format, err := image.Decode(bytes.NewReader(smokePNG))
	if err != nil {
		t.Fatalf("embedded smokePNG does not decode: %v", err)
	}
	if format != "png" {
		t.Errorf("embedded image format = %q, want png", format)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("embedded image bounds = %v, want 64x64", b)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate([]byte("short"), 10); got != "short" {
		t.Errorf("truncate(short, 10) = %q, want unchanged", got)
	}
	if got := truncate(bytes.Repeat([]byte("a"), 20), 5); got != "aaaaa..." {
		t.Errorf("truncate(20a, 5) = %q, want %q", got, "aaaaa...")
	}
}

func TestPostMultipart_BuildsWellFormedBody(t *testing.T) {
	var gotFilename string
	var gotContent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		f, hdr, err := req.FormFile("file")
		if err != nil {
			t.Errorf("server could not read multipart file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer f.Close()
		gotFilename = hdr.Filename
		gotContent, _ = io.ReadAll(f)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	r := &smokeRun{client: srv.Client(), baseURL: srv.URL}
	body, err := r.postMultipart("/", "file", "x.txt", []byte("hello bytes"), "", http.StatusCreated)
	if err != nil {
		t.Fatalf("postMultipart: %v (%s)", err, body)
	}
	if gotFilename != "x.txt" || string(gotContent) != "hello bytes" {
		t.Errorf("server saw filename=%q content=%q", gotFilename, gotContent)
	}
}

// stubServer fakes just enough of the running instance for run() to walk
// every workflow step. The zero fault ("") is the fully healthy install the
// happy-path test expects; any other value makes exactly one step's response
// wrong so the matching negative test can assert run() fails at that step.
type stubServer struct {
	t           *testing.T
	fault       string
	contactPOST int // POST /api/v1/contacts call counter (first = rich contact, second = related)
	attachment  []byte
}

func newStubServer(t *testing.T, fault string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(&stubServer{t: t, fault: fault})
}

func (s *stubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/health/live":
		s.simpleHealth(w, "live-code", "live-garbage", "live-status", `{"status":"live"}`, `{"status":"dead"}`)
	case r.Method == http.MethodGet && p == "/health/ready":
		s.ready(w)
	case r.Method == http.MethodGet && p == "/health":
		switch s.fault {
		case "deep-code":
			w.WriteHeader(http.StatusInternalServerError)
		case "deep-garbage":
			_, _ = w.Write([]byte("not json"))
		case "deep-status":
			_, _ = w.Write([]byte(`{"status":"unhealthy"}`))
		case "deep-degraded":
			_, _ = w.Write([]byte(`{"status":"degraded"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		}
	case r.Method == http.MethodPost && p == "/api/v1/register":
		if s.fault == "register-code" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"username taken"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodPost && p == "/api/v1/login":
		s.login(w)
	case r.Method == http.MethodPost && p == "/api/v1/contacts":
		s.createContact(w)
	case r.Method == http.MethodGet && strings.HasSuffix(p, "/profile_picture"):
		s.getPhoto(w)
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/profile_picture"):
		if s.fault == "photo-code" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/attachments"):
		s.postAttachment(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/v1/attachments/") && strings.HasSuffix(p, "/download"):
		s.getAttachment(w)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/v1/contacts/"):
		s.refetch(w)
	case r.Method == http.MethodPost && p == "/api/v1/relationship-edges":
		if s.fault == "relate-edge-code" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodGet && p == "/api/v1/search":
		s.search(w)
	case r.Method == http.MethodGet && p == "/api/v1/export/vcf":
		s.export(w, "export-vcf-code", "export-vcf-noname", "BEGIN:VCARD\nFN:"+smokeGiven+" "+smokeSurname+"\nEND:VCARD\n")
	case r.Method == http.MethodGet && p == "/api/v1/export/jscontact":
		switch s.fault {
		case "export-jscontact-code":
			w.WriteHeader(http.StatusInternalServerError)
		case "export-jscontact-invalid":
			_, _ = w.Write([]byte("{not json"))
		case "export-jscontact-noname":
			_, _ = w.Write([]byte(`{"name":{"full":"Someone Else"}}`))
		default:
			_, _ = w.Write([]byte(`{"name":{"full":"` + smokeGiven + " " + smokeSurname + `"}}`))
		}
	case r.Method == http.MethodGet && p == "/api/v1/export":
		s.export(w, "export-bundle-code", "export-bundle-noname", "=== CONTACTS ===\nID,Lastname\n1,"+smokeSurname+"\n")
	case r.Method == http.MethodGet && p == "/.well-known/carddav":
		s.wellKnown(w, "/carddav/")
	case r.Method == http.MethodGet && p == "/.well-known/caldav":
		s.wellKnown(w, "/caldav/")
	default:
		s.t.Errorf("stub: unexpected request %s %s", r.Method, p)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *stubServer) simpleHealth(w http.ResponseWriter, codeFault, garbageFault, statusFault, ok, bad string) {
	switch s.fault {
	case codeFault:
		w.WriteHeader(http.StatusInternalServerError)
	case garbageFault:
		_, _ = w.Write([]byte("not json"))
	case statusFault:
		_, _ = w.Write([]byte(bad))
	default:
		_, _ = w.Write([]byte(ok))
	}
}

func (s *stubServer) ready(w http.ResponseWriter) {
	switch s.fault {
	case "ready-code":
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready","checks":{}}`))
	case "ready-garbage":
		_, _ = w.Write([]byte("not json"))
	case "ready-missing-facet":
		_, _ = w.Write([]byte(`{"status":"ready","checks":{"database":{"status":"ok"},"migrations":{"status":"ok"}}}`))
	case "ready-facet-migrations":
		_, _ = w.Write([]byte(`{"status":"not_ready","checks":{"database":{"status":"ok"},"migrations":{"status":"failed","reason":"no migrations have been applied"},"filesystem":{"status":"ok"}}}`))
	default:
		_, _ = w.Write([]byte(`{"status":"ready","checks":{"database":{"status":"ok"},"migrations":{"status":"ok"},"filesystem":{"status":"ok"}}}`))
	}
}

func (s *stubServer) login(w http.ResponseWriter) {
	switch s.fault {
	case "login-code":
		w.WriteHeader(http.StatusUnauthorized)
	case "login-nocookie":
		w.WriteHeader(http.StatusOK)
	default:
		http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "stub-token", Path: "/"})
		w.WriteHeader(http.StatusOK)
	}
}

func (s *stubServer) createContact(w http.ResponseWriter) {
	s.contactPOST++
	first := s.contactPOST == 1
	switch {
	case first && s.fault == "create-code":
		w.WriteHeader(http.StatusInternalServerError)
	case first && s.fault == "create-garbage":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("not json"))
	case first && s.fault == "create-noid":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"contact":{}}`))
	case !first && s.fault == "relate-second-code":
		w.WriteHeader(http.StatusInternalServerError)
	case !first && s.fault == "relate-second-nouid":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"contact":{"id":2}}`))
	default:
		w.WriteHeader(http.StatusCreated)
		id := int64(s.contactPOST)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"contact":{"id":%d,"uid":"uid-%d"}}`, id, id)))
	}
}

func (s *stubServer) postAttachment(w http.ResponseWriter, r *http.Request) {
	switch s.fault {
	case "attach-code":
		w.WriteHeader(http.StatusInternalServerError)
		return
	case "attach-noid":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"attachment":{}}`))
		return
	}
	if f, _, err := r.FormFile("file"); err == nil {
		s.attachment, _ = io.ReadAll(f)
		_ = f.Close()
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"attachment":{"id":7}}`))
}

func (s *stubServer) getAttachment(w http.ResponseWriter) {
	switch s.fault {
	case "attach-download-code":
		w.WriteHeader(http.StatusInternalServerError)
	case "attach-mismatch":
		_, _ = w.Write([]byte("different bytes"))
	default:
		_, _ = w.Write(s.attachment)
	}
}

func (s *stubServer) getPhoto(w http.ResponseWriter) {
	switch s.fault {
	case "photo-download-code":
		w.WriteHeader(http.StatusInternalServerError)
	case "photo-notimage":
		_, _ = w.Write([]byte("this is not an image at all"))
	default:
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(smokePNG)
	}
}

func (s *stubServer) search(w http.ResponseWriter) {
	switch s.fault {
	case "search-code":
		w.WriteHeader(http.StatusInternalServerError)
	case "search-garbage":
		_, _ = w.Write([]byte("not json"))
	case "search-nomatch":
		_, _ = w.Write([]byte(`{"contacts":[]}`))
	default:
		_, _ = w.Write([]byte(`{"contacts":[{"id":1}]}`))
	}
}

func (s *stubServer) export(w http.ResponseWriter, codeFault, nonameFault, okBody string) {
	switch s.fault {
	case codeFault:
		w.WriteHeader(http.StatusInternalServerError)
	case nonameFault:
		_, _ = w.Write([]byte("no matching name here"))
	default:
		_, _ = w.Write([]byte(okBody))
	}
}

// wellKnown models an nginx .well-known discovery 301 (issue #865). The happy
// path emits a relative Location; the faults model the internal-port leak and
// a non-redirect response.
func (s *stubServer) wellKnown(w http.ResponseWriter, target string) {
	switch s.fault {
	case "wellknown-port-leak":
		w.Header().Set("Location", "http://localhost:8080"+target)
		w.WriteHeader(http.StatusMovedPermanently)
	case "wellknown-not-301":
		w.Header().Set("Location", target)
		w.WriteHeader(http.StatusOK)
	default:
		w.Header().Set("Location", target)
		w.WriteHeader(http.StatusMovedPermanently)
	}
}

func (s *stubServer) refetch(w http.ResponseWriter) {
	switch s.fault {
	case "refetch-code":
		w.WriteHeader(http.StatusInternalServerError)
		return
	case "refetch-garbage":
		_, _ = w.Write([]byte("not json"))
		return
	}
	card := map[string]any{
		"name":          map[string]any{"components": []component{{"given", smokeGiven}, {"surname", smokeSurname}}},
		"emails":        []map[string]any{{"address": smokeEmail}},
		"phones":        []map[string]any{{"number": smokePhone}},
		"addresses":     []map[string]any{{"components": []component{{"locality", smokeLocality}}}},
		"anniversaries": []map[string]any{{"kind": "birth"}},
	}
	switch s.fault {
	case "refetch-noname":
		card["name"] = map[string]any{"components": []component{}}
	case "refetch-noemail":
		card["emails"] = []map[string]any{}
	case "refetch-nophone":
		card["phones"] = []map[string]any{}
	case "refetch-noaddress":
		card["addresses"] = []map[string]any{}
	case "refetch-noanniversary":
		card["anniversaries"] = []map[string]any{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"card": card})
}

func TestRun_HappyPath(t *testing.T) {
	srv := newStubServer(t, "")
	defer srv.Close()
	if err := run(smokeConfig{baseURL: srv.URL}); err != nil {
		t.Fatalf("run against a healthy stub install failed: %v", err)
	}
}

func TestRun_DegradedDeepHealthStillPasses(t *testing.T) {
	// A brand-new install commonly reports /health = "degraded" (e.g. the
	// restore drill has no prior backup yet). It is still 200 and the CRM is
	// usable, so the smoke run must not fail on it.
	srv := newStubServer(t, "deep-degraded")
	defer srv.Close()
	if err := run(smokeConfig{baseURL: srv.URL}); err != nil {
		t.Fatalf("deep health = degraded on a fresh install must still pass: %v", err)
	}
}

func TestRun_StepFailures(t *testing.T) {
	cases := []struct {
		fault    string
		wantStep string
	}{
		{"live-status", "health"},
		{"live-code", "health"},
		{"live-garbage", "health"},
		{"ready-code", "health"},
		{"ready-garbage", "health"},
		{"ready-missing-facet", "health"},
		{"ready-facet-migrations", "health"},
		{"deep-status", "health"},
		{"deep-code", "health"},
		{"deep-garbage", "health"},
		{"register-code", "register-first-user"},
		{"login-code", "login"},
		{"login-nocookie", "login"},
		{"create-code", "create-contact"},
		{"create-garbage", "create-contact"},
		{"create-noid", "create-contact"},
		{"relate-second-code", "relate-contact"},
		{"relate-second-nouid", "relate-contact"},
		{"relate-edge-code", "relate-contact"},
		{"attach-code", "attach-file"},
		{"attach-noid", "attach-file"},
		{"attach-download-code", "attach-file"},
		{"attach-mismatch", "attach-file"},
		{"photo-code", "upload-photo"},
		{"photo-download-code", "upload-photo"},
		{"photo-notimage", "upload-photo"},
		{"search-code", "search-contact"},
		{"search-garbage", "search-contact"},
		{"search-nomatch", "search-contact"},
		{"export-vcf-code", "export"},
		{"export-vcf-noname", "export"},
		{"export-jscontact-code", "export"},
		{"export-jscontact-invalid", "export"},
		{"export-jscontact-noname", "export"},
		{"export-bundle-code", "export"},
		{"export-bundle-noname", "export"},
		{"wellknown-port-leak", "wellknown-discovery"},
		{"wellknown-not-301", "wellknown-discovery"},
		{"refetch-code", "refetch-fields"},
		{"refetch-garbage", "refetch-fields"},
		{"refetch-noname", "refetch-fields"},
		{"refetch-noemail", "refetch-fields"},
		{"refetch-nophone", "refetch-fields"},
		{"refetch-noaddress", "refetch-fields"},
		{"refetch-noanniversary", "refetch-fields"},
	}
	for _, c := range cases {
		t.Run(c.fault, func(t *testing.T) {
			srv := newStubServer(t, c.fault)
			defer srv.Close()
			err := run(smokeConfig{baseURL: srv.URL})
			if err == nil {
				t.Fatalf("fault %q: run() returned nil, want an error", c.fault)
			}
			if !strings.HasPrefix(err.Error(), c.wantStep+":") {
				t.Errorf("fault %q: error %q does not start with step %q", c.fault, err.Error(), c.wantStep)
			}
		})
	}
}

func TestRun_ConnectionRefused(t *testing.T) {
	srv := newStubServer(t, "")
	deadURL := srv.URL
	srv.Close() // nothing listening now

	err := run(smokeConfig{baseURL: deadURL})
	if err == nil || !strings.HasPrefix(err.Error(), "health:") {
		t.Fatalf("run() against a dead server = %v, want a health-step transport error", err)
	}
}
