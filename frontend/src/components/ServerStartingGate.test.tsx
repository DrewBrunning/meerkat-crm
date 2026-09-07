import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { AppThemeProvider } from '../AppThemeProvider';
import ServerStartingGate from './ServerStartingGate';
import '../i18n/config';
import type { ServerReadiness } from '../readiness/readiness';

// The gate is a thin state machine over probeServerReadiness, so its test
// drives a controllable mock rather than real fetches. Each call to the mock
// returns a promise this file resolves in order, which lets a test say "the
// first probe says starting, then after the poll interval it says ready".
const gate = vi.hoisted(() => {
  const resolvers: Array<(value: ServerReadiness) => void> = [];
  const probe = vi.fn(
    () =>
      new Promise<ServerReadiness>((resolve) => {
        resolvers.push(resolve);
      }),
  );
  return { probe, resolvers };
});

vi.mock('../readiness/readiness', () => ({
  probeServerReadiness: gate.probe,
  READY_POLL_MS: 3_000,
}));

const APP_MARKER = 'real app content';

function renderGate() {
  return render(
    <AppThemeProvider>
      <ServerStartingGate>
        <div>{APP_MARKER}</div>
      </ServerStartingGate>
    </AppThemeProvider>,
  );
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
  });
}

function resolveProbe(index: number, value: ServerReadiness) {
  act(() => gate.resolvers[index](value));
  return flush();
}

function clickRetry() {
  act(() => {
    screen.getByRole('button', { name: 'Try again' }).click();
  });
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  gate.resolvers.length = 0;
  vi.useRealTimers();
});

describe('ServerStartingGate', () => {
  test('mounts the app once the first probe reports ready', async () => {
    renderGate();
    // Before the probe resolves, the starting-up surface is shown and the app
    // is held back (no failed requests, no partial mount).
    expect(screen.queryByText(APP_MARKER)).not.toBeInTheDocument();
    expect(screen.getByRole('status')).toBeInTheDocument();

    await resolveProbe(0, 'ready');

    expect(screen.getByText(APP_MARKER)).toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  test('fails open: an unknown first probe mounts the app as if the gate did not exist', async () => {
    renderGate();
    await resolveProbe(0, 'unknown');
    expect(screen.getByText(APP_MARKER)).toBeInTheDocument();
  });

  test('an authoritative not-ready shows the starting-up state and recovers on the next poll', async () => {
    renderGate();
    await resolveProbe(0, 'starting');

    // The server is up-but-not-ready (mid-migration): a clear starting-up state,
    // not a wall of failed requests -- the app content is nowhere to be seen.
    expect(screen.queryByText(APP_MARKER)).not.toBeInTheDocument();
    expect(screen.getByRole('status')).toBeInTheDocument();
    expect(screen.getByTestId('server-starting-message')).toBeInTheDocument();

    // The gate re-probes after the poll interval; once the server is ready the
    // app mounts.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000);
    });
    await resolveProbe(1, 'ready');
    expect(screen.getByText(APP_MARKER)).toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  test('Try again after a not-ready answer re-probes immediately and mounts on ready', async () => {
    renderGate();
    await resolveProbe(0, 'starting');
    expect(screen.getByRole('status')).toBeInTheDocument();

    clickRetry();
    await resolveProbe(1, 'ready');

    expect(screen.getByText(APP_MARKER)).toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  test('unmounting while the server is starting cancels the readiness poll', async () => {
    renderGate();
    await resolveProbe(0, 'starting');

    // Unmount the gate (the page navigated away, or the app was torn down)
    // while a poll was pending: the scheduled re-probe must be cancelled.
    act(() => cleanup());
    gate.probe.mockClear();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000);
    });
    expect(gate.probe, 'a cancelled gate must not keep polling after unmount').not.toHaveBeenCalled();
  });

  test('a manual retry that is still not ready keeps the starting-up state up', async () => {
    renderGate();
    await resolveProbe(0, 'starting');

    clickRetry();
    await resolveProbe(1, 'starting');

    expect(screen.queryByText(APP_MARKER)).not.toBeInTheDocument();
    expect(screen.getByTestId('server-starting-message')).toBeInTheDocument();

    // ...and the poll loop is still alive: the server recovering later mounts
    // the app without another user action.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000);
    });
    await resolveProbe(2, 'ready');
    expect(screen.getByText(APP_MARKER)).toBeInTheDocument();
  });
});
