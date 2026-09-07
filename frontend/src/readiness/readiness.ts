// Server-readiness probe (issue #477, WEB-03).
//
// A deployment is not atomic, and this app's server does its migrations at
// startup, before it binds its listener. A client that loads the (nginx-served)
// frontend while the backend is still mid-migration therefore faces an API that
// is up-but-not-ready: nginx answers the static app and proxies every /api and
// /health request to a backend that is not listening yet (502/504) or that
// explicitly reports itself not ready (503, issue #421's /health/ready). Left
// alone, the app mounts and fires a wall of failed requests with no indication
// why. <ServerStartingGate> holds the app back until this probe says the
// server can serve.
//
// The probe is deliberately fail-open (docs/client-compatibility-policy.md's
// stance, same as the stale-client detector): only an *authoritative* "not
// ready yet" signal holds the app back. An unreachable or ambiguous /health/
// ready (offline, a 404, an HTML response, a timeout) resolves to 'unknown'
// and the app mounts exactly as it would have without this module — a network
// error must never lock a user out of an app that could otherwise work.
import { API_BASE_URL } from '../auth';

// /health/ready is registered at the server root, NOT under /api/v1 (see
// backend/routes/routes.go), so the versioned prefix is stripped like the
// health module does for /health.
const READY_URL = `${API_BASE_URL.replace(/\/api\/v1$/, '')}/health/ready`;

// How often the gate re-probes while the server reports not ready.
export const READY_POLL_MS = 3_000;

// Abort a probe that hangs instead of failing (a wedged proxy must not hold
// the app back forever — an aborted probe is 'unknown', i.e. fail open).
export const READY_PROBE_TIMEOUT_MS = 5_000;

export type ServerReadiness = 'ready' | 'starting' | 'unknown';

type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

export interface ReadinessResponseBody {
  /** ready | not_ready (backend controllers.ReadinessResponse). */
  status?: string;
  checks?: Record<string, unknown>;
}

/**
 * Classifies a fetched /health/ready response.
 *
 *   - 'ready'    — HTTP 200 with status "ready": the instance can serve.
 *   - 'starting' — an authoritative "not yet": 503 (the backend's own
 *                  readiness answer, e.g. migrations pending), or 502/504
 *                  (nginx in front of a backend that is not listening yet).
 *                  These are the mid-deploy signatures.
 *   - 'unknown'  — everything else: any other non-2xx (a 404, a real 500), a
 *                  200 that is not the readiness JSON (the SPA shell, a proxy
 *                  quirk), or an unparseable body. Fails open.
 */
export async function classifyReadiness(response: Response): Promise<ServerReadiness> {
  if (response.status === 502 || response.status === 503 || response.status === 504) {
    // Authoritative "not yet": the backend's own readiness 503 (migrations
    // pending, DB/filesystem unreachable) or nginx's 502/504 in front of a
    // backend that is not listening yet.
    return 'starting';
  }
  if (!response.ok) {
    return 'unknown';
  }
  let body: ReadinessResponseBody;
  try {
    body = (await response.json()) as ReadinessResponseBody;
  } catch {
    // A 200 that is not JSON (e.g. the SPA shell served by a fallback route)
    // is not a readiness answer — fail open.
    return 'unknown';
  }
  const status = body.status?.trim();
  if (status === 'ready') {
    return 'ready';
  }
  if (status === 'not_ready') {
    return 'starting';
  }
  return 'unknown';
}

/**
 * Fetches /health/ready and classifies the result. Never throws: a network
 * error or an aborted probe is 'unknown' (fail open). `fetchImpl` is
 * injectable for tests.
 */
export async function probeServerReadiness(
  fetchImpl: FetchLike = fetch,
  url: string = READY_URL,
): Promise<ServerReadiness> {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), READY_PROBE_TIMEOUT_MS);
  try {
    const response = await fetchImpl(url, {
      credentials: 'include',
      cache: 'no-store',
      signal: controller.signal,
    });
    return await classifyReadiness(response);
  } catch {
    return 'unknown';
  } finally {
    clearTimeout(timeoutId);
  }
}
