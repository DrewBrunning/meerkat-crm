import { useCallback, useContext, useEffect, useRef, useState, type ReactElement } from 'react';
import {
  UNSAFE_DataRouterContext as DataRouterContext,
  useBlocker,
} from 'react-router';

// Issue #805: guards in-app route navigation away from a dirty editing surface.
//
// react-router's useBlocker only works under a *data* router (created with
// createBrowserRouter/createMemoryRouter and rendered by <RouterProvider>) --
// the plain <BrowserRouter>+<Routes> mode never installs the DataRouterContext
// useBlocker reads, which is exactly why this hook exists behind the App.tsx
// migration. A dirty dialog (AddNoteDialog, AddActivityDialog, the
// source-import wizard's review step) registers a blocker while it is mounted;
// any react-router navigation -- a drawer <Link>, a programmatic navigate, or
// a browser Back/Forward pop -- is then suspended until the UI asks the user
// whether to discard or stay.
//
// The returned `guardElement` must be mounted inside the guarded surface (the
// dialog). It renders a bridge that owns the useBlocker call, which lets this
// hook degrade to a no-op when there is no data router at all -- isolated
// component tests render these dialogs under a plain <MemoryRouter> or no
// router, where there is no blocker machinery to arm and nothing to block.

interface NavigationGuardHandle {
  proceed: () => void;
  reset: () => void;
}

export interface NavigationGuard {
  // True while a navigation is currently suspended, waiting on a discard
  // decision. Drives the ConfirmDiscardDialog's open state.
  blocked: boolean;
  // Mount inside the guarded surface. Renders null (and registers nothing)
  // outside a data router.
  guardElement: ReactElement | null;
  // The user chose to leave: let the suspended navigation through.
  proceed: () => void;
  // The user chose to stay: cancel the suspended navigation.
  cancel: () => void;
}

interface NavigationGuardBridgeProps {
  shouldBlock: boolean;
  onHandle: (handle: NavigationGuardHandle) => void;
  onBlockedChange: (blocked: boolean) => void;
}

// Owns the useBlocker call so useNavigationGuard can mount it conditionally:
// the bridge is only rendered when a DataRouterContext exists, and a hook can
// never be called conditionally inside a single component. The blocker object
// the router hands back is mutated in place (its `state` flips between
// 'unblocked' and 'blocked' without the object identity necessarily changing),
// so both effects are keyed on `blocked`, the one value that actually
// changes across a block/unblock cycle. Only a 'blocked' blocker exposes
// proceed/reset (the react-router type models the other states without them),
// which is why the imperative handle is captured inside that branch.
function NavigationGuardBridge({ shouldBlock, onHandle, onBlockedChange }: NavigationGuardBridgeProps) {
  const blocker = useBlocker(shouldBlock);
  const blocked = blocker.state === 'blocked';

  useEffect(() => {
    if (blocker.state !== 'blocked') return;
    onHandle({
      proceed: () => blocker.proceed(),
      reset: () => blocker.reset(),
    });
  }, [blocked, blocker, onHandle]);

  useEffect(() => {
    onBlockedChange(blocked);
  }, [blocked, onBlockedChange]);

  return null;
}

export function useNavigationGuard(shouldBlock: boolean): NavigationGuard {
  const isDataRouter = useContext(DataRouterContext) != null;
  const [blocked, setBlocked] = useState(false);
  const handleRef = useRef<NavigationGuardHandle | null>(null);

  const captureHandle = useCallback((handle: NavigationGuardHandle) => {
    handleRef.current = handle;
  }, []);
  const reportBlocked = useCallback((nextBlocked: boolean) => {
    setBlocked(nextBlocked);
  }, []);

  const proceed = useCallback(() => {
    // Clear the local flag immediately so the confirm dialog closes even if
    // the suspended navigation is consumed by an unmount before the bridge
    // can report the blocker's own state transition.
    setBlocked(false);
    handleRef.current?.proceed();
  }, []);

  const cancel = useCallback(() => {
    setBlocked(false);
    handleRef.current?.reset();
  }, []);

  return {
    blocked,
    guardElement: isDataRouter ? (
      <NavigationGuardBridge
        shouldBlock={shouldBlock}
        onHandle={captureHandle}
        onBlockedChange={reportBlocked}
      />
    ) : null,
    proceed,
    cancel,
  };
}
