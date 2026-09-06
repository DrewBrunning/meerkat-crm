import { useCallback, useEffect, useRef, useState, type ReactElement } from 'react';
import { useBeforeUnloadGuard } from './useBeforeUnloadGuard';
import { useNavigationGuard } from './useNavigationGuard';

export interface DiscardGuardOptions {
  // Runs when the user confirms a discard triggered by an in-app route
  // navigation (drawer link, programmatic navigate, browser Back). A plain
  // close (Cancel/Escape) never reaches this -- it runs the function passed
  // to guardedClose instead, which already performs the same cleanup. Each
  // guarded surface passes its own full discard here: AddNoteDialog and
  // AddActivityDialog close themselves (which clears the session draft);
  // the import wizard cancels the live backend session and resets.
  onNavigationDiscard?: () => void;
}

interface DiscardGuard {
  // Wrap a real close/cancel handler with this. When the form is clean it
  // runs immediately; when dirty it opens <ConfirmDiscardDialog> instead and
  // only runs once the user confirms discarding.
  guardedClose: (closeFn: () => void) => void;
  // Spread directly onto <ConfirmDiscardDialog>.
  confirmDialogProps: {
    open: boolean;
    onKeepEditing: () => void;
    onDiscard: () => void;
  };
  // Mount inside the guarded surface (renders null outside a data router).
  // The navigation blocker lives only while this is mounted -- for a MUI
  // dialog that means while it is open, which is exactly the window a dirty
  // surface can lose work to an in-app navigation.
  navigationGuardElement: ReactElement | null;
}

// Issue #557: pairs a beforeunload guard (tab close / reload / external nav)
// with a confirm-before-discard guard for the dialog's own close paths (Cancel
// button, Escape key -- AppDialog already blocks a stray backdrop click for
// every dialog in this app, dirty or not). Issue #805 extends the same guard to
// *in-app route navigation*: the data-router blocker that useNavigationGuard
// arms intercepts a drawer link, a programmatic navigate, or a browser
// Back/Forward while the surface is dirty, and routes through the same
// ConfirmDiscardDialog. One hook covers all three triggers, since each exists
// to answer the same question: does leaving right now lose something the user
// hasn't saved.
export function useDiscardGuard(
  isDirty: boolean,
  options: DiscardGuardOptions = {},
): DiscardGuard {
  useBeforeUnloadGuard(isDirty);

  const [pendingClose, setPendingClose] = useState<(() => void) | null>(null);
  const navigation = useNavigationGuard(isDirty);

  // Keep the latest option without re-creating the handlers below on every
  // render (the callback identity is irrelevant -- it is only read at discard
  // time, and a ref avoids making the returned props unstable).
  const onNavigationDiscardRef = useRef(options.onNavigationDiscard);
  useEffect(() => {
    onNavigationDiscardRef.current = options.onNavigationDiscard;
  }, [options.onNavigationDiscard]);

  const guardedClose = useCallback(
    (closeFn: () => void) => {
      if (isDirty) {
        setPendingClose(() => closeFn);
      } else {
        closeFn();
      }
    },
    [isDirty],
  );

  const onKeepEditing = useCallback(() => {
    setPendingClose(null);
    navigation.cancel();
  }, [navigation]);

  const onDiscard = useCallback(() => {
    const closeFn = pendingClose;
    setPendingClose(null);

    if (closeFn) {
      // A pending dialog close (Cancel/Escape). The close handler already
      // performs the full discard cleanup, so run it; if a navigation is also
      // suspended underneath (Cancel pressed, then Back), let that through
      // too -- the close already discarded, no separate cleanup needed.
      closeFn();
    }
    if (navigation.blocked) {
      if (!closeFn) {
        onNavigationDiscardRef.current?.();
      }
      navigation.proceed();
    }
  }, [pendingClose, navigation]);

  return {
    guardedClose,
    confirmDialogProps: {
      open: pendingClose !== null || (isDirty && navigation.blocked),
      onKeepEditing,
      onDiscard,
    },
    navigationGuardElement: navigation.guardElement,
  };
}
