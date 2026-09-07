// Stale-client detector (issue #475, WEB-01) — the "backstop" half of the web
// mechanism. The service worker update prompt already handles the normal
// path (a new build installed, offer a reload); this detector covers the
// cases that path cannot:
//
//   - the mid-session deploy: a tab that has sat open for hours never
//     navigates, so the browser's service-worker update check may never fire.
//     Polling /health (cheap, unauthenticated) notices the server's release
//     moved and kicks registration.update() so the existing prompt surfaces.
//   - the genuinely incompatible client: this bundle below the server's
//     declared floor, or speaking a different api_contract_version. A reload
//     is then required, not offered — this module drives it.
//
// The polling is deliberately fail-open (docs/client-compatibility-policy.md):
// /health unreachable or malformed changes nothing and never blocks the app.
// An automatic reload only ever happens when the tab is clean; a dirty form
// routes through the gate's explicit consent dialog instead of losing input.
import { getHealth, type HealthResponse } from '../api/health';
import { assessServerContract, type BlockedReason } from './contract';
import { isAnythingDirty, onDirtyChange } from './dirty';
import { forceReloadToCurrentBuild, hasRecentlyForcedReload } from './reload';
import { isClientVersionStamped } from './version';

export const CHECK_INTERVAL_MS = 60_000;
export const BLOCK_SUPPRESS_MS = 10 * 60_000;

export type BlockNotice =
  | { kind: 'none' }
  | {
      kind: 'blocked';
      reason: BlockedReason;
      /** A form is dirty, so the reload must not happen silently. */
      dirty: boolean;
      /** A clean reload has already been triggered automatically. */
      autoReloading: boolean;
    };

export type NoticeListener = (notice: BlockNotice) => void;

let running = false;
let intervalId: number | null = null;
let noticeListener: NoticeListener | null = null;
let currentNotice: BlockNotice = { kind: 'none' };
let suppressBlockedUntil = 0;
let autoReloadAttempted = false;
let checking = false;
let visibilityListeners: Array<() => void> = [];
let dirtyUnsubscribe: (() => void) | null = null;

function emit(notice: BlockNotice): void {
  currentNotice = notice;
  noticeListener?.(notice);
}

function emitNoneIfBlocked(): void {
  if (currentNotice.kind === 'blocked') {
    emit({ kind: 'none' });
  }
}

/**
 * Kicks the browser's service-worker update check so the (already wired)
 * update prompt can surface a freshly-deployed build on a tab that has not
 * navigated. No-op when a worker is already waiting — the prompt is up.
 */
async function kickServiceWorkerUpdate(): Promise<void> {
  if (!('serviceWorker' in navigator)) {
    return;
  }
  try {
    const registration = await navigator.serviceWorker.getRegistration();
    if (registration && !registration.waiting) {
      await registration.update();
    }
  } catch {
    // A failed update check is fine — the periodic poll will retry.
  }
}

function scheduleBlockedActions(reason: BlockedReason): void {
  if (Date.now() < suppressBlockedUntil) {
    return;
  }
  if (hasRecentlyForcedReload()) {
    // The last forced reload did not converge (the server is still
    // advertising this floor); do not auto-loop — put a manual reload in
    // front of the user instead.
    emit({ kind: 'blocked', reason, dirty: false, autoReloading: false });
    return;
  }
  if (isAnythingDirty()) {
    // Never silently discard user input. The gate shows the consent dialog;
    // when the user finishes saving, onDirtyChange below reloads for them.
    emit({ kind: 'blocked', reason, dirty: true, autoReloading: false });
    return;
  }
  if (autoReloadAttempted) {
    emit({ kind: 'blocked', reason, dirty: false, autoReloading: false });
    return;
  }
  autoReloadAttempted = true;
  emit({ kind: 'blocked', reason, dirty: false, autoReloading: true });
  void forceReloadToCurrentBuild();
}

/**
 * Runs one contract check against /health and acts on the outcome. Public so
 * the gate and tests can trigger a check on demand; also invoked on an
 * interval and whenever the tab becomes visible/focused (the mid-session
 * case). Never throws: a network error or malformed response is a no-op.
 */
export async function checkNow(): Promise<void> {
  if (!isClientVersionStamped() || checking) {
    return;
  }
  checking = true;
  try {
    let health: HealthResponse;
    try {
      health = await getHealth();
    } catch {
      // Fail open: an unreachable /health (server restarting, flaky network)
      // must never block the app or spin a reload.
      return;
    }

    const assessment = assessServerContract(health);
    switch (assessment.severity) {
      case 'blocked':
        if (assessment.reason === 'contract-mismatch' || assessment.reason === 'below-floor') {
          scheduleBlockedActions(assessment.reason);
        }
        break;
      case 'update-available':
        emitNoneIfBlocked();
        await kickServiceWorkerUpdate();
        break;
      case 'compatible':
      default:
        emitNoneIfBlocked();
        break;
    }
  } finally {
    checking = false;
  }
}

/**
 * Starts the detector. A no-op (returns false) on builds without a stamped
 * version, matching register()'s production-only gate.
 */
export function startStaleClientDetector(): boolean {
  if (running) {
    return true;
  }
  if (!isClientVersionStamped()) {
    return false;
  }

  running = true;
  intervalId = window.setInterval(() => void checkNow(), CHECK_INTERVAL_MS);

  // The mid-session case: a tab left open through a deploy is the common
  // real scenario, and returning to it is when the staleness matters.
  const onVisible = () => {
    if (document.visibilityState === 'visible') {
      void checkNow();
    }
  };
  window.addEventListener('focus', onVisible);
  document.addEventListener('visibilitychange', onVisible);
  visibilityListeners.push(onVisible);

  // A dirty form being saved/closed while a block is pending means the
  // reload can now proceed safely.
  dirtyUnsubscribe = onDirtyChange(() => {
    if (currentNotice.kind === 'blocked' && currentNotice.dirty && !isAnythingDirty()) {
      if (
        Date.now() >= suppressBlockedUntil &&
        !autoReloadAttempted &&
        !hasRecentlyForcedReload()
      ) {
        autoReloadAttempted = true;
        emit({ ...currentNotice, dirty: false, autoReloading: true });
        void forceReloadToCurrentBuild();
      }
    }
  });

  void checkNow();
  return true;
}

/** Stops the detector and detaches its listeners (test teardown). */
export function stopStaleClientDetector(): void {
  if (intervalId !== null) {
    window.clearInterval(intervalId);
    intervalId = null;
  }
  for (const listener of visibilityListeners) {
    window.removeEventListener('focus', listener);
    document.removeEventListener('visibilitychange', listener);
  }
  visibilityListeners = [];
  dirtyUnsubscribe?.();
  dirtyUnsubscribe = null;
  running = false;
}

/** Subscribes to block notices. Returns an unsubscribe function. */
export function onStaleClientNotice(fn: NoticeListener): () => void {
  noticeListener = fn;
  // Replay the current state so a gate mounting after a block was detected
  // still shows it — the same replay-on-subscribe pattern as the update bus.
  if (currentNotice.kind === 'blocked') {
    fn(currentNotice);
  }
  return () => {
    if (noticeListener === fn) {
      noticeListener = null;
    }
  };
}

/** "Later" from the consent dialog: suppress the block for a while. */
export function dismissBlockedNotice(): void {
  suppressBlockedUntil = Date.now() + BLOCK_SUPPRESS_MS;
  emit({ kind: 'none' });
}

/** Explicit user consent ("Reload now") — reload regardless of dirty state. */
export function reloadBlockedClient(): void {
  autoReloadAttempted = true;
  emit({ kind: 'none' });
  void forceReloadToCurrentBuild();
}

/** Clears module state between tests. */
export function resetStaleClientDetectorForTest(): void {
  stopStaleClientDetector();
  noticeListener = null;
  currentNotice = { kind: 'none' };
  suppressBlockedUntil = 0;
  autoReloadAttempted = false;
  checking = false;
}
