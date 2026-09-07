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

interface FakeWorker {
  state: string;
  postMessage: ReturnType<typeof vi.fn>;
  listeners: Record<string, Array<() => void>>;
  addEventListener(type: string, fn: () => void): void;
  removeEventListener(type: string, fn: () => void): void;
  fire(type: string): void;
}

function fakeWorker(state: string): FakeWorker {
  const listeners: Record<string, Array<() => void>> = {};
  return {
    state,
    postMessage: vi.fn(),
    listeners,
    addEventListener(type, fn) {
      if (!listeners[type]) {
        listeners[type] = [];
      }
      listeners[type].push(fn);
    },
    removeEventListener(type, fn) {
      listeners[type] = (listeners[type] ?? []).filter((f) => f !== fn);
    },
    fire(type) {
      for (const fn of listeners[type] ?? []) {
        fn();
      }
    },
  };
}

interface FakeRegistration {
  waiting: FakeWorker | null;
  installing: FakeWorker | null;
  update: ReturnType<typeof vi.fn>;
}

function fakeRegistration(overrides: Partial<FakeRegistration>): FakeRegistration {
  return {
    waiting: null,
    installing: null,
    update: vi.fn(async () => {}),
    ...overrides,
  };
}

function stubEnvironment(registration: FakeRegistration | null) {
  const reload = vi.fn();
  let stored: string | null = null;

  vi.stubGlobal('navigator', {
    serviceWorker: {
      getRegistration: vi.fn(async () => registration),
    },
  });
  vi.stubGlobal('window', {
    location: { reload },
    setTimeout: (_fn: () => void, _ms?: number) => 0,
    clearTimeout: () => {},
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
    const { reload } = stubEnvironment(
      fakeRegistration({ waiting: fakeWorker('installed'), update: vi.fn() }),
    );
    await forceReloadToCurrentBuild();

    // The old worker still controls the page; reloading now would re-serve the
    // stale cache. applyUpdate does the SKIP_WAITING + controllerchange dance.
    expect(reload).not.toHaveBeenCalled();
    expect(applyUpdate).toHaveBeenCalledTimes(1);
  });

  test('asks the browser for a new worker when none is installing or waiting yet', async () => {
    const registration = fakeRegistration({});
    registration.update.mockImplementation(async () => {
      registration.waiting = fakeWorker('installed');
    });
    stubEnvironment(registration);

    await forceReloadToCurrentBuild();

    expect(registration.update).toHaveBeenCalledTimes(1);
    expect(applyUpdate).toHaveBeenCalledTimes(1);
  });

  test('waits for an installing worker to reach waiting before swapping', async () => {
    const installing = fakeWorker('installing');
    const registration = fakeRegistration({ installing });
    stubEnvironment(registration);

    const pending = forceReloadToCurrentBuild();
    // Let the routine reach the wait before the install finishes.
    await Promise.resolve();
    await Promise.resolve();

    // update() must not run (a worker is already on its way) and applyUpdate
    // must not fire before the install has landed in the waiting state.
    expect(registration.update).not.toHaveBeenCalled();
    expect(applyUpdate).not.toHaveBeenCalled();

    // The install finishes: the worker moves from installing to waiting.
    registration.waiting = installing;
    installing.fire('statechange');
    await pending;

    expect(applyUpdate).toHaveBeenCalledTimes(1);
  });

  test('falls back to a plain reload when the install fails before reaching waiting', async () => {
    const installing = fakeWorker('installing');
    const registration = fakeRegistration({ installing });
    const { reload } = stubEnvironment(registration);

    const pending = forceReloadToCurrentBuild();
    // Let the routine reach the wait (the getRegistration await resolves after
    // a microtask) before tearing the worker down.
    await Promise.resolve();
    await Promise.resolve();

    registration.installing = null;
    installing.fire('statechange');
    await pending;

    // The install never produced a waiting worker, so the only remaining move
    // is a plain reload.
    expect(applyUpdate).not.toHaveBeenCalled();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  test('falls back to a plain reload when update() finds no new worker', async () => {
    const { reload } = stubEnvironment(fakeRegistration({}));
    await forceReloadToCurrentBuild();

    expect(reload).toHaveBeenCalledTimes(1);
    expect(applyUpdate).not.toHaveBeenCalled();
  });

  test('leaves the block visible (no reload) when update() fails — fail open', async () => {
    const { reload } = stubEnvironment(
      fakeRegistration({
        update: vi.fn(async () => {
          throw new Error('offline');
        }),
      }),
    );
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
