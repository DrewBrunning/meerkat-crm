// Severity classification for the stale-client contract check (issue #475,
// WEB-01). This is the pure decision half of the mechanism — given what
// /health advertises and what THIS bundle is, which of the three states from
// docs/client-compatibility-policy.md applies:
//
//   - compatible        — silent. Backward compatibility is the norm; an
//                         absent floor means every released client works.
//   - update-available  — a newer build was deployed while this tab was open.
//                         Non-blocking: the service-worker update prompt
//                         (already wired) offers the reload.
//   - blocked           — this bundle is genuinely incompatible with the
//                         server: it speaks a different API contract
//                         generation, or it is below the server's declared
//                         floor. Reload is required, not offered — but a
//                         network error must never reach here (fail open), so
//                         only a successfully-fetched /health is assessed.
//
// The decision deliberately fails OPEN: an unparseable version, a missing
// contract field, or an unversioned build all land on "compatible" rather
// than risking a lock-out or reload loop from bad data.
import type { HealthResponse } from '../api/health';
import {
  compareClientVersions,
  getClientContractVersion,
  getClientVersion,
  isClientVersionStamped,
} from './version';

export type StalenessSeverity = 'compatible' | 'update-available' | 'blocked';

export type BlockedReason = 'contract-mismatch' | 'below-floor';

export type AssessmentReason = 'inert' | 'compatible' | 'server-newer' | BlockedReason;

export interface ContractAssessment {
  severity: StalenessSeverity;
  reason: AssessmentReason;
}

export function assessServerContract(health: HealthResponse): ContractAssessment {
  // Dev/test builds have no stamped version; the mechanism is off for them,
  // matching register()'s production-only gate. Never block an unversioned
  // build.
  if (!isClientVersionStamped()) {
    return { severity: 'compatible', reason: 'inert' };
  }

  const clientVersion = getClientVersion();
  const clientContract = getClientContractVersion();

  // A server that predates api_contract_version only ever spoke the first
  // (and so far only) generation — "v1". A present-but-different generation
  // means this bundle and the server disagree about the contract: the remedy
  // is a reload onto the build the server actually serves.
  const serverContract = health.api_contract_version?.trim() || 'v1';
  if (serverContract !== clientContract) {
    return { severity: 'blocked', reason: 'contract-mismatch' };
  }

  // min_client_version is absent until a MAINT-02 floor-move happens. When it
  // is present, this bundle being below it means the server has deliberately
  // stopped supporting this build — a forced reload is the web equivalent of
  // Android's force-update screen. An unparseable floor fails open (the
  // server is advertising garbage; an old client is not the problem).
  const floor = health.min_client_version?.trim();
  if (floor) {
    const comparedToFloor = compareClientVersions(clientVersion, floor);
    if (comparedToFloor !== null && comparedToFloor < 0) {
      return { severity: 'blocked', reason: 'below-floor' };
    }
  }

  // A server that reports a newer release than this bundle means a deploy
  // happened under this open tab. This is the *backstop* detection — the
  // service worker normally surfaces the same fact via its own update
  // lifecycle, but an idle tab that never navigates would otherwise sit on
  // the old bundle indefinitely (recommended action #5, issue #475).
  const serverVersion = health.version?.trim();
  if (serverVersion) {
    const comparedToServer = compareClientVersions(serverVersion, clientVersion);
    if (comparedToServer !== null && comparedToServer > 0) {
      return { severity: 'update-available', reason: 'server-newer' };
    }
  }

  return { severity: 'compatible', reason: 'compatible' };
}
