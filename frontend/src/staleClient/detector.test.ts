import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import type { HealthResponse } from '../api/health';
import { getHealth } from '../api/health';
import { reportDirty, resetDirtyTrackerForTest, nextDirtyKey } from './dirty';
import {
  checkNow,
  dismissBlockedNotice,
  onStaleClientNotice,
  reloadBlockedClient,
  resetStaleClientDetectorForTest,
  startStaleClientDetector,
  stopStaleClientDetector,
  type BlockNotice,
} from './detector';
import { forceReloadToCurrentBuild, hasRecentlyForcedReload } from './reload';
import {
  resetClientBuildIdentityForTest,
  setClientBuildIdentityForTest,
} from './version';

vi.mock('../api/health', () => ({ getHealth: vi.fn() }));
vi.mock('./reload', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./reload')>();
  return {
    ...actual,
    forceReloadToCurrentBuild: vi.fn(async () => {}),
    hasRecentlyForcedReload: vi.fn(() => false),
  };
});

const mockedGetHealth = vi.mocked(getHealth);
const mockedForceReload = vi.mocked(forceReloadToCurrentBuild);
const mockedRecentlyReloaded = vi.mocked(hasRecentlyForcedReload);

function healthBody(overrides: Partial<HealthResponse> = {}): HealthResponse {
  return {
    status: 'healthy',
    timestamp: '2026-09-06T00:00:00Z',
    database: { status: 'healthy', response_time_ms: 1 },
    version: '0.6.10',
    api_contract_version: 'v1',
    ...overrides,
  };
}

function subscribe() {
  const notices: BlockNotice[] = [];
  const unsubscribe = onStaleClientNotice((notice) => notices.push(notice));
  return { notices, unsubscribe };
}

beforeEach(() => {
  setClientBuildIdentityForTest('0.6.8');
  mockedRecentlyReloaded.mockReturnValue(false);
  mockedForceReload.mockResolvedValue(undefined);
});

afterEach(() => {
  resetStaleClientDetectorForTest();
  resetDirtyTrackerForTest();
  resetClientBuildIdentityForTest();
  vi.clearAllMocks();
});

describe('startStaleClientDetector', () => {
  test('is a no-op on an unversioned (dev/test) build', () => {
    resetClientBuildIdentityForTest();
    expect(startStaleClientDetector()).toBe(false);
    expect(mockedGetHealth).not.toHaveBeenCalled();
  });

  test('starts and runs an immediate contract check on a stamped build', async () => {
    mockedGetHealth.mockResolvedValue(healthBody({ version: '0.6.8' }));
    expect(startStaleClientDetector()).toBe(true);
    await vi.waitFor(() => expect(mockedGetHealth).toHaveBeenCalled());
    stopStaleClientDetector();
  });
});

describe('checkNow — severities', () => {
  test('compatible: no notice, no reload', async () => {
    const { notices } = subscribe();
    mockedGetHealth.mockResolvedValue(healthBody({ version: '0.6.8' }));

    await checkNow();

    expect(notices).toEqual([]);
    expect(mockedForceReload).not.toHaveBeenCalled();
  });

  test('below-floor with a clean tab: automatic reload is triggered', async () => {
    const { notices } = subscribe();
    mockedGetHealth.mockResolvedValue(
      healthBody({ version: '0.6.10', min_client_version: '0.6.10' }),
    );

    await checkNow();

    expect(mockedForceReload).toHaveBeenCalledTimes(1);
    expect(notices).toEqual([
      { kind: 'blocked', reason: 'below-floor', dirty: false, autoReloading: true },
    ]);
  });

  test('contract mismatch with a clean tab: automatic reload is triggered', async () => {
    mockedGetHealth.mockResolvedValue(healthBody({ api_contract_version: 'v2' }));

    await checkNow();

    expect(mockedForceReload).toHaveBeenCalledTimes(1);
  });

  test('below-floor with a dirty form: no silent reload, consent notice shown', async () => {
    const dirtyKey = nextDirtyKey('note');
    reportDirty(dirtyKey, true);
    const { notices } = subscribe();
    mockedGetHealth.mockResolvedValue(
      healthBody({ version: '0.6.10', min_client_version: '0.6.10' }),
    );

    await checkNow();

    expect(mockedForceReload).not.toHaveBeenCalled();
    expect(notices).toEqual([
      { kind: 'blocked', reason: 'below-floor', dirty: true, autoReloading: false },
    ]);
    reportDirty(dirtyKey, false);
  });

  test('a block that appeared while dirty auto-reloads once the form is saved', async () => {
    const dirtyKey = nextDirtyKey('note');
    reportDirty(dirtyKey, true);
    const { notices } = subscribe();
    mockedGetHealth.mockResolvedValue(
      healthBody({ version: '0.6.10', min_client_version: '0.6.10' }),
    );

    startStaleClientDetector();
    await vi.waitFor(() => expect(notices.length).toBeGreaterThan(0));
    expect(mockedForceReload).not.toHaveBeenCalled();

    reportDirty(dirtyKey, false);

    await vi.waitFor(() => expect(mockedForceReload).toHaveBeenCalledTimes(1));
    stopStaleClientDetector();
  });

  test('network error fails open: no notice, no reload', async () => {
    const { notices } = subscribe();
    mockedGetHealth.mockRejectedValue(new Error('offline'));

    await checkNow();

    expect(notices).toEqual([]);
    expect(mockedForceReload).not.toHaveBeenCalled();
  });

  test('update-available: no reload, but the service worker update check is kicked', async () => {
    const update = vi.fn(async () => {});
    vi.stubGlobal('navigator', {
      serviceWorker: {
        getRegistration: vi.fn(async () => ({ waiting: null, update })),
      },
    });
    mockedGetHealth.mockResolvedValue(healthBody({ version: '0.6.10' }));

    await checkNow();

    expect(mockedForceReload).not.toHaveBeenCalled();
    expect(update).toHaveBeenCalledTimes(1);
    vi.unstubAllGlobals();
  });

  test('update-available does not kick an update when a worker is already waiting', async () => {
    const update = vi.fn(async () => {});
    vi.stubGlobal('navigator', {
      serviceWorker: {
        getRegistration: vi.fn(async () => ({ waiting: { postMessage: vi.fn() }, update })),
      },
    });
    mockedGetHealth.mockResolvedValue(healthBody({ version: '0.6.10' }));

    await checkNow();

    expect(update).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  test('a block is cleared once the server reports compatible again', async () => {
    const { notices } = subscribe();
    mockedGetHealth.mockResolvedValue(healthBody({ min_client_version: '0.6.10' }));
    await checkNow();
    expect(notices.length).toBe(1);

    mockedGetHealth.mockResolvedValue(healthBody({ version: '0.6.8' }));
    await checkNow();
    expect(notices).toEqual([notices[0], { kind: 'none' }]);
  });
});

describe('loop guard and consent', () => {
  test('a second block right after a forced reload shows the manual dialog, no auto-loop', async () => {
    const { notices } = subscribe();
    mockedRecentlyReloaded.mockReturnValue(true);
    mockedGetHealth.mockResolvedValue(healthBody({ min_client_version: '0.6.10' }));

    await checkNow();

    expect(mockedForceReload).not.toHaveBeenCalled();
    expect(notices).toEqual([
      { kind: 'blocked', reason: 'below-floor', dirty: false, autoReloading: false },
    ]);
  });

  test('dismissBlockedNotice suppresses further automatic reloads for a while', async () => {
    const { notices } = subscribe();
    // Loop-guard mode (an earlier reload did not converge) so the first block
    // is the manual dialog, not an auto-reload we would then have to subtract.
    mockedRecentlyReloaded.mockReturnValue(true);
    mockedGetHealth.mockResolvedValue(healthBody({ min_client_version: '0.6.10' }));

    await checkNow();
    expect(notices.at(-1)).toEqual({
      kind: 'blocked',
      reason: 'below-floor',
      dirty: false,
      autoReloading: false,
    });
    expect(mockedForceReload).not.toHaveBeenCalled();

    dismissBlockedNotice();
    expect(notices.at(-1)).toEqual({ kind: 'none' });

    await checkNow();
    expect(mockedForceReload).not.toHaveBeenCalled();
    expect(notices.at(-1)).toEqual({ kind: 'none' });
  });

  test('reloadBlockedClient always reloads, even with a dirty form (explicit consent)', async () => {
    const dirtyKey = nextDirtyKey('note');
    reportDirty(dirtyKey, true);
    mockedGetHealth.mockResolvedValue(healthBody({ min_client_version: '0.6.10' }));

    await checkNow();
    expect(mockedForceReload).not.toHaveBeenCalled();

    reloadBlockedClient();
    expect(mockedForceReload).toHaveBeenCalledTimes(1);
    reportDirty(dirtyKey, false);
  });
});
