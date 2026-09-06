import { useEffect, useRef } from 'react';
import { nextDirtyKey, reportDirty } from '../staleClient/dirty';

// Issue #557: warns before a tab close, reload, or external navigation
// discards unsaved work. `beforeunload` is the only guard that can catch
// those three cases at all -- react-router's useBlocker only covers
// in-app navigation, and this app's router (`<BrowserRouter>` + `<Routes>`,
// not a data router) doesn't provide the router context useBlocker needs
// regardless. In-app navigation away from a dirty *dialog* is instead
// guarded at the dialog's own close handlers (see useDiscardGuard) -- every
// editing surface in this app is a MUI Dialog, not a routed page, so that
// covers the cases useBlocker exists for here without the router migration.
//
// The browser controls the confirmation's wording; the `returnValue` string
// itself is ignored by every modern browser (it shows a fixed built-in
// message instead), but both the assignment and the return are required for
// the various engines that implement this event differently.
//
// While this hook is also this app's dirty-state choke point for the
// stale-client forced reload (issue #475, WEB-01): every editing surface
// funnels through useDiscardGuard → useBeforeUnloadGuard, so reporting each
// mounted instance's isDirty into the shared dirty registry (staleClient/
// dirty.ts) gives the forced-reload decision one app-wide answer. A stable
// per-instance key (not the boolean alone) keeps simultaneous dirty dialogs
// from stepping on each other's cleanup.
export function useBeforeUnloadGuard(isDirty: boolean): void {
  const key = useRef(nextDirtyKey('beforeunload')).current;

  useEffect(() => {
    if (!isDirty) return;

    const handler = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
      return '';
    };

    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [isDirty]);

  useEffect(() => {
    reportDirty(key, isDirty);
    return () => reportDirty(key, false);
  }, [isDirty, key]);
}
