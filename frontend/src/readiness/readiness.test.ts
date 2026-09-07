// Readiness probe unit tests (issue #477, WEB-03). The pure classification is
// the load-bearing part: only an *authoritative* not-ready signal may hold the
// app back, and everything ambiguous must fail open ('unknown' mounts the app
// as if this module did not exist).
import { afterEach, describe, expect, test, vi } from 'vitest';
import { classifyReadiness, probeServerReadiness, READY_PROBE_TIMEOUT_MS } from './readiness';

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe('classifyReadiness', () => {
  test('a 200 with status ready is ready', async () => {
    const response = jsonResponse({ status: 'ready', checks: {} }, 200);
    expect(await classifyReadiness(response)).toBe('ready');
  });

  test('a 200 with whitespace around status ready is ready', async () => {
    const response = jsonResponse({ status: '  ready  ' }, 200);
    expect(await classifyReadiness(response)).toBe('ready');
  });

  test('a 503 not_ready (the backend mid-migration readiness answer) is starting', async () => {
    const response = jsonResponse(
      {
        status: 'not_ready',
        checks: { migrations: { status: 'failed', reason: 'pending migrations' } },
      },
      503,
    );
    expect(await classifyReadiness(response)).toBe('starting');
  });

  test('a 502 (nginx in front of a backend that is not listening yet) is starting', async () => {
    const response = new Response('Bad Gateway', { status: 502 });
    expect(await classifyReadiness(response)).toBe('starting');
  });

  test('a 504 (proxy timeout while the backend boots) is starting', async () => {
    const response = new Response('Gateway Timeout', { status: 504 });
    expect(await classifyReadiness(response)).toBe('starting');
  });

  test('a 200 not_ready body is starting even without a 503 status', async () => {
    const response = jsonResponse({ status: 'not_ready' }, 200);
    expect(await classifyReadiness(response)).toBe('starting');
  });

  test('a non-JSON 200 (the SPA shell served by a fallback route) fails open as unknown', async () => {
    const response = new Response('<!doctype html><html>app shell</html>', {
      status: 200,
      headers: { 'Content-Type': 'text/html' },
    });
    expect(await classifyReadiness(response)).toBe('unknown');
  });

  test('an unparseable 200 JSON body fails open as unknown', async () => {
    const response = new Response('{not json', { status: 200 });
    expect(await classifyReadiness(response)).toBe('unknown');
  });

  test('a 200 with an unknown status field fails open as unknown', async () => {
    const response = jsonResponse({ status: 'healthy' }, 200);
    expect(await classifyReadiness(response)).toBe('unknown');
  });

  test('a 404 fails open as unknown (misrouted proxy, not a readiness answer)', async () => {
    const response = new Response('not found', { status: 404 });
    expect(await classifyReadiness(response)).toBe('unknown');
  });

  test('a real 500 fails open as unknown (never hold the app back on a bug)', async () => {
    const response = new Response('boom', { status: 500 });
    expect(await classifyReadiness(response)).toBe('unknown');
  });

  test('an empty body on a 200 fails open as unknown', async () => {
    const response = new Response('', { status: 200 });
    expect(await classifyReadiness(response)).toBe('unknown');
  });
});

describe('probeServerReadiness', () => {
  test('classifies a ready response from the injected fetch', async () => {
    const fetchImpl = vi.fn(async (_url: string, _init?: RequestInit) =>
      jsonResponse({ status: 'ready' }, 200),
    );
    expect(await probeServerReadiness(fetchImpl)).toBe('ready');
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    // The probe must not cache the result -- a long-lived tab through a deploy
    // must re-read readiness on every poll.
    const options = fetchImpl.mock.calls[0][1] as RequestInit;
    expect(options.cache).toBe('no-store');
  });

  test('a network failure (offline) fails open as unknown', async () => {
    const fetchImpl = vi.fn(async (_url: string, _init?: RequestInit) => {
      throw new Error('network down');
    });
    expect(await probeServerReadiness(fetchImpl)).toBe('unknown');
  });

  test('a fetch that aborts on the probe timeout fails open as unknown', async () => {
    vi.useFakeTimers();
    const fetchImpl = vi.fn(
      (_url: string, options?: RequestInit) =>
        new Promise<never>((_resolve, reject) => {
          options?.signal?.addEventListener('abort', () =>
            reject(new DOMException('Aborted', 'AbortError')),
          );
        }),
    );
    const pending = probeServerReadiness(fetchImpl);
    await vi.advanceTimersByTimeAsync(READY_PROBE_TIMEOUT_MS + 1);
    expect(await pending).toBe('unknown');
  });
});
