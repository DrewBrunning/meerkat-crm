import { afterEach, describe, expect, test, vi } from 'vitest';
import { applyUpdate } from '../serviceWorkerUpdates';
import {
  forceReloadToCurrentBuild,
  hasRecentlyForcedReload,
  markForcedReload,
  RELOAD_COOLDOWN_MS,
} from './reload';

// The reload module drives real navigations through the service worker, so
// its tests stub navigator.serviceWorker + window.location and assert the
// sequence of calls, exactly like serviceWorkerUpdates.test.ts stubs the same
// surface for applyUpdate.

vi.mock('../serviceWorkerUpdates', () => ({ applyUpdate: vi.fn() }));

type Registration = {
  waiting: { postMessage: (m: unknown) => void } | null;
  update: ReturnType<typeof vi.fn>;
};

function stubEnvironment(registration: Registration | null) {
  const reload = vi.fn();
  let stored: string | null = null;

  vi.stubGlobal('navigator', {
    serviceWorker: {
      getRegistration: vi.fn(async () => registration),
    },
  });
  vi.stubGlobal('window', {
    location: { reload },
    setTimeout: () => 0,
    sessionStorage: {
      getItem: vi.fn(() => stored),
      setItem: vi.fn((_key: string, value: string) => {
        stored = value;
      }),
    },
  });
  return { reload };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  vi.useRealTimers();
});

describe('hasRecentlyForcedReload / markForcedReload (loop guard)', () => {
  test('is false until a reload is marked', () => {
    expect(hasRecentlyForcedReload()).toBe(false);
  });

  test('is true right after markForcedReload', () => {
    markForcedReload();
    expect(hasRecentlyForcedReload()).toBe(true);
  });

  test('expires after the cooldown window', () => {
    vi.useFakeTimers();
    markForcedReload();
    vi.advanceTimersByTime(RELOAD_COOLDOWN_MS);
    expect(hasRecentlyForcedReload()).toBe(false);
  });
});

describe('forceReloadToCurrentBuild', () => {
  test('reloads immediately when the browser has no service worker', async () => {
    const { reload } = stubEnvironment(null);
    vi.stubGlobal('navigator', {});
    await forceReloadToCurrentBuild();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  test('reloads immediately when there is no registration', async () => {
    const { reload } = stubEnvironment(null);
    await forceReloadToCurrentBuild();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  test('hands a waiting worker control instead of a bare reload', async () => {
    const { reload } = stubEnvironment({ waiting: { postMessage: vi.fn() }, update: vi.fn() });
    await forceReloadToCurrentBuild();

    // The old worker still controls the page; reloading now would re-serve the
    // stale cache. applyUpdate does the SKIP_WAITING + controllerchange dance.
    expect(reload).not.toHaveBeenCalled();
    expect(applyUpdate).toHaveBeenCalledTimes(1);
  });

  test('asks the browser for a new worker when none is waiting yet', async () => {
    const registration: Registration = { waiting: null, update: vi.fn() };
    registration.update.mockImplementation(async () => {
      registration.waiting = { postMessage: vi.fn() };
    });
    stubEnvironment(registration);

    await forceReloadToCurrentBuild();

    expect(registration.update).toHaveBeenCalledTimes(1);
    expect(applyUpdate).toHaveBeenCalledTimes(1);
  });

  test('falls back to a plain reload when update() finds no new worker', async () => {
    const { reload } = stubEnvironment({ waiting: null, update: vi.fn(async () => {}) });
    await forceReloadToCurrentBuild();

    expect(reload).toHaveBeenCalledTimes(1);
    expect(applyUpdate).not.toHaveBeenCalled();
  });

  test('leaves the block visible (no reload) when update() fails — fail open', async () => {
    const { reload } = stubEnvironment({
      waiting: null,
      update: vi.fn(async () => {
        throw new Error('offline');
      }),
    });
    await forceReloadToCurrentBuild();

    // Failing open: no blind reload onto a cache that would re-serve the same
    // stale build; the caller keeps its block visible and retries later.
    expect(reload).not.toHaveBeenCalled();
    expect(applyUpdate).not.toHaveBeenCalled();
  });

  test('falls back to a plain reload when the service worker API throws', async () => {
    const { reload } = stubEnvironment(null);
    vi.stubGlobal('navigator', {
      serviceWorker: {
        getRegistration: vi.fn(async () => {
          throw new Error('no sw');
        }),
      },
    });
    await forceReloadToCurrentBuild();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  test('marks the loop guard before reloading', async () => {
    stubEnvironment(null);
    await forceReloadToCurrentBuild();
    expect(hasRecentlyForcedReload()).toBe(true);
  });
});
