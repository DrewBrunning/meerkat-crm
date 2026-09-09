import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import '../i18n/config';
import { SnackbarProvider } from '../context/SnackbarContext';
import SessionsSettings from './SessionsSettings';

beforeEach(() => {
  localStorage.setItem(
    'user_info',
    JSON.stringify({ user_id: 1, username: 'test', is_admin: false }),
  );
});

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.unstubAllGlobals();
});

type MockResponse = unknown | { body: unknown; ok?: boolean };

function mockFetchByUrl(
  handlers: Record<string, (method: string, init?: RequestInit) => MockResponse>,
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = (init?.method || 'GET').toUpperCase();
      for (const [pattern, respond] of Object.entries(handlers)) {
        const [pMethod, pUrl] = pattern.includes(' ') ? pattern.split(' ') : ['', pattern];
        if (url.includes(pUrl) && (!pMethod || pMethod === method)) {
          const result = respond(method, init);
          const body =
            result && typeof result === 'object' && 'body' in result ? result.body : result;
          const ok = result && typeof result === 'object' && 'ok' in result ? !!result.ok : true;
          return { ok, json: async () => body, text: async () => JSON.stringify(body) };
        }
      }
      throw new Error(`unexpected fetch: ${method} ${url}`);
    }),
  );
}

const twoSessions = {
  sessions: [
    {
      id: 'sid-current',
      created_at: '2026-09-01T10:00:00Z',
      last_seen_at: '2026-09-09T09:00:00Z',
      expires_at: '2026-09-13T10:00:00Z',
      user_agent: 'Firefox on Linux',
      ip: '203.0.113.4',
      current: true,
    },
    {
      id: 'sid-phone',
      created_at: '2026-09-05T08:00:00Z',
      last_seen_at: '2026-09-08T20:00:00Z',
      expires_at: '2026-09-17T08:00:00Z',
      user_agent: 'Safari on iPhone',
      ip: '198.51.100.9',
      current: false,
    },
  ],
};

test('lists sessions and marks the current one', async () => {
  mockFetchByUrl({ 'GET /sessions': () => twoSessions });

  render(
    <SnackbarProvider>
      <SessionsSettings />
    </SnackbarProvider>,
  );

  await waitFor(() => expect(screen.getByText('Active sessions')).toBeInTheDocument());
  expect(screen.getByText('Firefox on Linux')).toBeInTheDocument();
  expect(screen.getByText('Safari on iPhone')).toBeInTheDocument();
  expect(screen.getByText('This device')).toBeInTheDocument();
});

test('revoking a session calls the API and refetches', async () => {
  const getHandler = vi.fn(() => twoSessions);
  const delHandler = vi.fn(() => ({ message: 'Session revoked' }));
  mockFetchByUrl({
    'GET /sessions': getHandler,
    'DELETE /sessions/sid-phone': delHandler,
  });

  render(
    <SnackbarProvider>
      <SessionsSettings />
    </SnackbarProvider>,
  );

  await waitFor(() => expect(screen.getByText('Safari on iPhone')).toBeInTheDocument());

  const phoneRow = screen.getByText('Safari on iPhone').closest('tr') as HTMLElement;
  fireEvent.click(within(phoneRow).getByRole('button', { name: 'Revoke' }));

  await waitFor(() => expect(delHandler).toHaveBeenCalledTimes(1));
  // one initial load + one refetch after the revoke
  await waitFor(() => expect(getHandler).toHaveBeenCalledTimes(2));
});

test('"log out all other devices" is shown only when there is another session', async () => {
  mockFetchByUrl({
    'GET /sessions': () => ({ sessions: [twoSessions.sessions[0]] }),
  });

  render(
    <SnackbarProvider>
      <SessionsSettings />
    </SnackbarProvider>,
  );

  await waitFor(() => expect(screen.getByText('Firefox on Linux')).toBeInTheDocument());
  expect(screen.queryByText('Log out all other devices')).not.toBeInTheDocument();
});
