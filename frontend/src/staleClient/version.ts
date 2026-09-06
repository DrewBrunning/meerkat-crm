// Client build identity + version comparison (issue #475, WEB-01).
//
// The web client is served by the same release as the API (one all-in-one
// deployment), so a tab's *own* release version can be stamped at build time
// and compared against what the server's /health now advertises. That is what
// turns "the server moved on while this tab stayed open" into an actionable
// signal:
//
//   - server version newer than this bundle  -> a deploy happened mid-session;
//     the service-worker update prompt offers the reload (backstop to #476).
//   - this bundle below the server's declared `min_client_version`, or the
//     bundle speaking a different `api_contract_version` -> incompatible;
//     reload is required, not offered.
//
// import.meta.env?.VITE_APP_VERSION is read *guarded*, exactly the way
// auth.ts reads VITE_API_URL: Vite injects import.meta.env in the browser and
// leaves it undefined in Node (the e2e harness imports these modules), and
// either way this must not crash. A dev / test build that was not stamped
// leaves the version empty, which disables the whole stale-client mechanism —
// the same production-only posture serviceWorkerRegistration.register() takes.
const rawVersion: string = (import.meta.env?.VITE_APP_VERSION || '').trim();

// Mutable so tests can stamp a build identity without a real vite build.
let clientVersion = rawVersion;

// The API contract generation THIS BUNDLE was compiled against. "v1" while
// the route table is on /api/v1; it is embedded next to the version so a
// future server that announces contract "v2" can be recognized as
// incompatible with this bundle (docs/client-compatibility-policy.md).
const clientContractVersion = 'v1';

export function getClientVersion(): string {
  return clientVersion;
}

/** False in dev/test builds (VITE_APP_VERSION unset) — the detector is inert. */
export function isClientVersionStamped(): boolean {
  return clientVersion !== '';
}

export function getClientContractVersion(): string {
  return clientContractVersion;
}

// --- Version parsing and comparison ---------------------------------------
//
// Mirrors the versionName shapes backend/config/config.go accepts for
// MIN_CLIENT_VERSION so both sides agree on what is comparable: major,
// major.minor, or major.minor.patch, optional `-prerelease` / `+build`
// suffix, optional leading `v`. Precedence follows semver: numeric fields
// first, then prerelease (absent > present; numeric identifiers sort below
// alphanumeric; a shorter identifier list sorts below a longer one on equal
// prefixes). Build metadata is ignored for ordering.

export interface ParsedClientVersion {
  major: number;
  minor: number;
  patch: number;
  prerelease: readonly string[];
}

// Version shapes are validated against this exact pattern on the backend too
// (backend/config/config.go), which is what makes "this is a version" a
// two-sided agreement rather than a client invention. eslint-disable for
// security/detect-unsafe-regex: inputs are short, operator/CI-supplied
// version strings (never unbounded user text), and the pattern is anchored,
// so this is not a ReDoS vector — same reasoning as the accepted localhost
// pattern in serviceWorkerRegistration.ts.
// eslint-disable-next-line security/detect-unsafe-regex
const VERSION_PATTERN = /^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-([0-9A-Za-z][0-9A-Za-z.-]*))?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?$/;

export function parseClientVersion(input: string): ParsedClientVersion | null {
  const match = VERSION_PATTERN.exec(input.trim());
  if (!match) {
    return null;
  }
  return {
    major: Number(match[1]),
    minor: match[2] === undefined ? 0 : Number(match[2]),
    patch: match[3] === undefined ? 0 : Number(match[3]),
    prerelease: match[4] === undefined ? [] : match[4].split('.'),
  };
}

function comparePrerelease(a: readonly string[], b: readonly string[]): number {
  // A version without a prerelease outranks one with (semver 11.4.2).
  if (a.length === 0 && b.length === 0) return 0;
  if (a.length === 0) return 1;
  if (b.length === 0) return -1;

  const length = Math.min(a.length, b.length);
  for (let i = 0; i < length; i += 1) {
    const x = a[i];
    const y = b[i];
    const xNum = /^\d+$/.test(x);
    const yNum = /^\d+$/.test(y);
    if (xNum && yNum) {
      const diff = Number(x) - Number(y);
      if (diff !== 0) return diff < 0 ? -1 : 1;
    } else if (xNum) {
      // Numeric identifiers always sort lower than alphanumeric ones.
      return -1;
    } else if (yNum) {
      return 1;
    } else if (x !== y) {
      return x < y ? -1 : 1;
    }
  }
  if (a.length !== b.length) {
    return a.length < b.length ? -1 : 1;
  }
  return 0;
}

/** Returns -1, 0 or 1 comparing `a` against `b`. Unparseable input returns null. */
export function compareClientVersions(a: string, b: string): -1 | 0 | 1 | null {
  const pa = parseClientVersion(a);
  const pb = parseClientVersion(b);
  if (!pa || !pb) {
    return null;
  }

  if (pa.major !== pb.major) return pa.major < pb.major ? -1 : 1;
  if (pa.minor !== pb.minor) return pa.minor < pb.minor ? -1 : 1;
  if (pa.patch !== pb.patch) return pa.patch < pb.patch ? -1 : 1;
  const pre = comparePrerelease(pa.prerelease, pb.prerelease);
  return pre === 0 ? 0 : pre < 0 ? -1 : 1;
}

// --- Test seam -------------------------------------------------------------

export function setClientBuildIdentityForTest(version: string): void {
  clientVersion = version;
}

export function resetClientBuildIdentityForTest(): void {
  clientVersion = rawVersion;
}
