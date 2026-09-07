// App-wide "something has unsaved changes" registry (issue #475, WEB-01).
//
// The forced-reload decision — is it safe to reload this tab, or would that
// silently discard what the user is typing? — needs a single answer that no
// single component owns: every editing surface in this app is a MUI Dialog
// with its own local isDirty flag (hooks/useDiscardGuard). Those surfaces
// already funnel through hooks/useBeforeUnloadGuard, so that hook reports
// each mounted instance's dirty state here. The stale-client detector asks
// `isAnythingDirty()` before it reloads a tab whose contract has gone stale.
//
// Deliberately a Set of keys, not a counter: reporting the same key twice
// must be idempotent, and a component unmounting while dirty must clear its
// own slot without affecting anyone else's.

type DirtyListener = () => void;

const dirtyKeys = new Set<string>();
let dirtyListener: DirtyListener | null = null;
let keySequence = 0;

/** Allocates a stable key for one mounted editing surface. */
export function nextDirtyKey(label: string): string {
  keySequence += 1;
  return `${label}:${keySequence}`;
}

/**
 * Reports whether the editing surface identified by `key` currently has
 * unsaved changes. `false` (clean or unmounted) removes the key.
 */
export function reportDirty(key: string, dirty: boolean): void {
  const changed = dirty ? dirtyKeys.add(key) : dirtyKeys.delete(key);
  if (changed) {
    dirtyListener?.();
  }
}

/** True when any mounted editing surface has unsaved changes. */
export function isAnythingDirty(): boolean {
  return dirtyKeys.size > 0;
}

/**
 * Subscribes to dirty-state changes (a component finished saving, a dialog
 * opened). Returns an unsubscribe function. Single-listener by design, like
 * serviceWorkerUpdates.ts — the one consumer is the stale-client detector.
 */
export function onDirtyChange(fn: DirtyListener): () => void {
  dirtyListener = fn;
  return () => {
    if (dirtyListener === fn) {
      dirtyListener = null;
    }
  };
}

/** Clears module state between tests. */
export function resetDirtyTrackerForTest(): void {
  dirtyKeys.clear();
  dirtyListener = null;
  keySequence = 0;
}
