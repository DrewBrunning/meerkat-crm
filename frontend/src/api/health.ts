// Health / build identity API.
//
// Exists so the running build can be shown to the user. /health (the deep
// check) reports the version, commit and build date injected at link time
// (backend/buildinfo); before that it returned a hardcoded "0.1.0" for every
// build ever made, so a bug report could not be tied to the binary that
// produced it.
//
// The backend health surface is three endpoints (issue #421): /health/live
// (liveness), /health/ready (readiness), and /health (deep). Only the deep
// endpoint carries the build identity, so that is the one this module hits.
import { API_BASE_URL, apiFetch, getAuthHeaders, parseErrorResponse } from './client';

// /health is registered at the server root, NOT under /api/v1 (see
// backend/routes/routes.go), so the versioned prefix has to be stripped.
const HEALTH_URL = `${API_BASE_URL.replace(/\/api\/v1$/, '')}/health`;

export interface DatabaseHealth {
  status: string;
  response_time_ms: number;
}

export interface HealthResponse {
  /** healthy | degraded | unhealthy (issue #421). "degraded" is still HTTP 200. */
  status: string;
  timestamp: string;
  database: DatabaseHealth;
  version: string;
  /** Short git SHA; absent on a build with no VCS info. May carry a "-dirty" suffix. */
  commit?: string;
  build_date?: string;
  /**
   * The API contract generation this server speaks — always present on a
   * v0.6.10+ server ("v1" while the API is on /api/v1). Absent on a server
   * that predates the field, which is treated as "v1" by consumers
   * (docs/client-compatibility-policy.md, issues #475/#528).
   */
  api_contract_version?: string;
  /**
   * The oldest client version the server still supports. Absent until an
   * operator raises the floor via MIN_CLIENT_VERSION (a MAINT-02 event);
   * absence means no floor has ever been declared.
   */
  min_client_version?: string;
  // No `checks` field: the per-facet deep-health breakdown was removed from
  // the unauthenticated /health (issue #864). The full services.DeepHealth
  // snapshot is admin-only at GET /api/v1/admin/system-status (see
  // api/systemStatus.ts).
}

// GET /health — unauthenticated, but auth headers are sent when present so the
// call behaves like every other one in this module.
export async function getHealth(): Promise<HealthResponse> {
  const response = await apiFetch(HEALTH_URL, { headers: getAuthHeaders() });
  if (!response.ok) throw await parseErrorResponse(response);
  return response.json();
}

/**
 * Formats the build identity for display, e.g. "v0.2.0 (abc1234)".
 * Falls back to the bare version when no commit was stamped.
 */
export function formatBuildVersion(health: Pick<HealthResponse, 'version' | 'commit'>): string {
  if (!health.commit) return health.version;
  return `${health.version} (${health.commit})`;
}
