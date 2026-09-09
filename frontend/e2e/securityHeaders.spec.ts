import { test as base, expect } from '@playwright/test';
import { test as a11yTest } from './fixtures';
import { API_BASE_URL } from './global-setup';

// Issue #374 — security headers are set in two places that must stay in sync:
// backend/middleware/security_headers.go (API) and docker/nginx.conf (SPA),
// where nginx repeats the same `add_header` block in every `location` because
// it does not inherit server-level `add_header` once a location defines its
// own (see the comment at docker/nginx.conf's static-asset location). That
// repetition is exactly where a header silently drifts or gets dropped on one
// route but not another, and nothing before this asserted the *real served
// responses* carried them.
//
// Runs against the all-in-one image via docker-compose.test.yml (nginx on
// 7300, see playwright.config.ts's baseURL) -- the same real nginx path a
// deployment actually serves -- not `yarn start`, which never touches
// docker/nginx.conf at all. Removing an `add_header` line from any of the
// `location` blocks this spec hits should fail it.

const NGINX_SECURITY_HEADERS = {
  'x-frame-options': 'DENY',
  'x-content-type-options': 'nosniff',
  'referrer-policy': 'strict-origin-when-cross-origin',
  // Tuned for the SPA (MUI's inline styles, self-hosted fonts, ...) --
  // deliberately looser than the API's CSP below. See the doc comment on
  // contentSecurityPolicy in backend/middleware/security_headers.go for why
  // the two are allowed to differ.
  'content-security-policy':
    "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none';",
  'permissions-policy': 'camera=(), microphone=(), geolocation=(), interest-cohort=()',
} as const;

// The Go API process never renders an active document, so its CSP is
// maximally restrictive rather than mirroring the SPA's -- see
// contentSecurityPolicy's doc comment in
// backend/middleware/security_headers.go.
const API_SECURITY_HEADERS = {
  'x-frame-options': 'DENY',
  'x-content-type-options': 'nosniff',
  'referrer-policy': 'strict-origin-when-cross-origin',
  'content-security-policy': "default-src 'none'; frame-ancestors 'none'",
  'permissions-policy': 'camera=(), microphone=(), geolocation=(), interest-cohort=()',
} as const;

function assertHeaders(headers: Record<string, string>, expected: Record<string, string>) {
  for (const [name, value] of Object.entries(expected)) {
    expect(headers[name], `expected header ${name}`).toBe(value);
  }
}

const test = base;

test.describe('Security headers: SPA (nginx)', () => {
  test('/ carries the nginx security headers (server-level add_header)', async ({ request }) => {
    const response = await request.get('/');
    expect(response.status()).toBe(200);
    assertHeaders(response.headers(), NGINX_SECURITY_HEADERS);
  });

  test('/index.html carries the same headers via its own exact-match location', async ({
    request,
  }) => {
    // index.html has its own `location = /index.html` block (so it can also
    // force no-cache -- see docker/nginx.conf) which repeats every
    // add_header line independently of the server block above.
    const response = await request.get('/index.html');
    expect(response.status()).toBe(200);
    assertHeaders(response.headers(), NGINX_SECURITY_HEADERS);
    expect(response.headers()['cache-control']).toContain('no-cache');
  });

  test('a hashed static asset carries the same headers via the immutable-cache location', async ({
    request,
  }) => {
    // Discover a real built asset path the same way viteBuild.spec.ts does,
    // rather than hardcoding a hash that changes every build.
    const html = await (await request.get('/')).text();
    const entryMatch = html.match(/<script type="module"[^>]*src="(\/assets\/[^"]+\.js)"/);
    expect(entryMatch, 'the shell should reference a hashed module entry chunk').not.toBeNull();

    const response = await request.get(entryMatch?.[1]);
    expect(response.status()).toBe(200);
    assertHeaders(response.headers(), NGINX_SECURITY_HEADERS);
    expect(response.headers()['cache-control']).toContain('immutable');
    // Issue #872: the API's no-store must NOT bleed onto hashed static assets
    // -- they stay long-cached. The backend's Cache-Control is scoped to /api/.
    expect(response.headers()['cache-control']).not.toContain('no-store');
  });

  test("Strict-Transport-Security is absent, matching this stack's COOKIE_SECURE=off toggle", async ({
    request,
  }) => {
    // docker-compose.test.yml never sets COOKIE_SECURE, so it defaults to
    // false -- both docker/entrypoint.sh's rendering of /etc/nginx/hsts.conf
    // and the backend's SecurityHeadersMiddleware key off that same signal
    // (see docker/entrypoint.sh and backend/main.go), so absence here is the
    // correct assertion for "off", not a gap. The on/off branch logic itself
    // is pinned by TestSecurityHeadersMiddleware in
    // backend/middleware/security_headers_test.go.
    const response = await request.get('/');
    expect(response.headers()['strict-transport-security']).toBeUndefined();
  });
});

test.describe('Security headers: API (Go backend)', () => {
  test("an /api/ response carries the backend's own, stricter CSP -- not the SPA's", async ({
    request,
  }) => {
    const response = await request.get(`${API_BASE_URL}/contacts?limit=1`);
    expect(response.ok(), `contacts: ${response.status()} ${await response.text()}`).toBeTruthy();
    assertHeaders(response.headers(), API_SECURITY_HEADERS);
  });

  test('Strict-Transport-Security is absent on /api/ too', async ({ request }) => {
    const response = await request.get(`${API_BASE_URL}/contacts?limit=1`);
    expect(response.headers()['strict-transport-security']).toBeUndefined();
  });

  test('an authenticated /api/ response is Cache-Control: no-store (issue #872)', async ({
    request,
  }) => {
    // Private JSON must not be retained by a shared/intermediary cache or the
    // browser bfcache. Backend-set, scoped to the /api/ prefix -- see the
    // no-store block in backend/middleware/security_headers.go.
    const response = await request.get(`${API_BASE_URL}/contacts?limit=1`);
    expect(response.ok(), `contacts: ${response.status()} ${await response.text()}`).toBeTruthy();
    expect(response.headers()['cache-control']).toBe('no-store');
  });
});

// CSP enforcement -- proves the policy is actually applied by the browser,
// not just advertised in a header a scanner can read. Uses the a11y-wrapped
// `page` fixture from ./fixtures (needs a real navigation, unlike the
// header-only checks above which only need `request`).
a11yTest.describe('Security headers: CSP enforcement', () => {
  a11yTest(
    'blocks an injected inline script instead of merely advertising the policy',
    async ({ page }) => {
      await page.goto('/');

      // Inject an inline <script> after load (a literal one in the served HTML
      // would just fail to parse before we could observe it) and listen for
      // the browser's own securitypolicyviolation event -- the only way to
      // prove enforcement rather than presence of the header.
      const result = await page.evaluate(() => {
        return new Promise<{
          ran: boolean;
          violated: boolean;
          violatedDirective: string;
          blockedUri: string;
        }>((resolve) => {
          (window as unknown as { __cspTestInjectedRan?: boolean }).__cspTestInjectedRan = false;

          let violated = false;
          let violatedDirective = '';
          let blockedUri = '';
          const onViolation = (e: SecurityPolicyViolationEvent) => {
            violated = true;
            violatedDirective = e.violatedDirective;
            blockedUri = e.blockedURI;
          };
          window.addEventListener('securitypolicyviolation', onViolation);

          const script = document.createElement('script');
          script.textContent = 'window.__cspTestInjectedRan = true;';
          document.body.appendChild(script);

          // The violation event fires synchronously with the blocked
          // execution, but give the event loop a tick before reading state
          // back out, so this can't race the listener.
          setTimeout(() => {
            window.removeEventListener('securitypolicyviolation', onViolation);
            resolve({
              ran:
                (window as unknown as { __cspTestInjectedRan?: boolean }).__cspTestInjectedRan ===
                true,
              violated,
              violatedDirective,
              blockedUri,
            });
          }, 200);
        });
      });

      expect(result.ran, 'the injected inline script must not have executed').toBe(false);
      expect(result.violated, 'a securitypolicyviolation event should have fired').toBe(true);
      expect(result.violatedDirective).toContain('script-src');
      expect(result.blockedUri).toBe('inline');
    },
  );
});
