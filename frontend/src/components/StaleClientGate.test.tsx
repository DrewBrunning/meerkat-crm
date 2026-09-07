import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import { AppThemeProvider } from '../AppThemeProvider';
import '../i18n/config';
import type { BlockNotice } from '../staleClient/detector';
import StaleClientGate from './StaleClientGate';

// The gate is a pure view over the stale-client detector's notice bus, so its
// test drives the bus mock directly rather than exercising the detector.
const detector = vi.hoisted(() => ({
  listener: null as null | ((notice: BlockNotice) => void),
  dismissBlockedNotice: vi.fn(),
  reloadBlockedClient: vi.fn(),
}));

vi.mock('../staleClient/detector', () => ({
  onStaleClientNotice: (fn: (notice: BlockNotice) => void) => {
    detector.listener = fn;
    return () => {
      detector.listener = null;
    };
  },
  dismissBlockedNotice: detector.dismissBlockedNotice,
  reloadBlockedClient: detector.reloadBlockedClient,
}));

function pushNotice(notice: BlockNotice) {
  act(() => detector.listener?.(notice));
}

function blocked(dirty: boolean, autoReloading = false): BlockNotice {
  return { kind: 'blocked', reason: 'below-floor', dirty, autoReloading };
}

function renderGate() {
  return render(
    <AppThemeProvider>
      <StaleClientGate />
    </AppThemeProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('StaleClientGate', () => {
  test('renders nothing before any block is detected', () => {
    renderGate();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  test('renders nothing while a clean automatic reload is in flight', () => {
    renderGate();
    pushNotice(blocked(false, true));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  test('a dirty block shows the consent dialog and never reloads silently', () => {
    renderGate();
    pushNotice(blocked(true));

    expect(screen.getByText('Incompatible version')).toBeInTheDocument();
    expect(screen.getByText(/unsaved changes/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Reload now' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Later' })).toBeInTheDocument();
  });

  test('Later on a dirty block dismisses without reloading', () => {
    renderGate();
    pushNotice(blocked(true));

    screen.getByRole('button', { name: 'Later' }).click();

    expect(detector.dismissBlockedNotice).toHaveBeenCalledTimes(1);
    expect(detector.reloadBlockedClient).not.toHaveBeenCalled();
  });

  test('Reload now on a dirty block is the explicit discard path', () => {
    renderGate();
    pushNotice(blocked(true));

    screen.getByRole('button', { name: 'Reload now' }).click();

    expect(detector.reloadBlockedClient).toHaveBeenCalledTimes(1);
  });

  test('a clean block that already exhausted its automatic attempt is a blocking dialog', () => {
    renderGate();
    pushNotice(blocked(false));

    expect(screen.getByText('Incompatible version')).toBeInTheDocument();
    expect(screen.getByText(/no longer supported/)).toBeInTheDocument();
    // Only one way out — reload.
    expect(screen.getByRole('button', { name: 'Reload now' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Later' })).not.toBeInTheDocument();

    screen.getByRole('button', { name: 'Reload now' }).click();
    expect(detector.reloadBlockedClient).toHaveBeenCalledTimes(1);
  });

  test('hides once the detector reports the block cleared', () => {
    renderGate();
    pushNotice(blocked(false));
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    pushNotice({ kind: 'none' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
