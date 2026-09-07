import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react';
import { createMemoryRouter, Link, RouterProvider } from 'react-router';
import { afterEach, describe, expect, test, vi } from 'vitest';
import ConfirmDiscardDialog from '../components/ConfirmDiscardDialog';
import { useDiscardGuard } from './useDiscardGuard';
import '../i18n/config';

afterEach(() => {
  // This codebase's vitest setup has no auto-cleanup: without unmounting,
  // an earlier test's still-live beforeunload listener would fire alongside
  // (or instead of) the current test's hook instance.
  cleanup();
  vi.restoreAllMocks();
});

describe('useDiscardGuard', () => {
  test('closes immediately when not dirty', () => {
    const { result } = renderHook(() => useDiscardGuard(false));
    const closeFn = vi.fn();

    act(() => result.current.guardedClose(closeFn));

    expect(closeFn).toHaveBeenCalledTimes(1);
    expect(result.current.confirmDialogProps.open).toBe(false);
  });

  test('opens the confirm dialog instead of closing when dirty', () => {
    const { result } = renderHook(() => useDiscardGuard(true));
    const closeFn = vi.fn();

    act(() => result.current.guardedClose(closeFn));

    expect(closeFn).not.toHaveBeenCalled();
    expect(result.current.confirmDialogProps.open).toBe(true);
  });

  test('onKeepEditing closes the confirm dialog without running the close function', () => {
    const { result } = renderHook(() => useDiscardGuard(true));
    const closeFn = vi.fn();
    act(() => result.current.guardedClose(closeFn));

    act(() => result.current.confirmDialogProps.onKeepEditing());

    expect(closeFn).not.toHaveBeenCalled();
    expect(result.current.confirmDialogProps.open).toBe(false);
  });

  test('onDiscard runs the pending close function and closes the confirm dialog', () => {
    const { result } = renderHook(() => useDiscardGuard(true));
    const closeFn = vi.fn();
    act(() => result.current.guardedClose(closeFn));

    act(() => result.current.confirmDialogProps.onDiscard());

    expect(closeFn).toHaveBeenCalledTimes(1);
    expect(result.current.confirmDialogProps.open).toBe(false);
  });

  test('warns on beforeunload while dirty', () => {
    const { rerender } = renderHook(({ isDirty }) => useDiscardGuard(isDirty), {
      initialProps: { isDirty: false },
    });

    const clean = new Event('beforeunload', { cancelable: true }) as BeforeUnloadEvent;
    window.dispatchEvent(clean);
    expect(clean.defaultPrevented).toBe(false);

    rerender({ isDirty: true });

    const dirty = new Event('beforeunload', { cancelable: true }) as BeforeUnloadEvent;
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);
  });
});

// Issue #805: once the app runs on a data router, useDiscardGuard also arms a
// blocker that guards *in-app route navigation* away from a dirty surface.
// These tests mount the guard under a real data router (createMemoryRouter +
// RouterProvider) and attempt a navigation to prove the confirm dialog
// intercepts it, and that Keep editing / Discard resolve it correctly.
function GuardHarness({
  isDirty,
  onNavigationDiscard,
  onClose,
}: {
  isDirty: boolean;
  onNavigationDiscard: () => void;
  onClose: () => void;
}) {
  const { guardedClose, confirmDialogProps, navigationGuardElement } = useDiscardGuard(isDirty, {
    onNavigationDiscard,
  });
  return (
    <div>
      {navigationGuardElement}
      <Link to="/away">leave</Link>
      <button type="button" onClick={() => guardedClose(onClose)}>
        close
      </button>
      <ConfirmDiscardDialog {...confirmDialogProps} />
    </div>
  );
}

function makeRouter(isDirty: boolean, onNavigationDiscard = vi.fn(), onClose = vi.fn()) {
  return createMemoryRouter(
    [
      {
        path: '/home',
        element: (
          <GuardHarness
            isDirty={isDirty}
            onNavigationDiscard={onNavigationDiscard}
            onClose={onClose}
          />
        ),
      },
      { path: '/away', element: <div>away page</div> },
    ],
    { initialEntries: ['/home'] },
  );
}

describe('useDiscardGuard under a data router (in-app navigation guard)', () => {
  test('clean surface: navigation proceeds without a prompt', () => {
    const router = makeRouter(false);
    render(<RouterProvider router={router} />);

    fireEvent.click(screen.getByText('leave'));

    expect(router.state.location.pathname).toBe('/away');
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();
  });

  test('dirty surface: in-app navigation opens the confirm dialog and stays put', async () => {
    const onNavigationDiscard = vi.fn();
    const router = makeRouter(true, onNavigationDiscard);
    render(<RouterProvider router={router} />);

    await act(async () => {
      router.navigate('/away');
    });

    expect(router.state.location.pathname).toBe('/home');
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();
    expect(onNavigationDiscard).not.toHaveBeenCalled();
  });

  test('Keep editing cancels the navigation and leaves the surface dirty', async () => {
    const onNavigationDiscard = vi.fn();
    const router = makeRouter(true, onNavigationDiscard);
    render(<RouterProvider router={router} />);

    await act(async () => {
      router.navigate('/away');
    });
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));

    expect(router.state.location.pathname).toBe('/home');
    // MUI keeps a closing Dialog's children mounted through its exit
    // transition, so wait for the confirm to actually unmount.
    await waitFor(() => expect(screen.queryByText('Discard unsaved changes?')).toBeNull());
    expect(onNavigationDiscard).not.toHaveBeenCalled();
  });

  test('Discard runs the navigation-discard cleanup and lets the navigation through', async () => {
    const onNavigationDiscard = vi.fn();
    const router = makeRouter(true, onNavigationDiscard);
    render(<RouterProvider router={router} />);

    await act(async () => {
      router.navigate('/away');
    });
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));

    expect(onNavigationDiscard).toHaveBeenCalledTimes(1);
    expect(router.state.location.pathname).toBe('/away');
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();
  });

  test('a pending dialog close and a blocked navigation both resolve on Discard', async () => {
    const onNavigationDiscard = vi.fn();
    const onClose = vi.fn();
    const router = makeRouter(true, onNavigationDiscard, onClose);
    render(<RouterProvider router={router} />);

    // Cancel (a pending close) opens the confirm, then a navigation is also
    // attempted while it is open.
    fireEvent.click(screen.getByText('close'));
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();
    await act(async () => {
      router.navigate('/away');
    });

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));

    // The pending close ran (the surface closed itself) and the suspended
    // navigation went through; because the close already performed the
    // discard cleanup, the navigation cleanup must NOT also run.
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onNavigationDiscard).not.toHaveBeenCalled();
    expect(router.state.location.pathname).toBe('/away');
  });
});
