package largedata

import (
	"fmt"

	"mycorrhizal/internal/canonicalfixture"

	"gorm.io/gorm"
)

// Representative benchmark-dataset profiles (PERF-01, issue #468).
//
// Every performance ticket in milestone v0.6.9 — #469 (core-operation
// benchmarks), #470 (data-movement benchmarks), #471 (budgets), #495
// (large-dataset migration), #498 (constrained-resource capacity) and #453
// (restore at scale) — runs against one shared, *representative* dataset. A
// benchmark over a thin flat dataset measures nothing that matters here: the
// operations that get slow are relationship traversal on a dense graph, FTS
// over a large corpus, and export over contacts with many repeatable fields,
// none of which appear in a thin dataset.
//
// A Profile is a written-down shape, not an adjective. It layers three things
// on top of the block-replicated canonical manifest (Scale, largedata.go):
//
//  1. a target contact count and a user count — multi-user is supported at
//     1.0.0 (#558), and per-user jobs/indexes scale with user count
//     independently of per-user data volume;
//  2. a NON-UNIFORM graph — Scale alone yields a disconnected forest of
//     identical 27-contact islands, but traversal cost lives in the tails, so
//     each profile adds a deep cross-block relationship chain and a set of
//     dense hub contacts;
//  3. an explicit seed, so a regression is reproducible and two runs are
//     byte-for-byte comparable.
//
// The pathological records the canonical manifest carries (the soft-deleted
// `gina` + `julie`-recreates-her-uid pair, the ~1700-char note, the Unicode
// and duplicate-detection data, the sensitive rows) are present in every
// block at every scale — a slow query is usually the one hitting the
// pathological record, not the average one.
//
// The catalogued numbers and their rationale are published for operators in
// docs/development/scale-profiles.md.

// Profile is one named benchmark-dataset shape.
type Profile struct {
	// Name is the stable identifier used by `migratebench seed --profile` and
	// the docs table.
	Name string
	// Seed namespaces every regenerated contact UID. Same (Name, Seed) ⇒
	// byte-identical manifests ⇒ comparable benchmark runs; a different Seed
	// yields a different but equally deterministic dataset.
	Seed int64
	// Contacts is the per-user target, rounded up to a whole manifest block
	// (27 contacts since the I18N-01 records landed, issue #484) by
	// UserManifests. The catalogue below picks targets that ARE whole blocks,
	// so the round-up is exact and tests can assert
	// ContactCount == Contacts x Users.
	Contacts int
	// Users is how many isolated user accounts the dataset spans. Each gets
	// the full per-user Contacts count and its own re-keyed rows.
	Users int
	// Hubs is the number of dense hub contacts — nodes with an out-degree far
	// above the uniform block average, where traversal cost concentrates.
	Hubs int
	// HubFanout is how many extra edges each hub gets (to other blocks' lead
	// contacts). Clamped to blocks-1.
	HubFanout int
	// ChainDepth is the length of the single deep cross-block relationship
	// chain (lead contact of block i → block i+1, `parent_of`). Clamped to
	// blocks-1. This is the multi-hop traversal path #469 measures.
	ChainDepth int
}

// The catalogue. Keep it short — two or three is enough (#468). Smoke is the
// extra fast one the default `go test` suite runs every PR; the other three
// are the documented profiles and run only under MYCORRHIZAL_LARGE_TESTS=1.
var (
	// Smoke is a deliberately tiny profile — big enough to exercise the graph
	// shape, multi-user split, and every pathological record, small enough to
	// populate in seconds under -race on every PR.
	Smoke = Profile{Name: "smoke", Seed: 1, Contacts: 162, Users: 2, Hubs: 2, HubFanout: 8, ChainDepth: 6}

	// Typical is what a real personal-CRM user has after a few years: a few
	// hundred contacts, one account, a handful of well-connected people.
	Typical = Profile{Name: "typical", Seed: 1, Contacts: 918, Users: 1, Hubs: 5, HubFanout: 25, ChainDepth: 12}

	// Large is a heavy user, or someone who imported an entire address-book
	// history, on a small shared instance: tens of thousands of contacts
	// across a few accounts.
	Large = Profile{Name: "large", Seed: 1, Contacts: 15012, Users: 3, Hubs: 25, HubFanout: 150, ChainDepth: 40}

	// Stress is beyond the intended MVP scale — its job is to find the cliff,
	// not to promise support: ~100k contacts per user across ten accounts.
	Stress = Profile{Name: "stress", Seed: 1, Contacts: 100008, Users: 10, Hubs: 60, HubFanout: 400, ChainDepth: 80}
)

// Profiles returns the catalogue in ascending size order.
func Profiles() []Profile {
	return []Profile{Smoke, Typical, Large, Stress}
}

// ProfileByName looks a profile up by its Name.
func ProfileByName(name string) (Profile, bool) {
	for _, p := range Profiles() {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// blocksFor is the whole number of manifest blocks a per-user Contacts target
// rounds up to.
func (p Profile) blocksFor(base *canonicalfixture.Manifest) int {
	return (p.Contacts + len(base.Contacts) - 1) / len(base.Contacts)
}

// ProfileDataset is what Populate returns: the profile it built and one
// populated canonicalfixture.Dataset per user, in user order. Every created
// row (contacts keyed by manifest name, notes, edges, …) hangs off a
// per-user Dataset; tombstoned rows included.
type ProfileDataset struct {
	Profile Profile
	Users   []canonicalfixture.Dataset
}

// ContactCount is the total live+tombstoned contacts populated across all
// users.
func (d *ProfileDataset) ContactCount() int {
	n := 0
	for i := range d.Users {
		n += len(d.Users[i].Contacts)
	}
	return n
}

// UserManifests builds the profile's per-user manifests up front. It is the
// explicit "hand me the manifests" API; Populate does NOT call it (it streams
// one user at a time so peak memory is a single user's manifest even for the
// stress profile). Prefer a small profile or a single user when calling this
// directly.
func (p Profile) UserManifests(base *canonicalfixture.Manifest) ([]*canonicalfixture.Manifest, error) {
	if err := p.validate(base); err != nil {
		return nil, err
	}
	out := make([]*canonicalfixture.Manifest, 0, p.Users)
	for u := 0; u < p.Users; u++ {
		m, err := p.userManifest(base, u)
		if err != nil { // # pragma: no cover — validate() already rejected the inputs that could make userManifest fail
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (p Profile) validate(base *canonicalfixture.Manifest) error {
	if base == nil {
		return fmt.Errorf("largedata: nil base manifest")
	}
	if len(base.Contacts) == 0 {
		return fmt.Errorf("largedata: base manifest declares no contacts to scale")
	}
	if p.Users < 1 {
		return fmt.Errorf("largedata: profile %q has Users=%d, want >= 1", p.Name, p.Users)
	}
	if p.Contacts < MinContacts {
		return fmt.Errorf("largedata: profile %q has Contacts=%d, want >= %d", p.Name, p.Contacts, MinContacts)
	}
	return nil
}

// userManifest block-scales base to the profile's per-user contact count for
// one user, namespacing that user's regenerated card UIDs by
// (Name, Seed, userIndex) and re-keying its manifest user to a unique
// username/email, then appends the profile's hub and chain relationship rows.
// The result passes canonicalfixture.Manifest.Validate and populates through
// canonicalfixture.Populate unchanged.
func (p Profile) userManifest(base *canonicalfixture.Manifest, u int) (*canonicalfixture.Manifest, error) {
	salt := fmt.Sprintf("perf/%s/%d/u%d", p.Name, p.Seed, u)
	m, err := scaleSalted(base, p.Contacts, salt)
	if err != nil { // # pragma: no cover — a positive Contacts and a non-empty base cannot fail scaleSalted
		return nil, err
	}
	m.User = canonicalfixture.ManifestUser{
		Username: fmt.Sprintf("%s_u%06d", base.User.Username, u),
		Email:    fmt.Sprintf("u%06d.%s", u, base.User.Email),
	}
	m.Description = fmt.Sprintf(
		"PERF-01 (issue #468) %q profile, user %d/%d: canonical TEST-02 manifest scaled to %d contacts + %d hub / depth-%d chain graph edges. Generated by internal/largedata.Profile.",
		p.Name, u+1, p.Users, len(m.Contacts), p.Hubs, p.ChainDepth)
	appendGraphShape(m, base, p)
	if err := m.Validate(); err != nil { // # pragma: no cover — a scaled manifest whose only extra edges reference in-manifest lead contacts always validates; TestProfileUserManifestsValidate asserts it
		return nil, fmt.Errorf("largedata: profile %q user %d manifest invalid: %w", p.Name, u, err)
	}
	return m, nil
}

// leadName is the re-keyed name of a block's lead contact (base contact index
// 0 — `ada` in the canonical manifest, which is live: never the soft-deleted
// gina/julie pair).
func leadName(base *canonicalfixture.Manifest, block int) string {
	return fmt.Sprintf("%s_%06d", base.Contacts[0].Name, block)
}

// appendGraphShape adds the profile's non-uniform graph to an already-scaled
// per-user manifest: one deep `parent_of` chain threading consecutive blocks'
// lead contacts, and a set of `friend_of` hubs each fanning out to a run of
// later blocks' lead contacts. Both relation types are registry tokens so the
// edges pass DB-01's INV-D2 check; both endpoints are live lead contacts so
// they pass INV-D1/D7. Selection is pure index arithmetic — deterministic, no
// RNG — so the shaped manifest stays byte-stable for a given profile.
func appendGraphShape(m, base *canonicalfixture.Manifest, p Profile) {
	blocks := p.blocksFor(base)
	if blocks < 2 {
		return // a one-block dataset has no cross-block edges to add
	}

	// The chain: lead(0) --parent_of--> lead(1) --parent_of--> ... . Depth is
	// clamped so the chain never runs past the last block.
	depth := p.ChainDepth
	if depth > blocks-1 {
		depth = blocks - 1
	}
	for i := 0; i < depth; i++ {
		m.Relationships = append(m.Relationships, canonicalfixture.RelationshipEntry{
			Source:  leadName(base, i),
			Target:  leadName(base, i+1),
			Type:    "parent_of",
			Comment: "PERF-01 graph shape: deep cross-block traversal chain",
		})
	}

	// The hubs: spread p.Hubs hub blocks evenly across the dataset, each
	// fanning out `fanout` `friend_of` edges to the immediately following
	// blocks' lead contacts (wrapping so a hub near the end still gets its
	// full degree).
	fanout := p.HubFanout
	if fanout > blocks-1 {
		fanout = blocks - 1
	}
	hubs := p.Hubs
	if hubs > blocks {
		hubs = blocks
	}
	for h := 0; h < hubs; h++ {
		hubBlock := (h * blocks) / hubs // hubs >= 1 here (loop guard)
		for k := 1; k <= fanout; k++ {
			target := (hubBlock + k) % blocks
			if target == hubBlock {
				continue // # pragma: no cover — fanout <= blocks-1 keeps the wrap off the hub itself
			}
			m.Relationships = append(m.Relationships, canonicalfixture.RelationshipEntry{
				Source:  leadName(base, hubBlock),
				Target:  leadName(base, target),
				Type:    "friend_of",
				Comment: "PERF-01 graph shape: dense hub",
			})
		}
	}
}

// Populate loads every user's manifest for profile p into db, which MUST be a
// real migrated schema (database.InitDB / internal/dbtest — CLAUDE.md backend
// trap #1). Each user is populated in its own transaction through
// canonicalfixture.Populate, i.e. models.ApplyRecordToContact (trap #2), so
// the dataset exercises exactly the code paths the REST API does. Peak memory
// is a single user's scaled manifest — each is built, populated, and released
// before the next — so even the stress profile does not hold every user's
// manifest at once.
func Populate(db *gorm.DB, base *canonicalfixture.Manifest, p Profile) (*ProfileDataset, error) {
	if db == nil {
		return nil, fmt.Errorf("largedata: nil db")
	}
	if err := p.validate(base); err != nil {
		return nil, err
	}
	ds := &ProfileDataset{Profile: p, Users: make([]canonicalfixture.Dataset, 0, p.Users)}
	for u := 0; u < p.Users; u++ {
		m, err := p.userManifest(base, u)
		if err != nil { // # pragma: no cover — validate() already rejected the inputs that could make userManifest fail
			return nil, err
		}
		userDS, err := canonicalfixture.Populate(db, m)
		if err != nil { // # pragma: no cover — a validated scaled manifest always populates a real migrated schema; canonicalfixture's own tests exercise its failure vocabulary
			return nil, fmt.Errorf("largedata: profile %q populating user %d: %w", p.Name, u, err)
		}
		ds.Users = append(ds.Users, *userDS)
	}
	return ds, nil
}
