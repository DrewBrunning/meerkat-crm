import { useEffect } from 'react';

// Issue #557: warns before a tab close, reload, or external navigation
// discards unsaved work. `beforeunload` is the only guard that can catch
// those three cases at all -- the browser owns them and never lets a web
// page intercept them. In-app route navigation (a drawer link, a
// programmatic navigate, browser Back/Forward) is a separate case that
// `beforeunload` cannot see and react-router's useBlocker exists for; since
// issue #805 migrated the app to a data router, useDiscardGuard arms that
// blocker via useNavigationGuard. Each guard covers the surfaces the other
// physically cannot, and useDiscardGuard wires both.
//
// The browser controls the confirmation's wording; the `returnValue` string
// itself is ignored by every modern browser (it shows a fixed built-in
// message instead), but both the assignment and the return are required for
// the various engines that implement this event differently.
export function useBeforeUnloadGuard(isDirty: boolean): void {
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
}
