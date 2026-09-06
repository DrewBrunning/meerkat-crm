import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test } from 'vitest';
import { useState } from 'react';
import { Link, MemoryRouter, RouterProvider, createMemoryRouter } from 'react-router';
import { useNavigationGuard } from './useNavigationGuard';

afterEach(() => {
  cleanup();
});

// A tiny surface that arms the guard and exposes its decision buttons, the
// same shape the discard-guarded dialogs use (guardElement rendered next to
// the content, proceed/cancel wired to the confirm dialog's buttons).
function GuardHarness({ shouldBlock }: { shouldBlock: boolean }) {
  const guard = useNavigationGuard(shouldBlock);
  const [dirty, setDirty] = useState(shouldBlock);
  return (
    <div>
      {guard.guardElement}
      <Link to="/away">leave</Link>
      <button onClick={() => setDirty(true)}>make dirty</button>
      <span>dirty:{dirty ? 'yes' : 'no'}</span>
      <span>blocked:{guard.blocked ? 'yes' : 'no'}</span>
      {guard.blocked && (
        <>
          <button onClick={guard.cancel}>cancel</button>
          <button onClick={guard.proceed}>discard and leave</button>
        </>
      )}
    </div>
  );
}

function makeRouter(shouldBlock: boolean) {
  return createMemoryRouter(
    [
      { path: '/home', element: <GuardHarness shouldBlock={shouldBlock} /> },
      { path: '/away', element: <div>away page</div> },
    ],
    { initialEntries: ['/home'] },
  );
}

describe('useNavigationGuard', () => {
  test('degrades to a no-op with no router at all (isolated component render)', () => {
    const guard = captureGuard();
    expect(guard.guardElement).toBeNull();
    expect(guard.blocked).toBe(false);
    expect(() => guard.proceed()).not.toThrow();
    expect(() => guard.cancel()).not.toThrow();
  });

  test('degrades to a no-op under a declarative <MemoryRouter> (no data router)', () => {
    function Host() {
      const guard = useNavigationGuard(true);
      return (
        <div>
          {guard.guardElement}
          <span>blocked:{guard.blocked ? 'yes' : 'no'}</span>
          <span>element:{guard.guardElement === null ? 'null' : 'present'}</span>
        </div>
      );
    }
    render(
      <MemoryRouter>
        <Host />
      </MemoryRouter>,
    );
    expect(screen.getByText('blocked:no')).toBeTruthy();
    expect(screen.getByText('element:null')).toBeTruthy();
  });

  test('clean (not dirty): navigation is not interrupted', () => {
    const router = makeRouter(false);
    render(<RouterProvider router={router} />);

    fireEvent.click(screen.getByText('leave'));

    expect(router.state.location.pathname).toBe('/away');
    expect(screen.queryByText('blocked:yes')).toBeNull();
  });

  test('dirty: a navigation is suspended until the user decides', async () => {
    const router = makeRouter(true);
    render(<RouterProvider router={router} />);

    // Attempt to leave while dirty -- the "in-app route navigation" (drawer
    // link, programmatic navigate, browser Back) #805 exists to guard. The
    // navigate promise stays pending while blocked, so it must not be awaited.
    await act(async () => {
      router.navigate('/away');
    });

    expect(router.state.location.pathname).toBe('/home');
    expect(screen.getByText('blocked:yes')).toBeTruthy();
  });

  test('cancel() keeps the current route and clears the blocked flag', async () => {
    const router = makeRouter(true);
    render(<RouterProvider router={router} />);

    await act(async () => {
      router.navigate('/away');
    });
    expect(screen.getByText('blocked:yes')).toBeTruthy();

    fireEvent.click(screen.getByText('cancel'));

    expect(router.state.location.pathname).toBe('/home');
    expect(screen.queryByText('blocked:yes')).toBeNull();
  });

  test('proceed() lets the suspended navigation through', async () => {
    const router = makeRouter(true);
    render(<RouterProvider router={router} />);

    await act(async () => {
      router.navigate('/away');
    });
    expect(screen.getByText('blocked:yes')).toBeTruthy();

    fireEvent.click(screen.getByText('discard and leave'));

    expect(router.state.location.pathname).toBe('/away');
    expect(screen.queryByText('blocked:yes')).toBeNull();
  });
});

// Runs the hook inside a rendered component with no router at all and returns
// what the hook returned (the isolated-render degrade path).
function captureGuard() {
  let captured: ReturnType<typeof useNavigationGuard> | undefined;
  function Capture() {
    captured = useNavigationGuard(false);
    return null;
  }
  render(<Capture />);
  if (!captured) throw new Error('hook did not run');
  return captured;
}
