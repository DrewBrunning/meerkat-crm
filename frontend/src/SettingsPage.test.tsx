import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import './i18n/config';
import { AppThemeProvider } from './AppThemeProvider';
import { getCurrentUser } from './api/admin';
import {
  type ApiToken,
  createApiToken,
  getApiTokens,
  revokeAllApiTokens,
  revokeApiToken,
  rotateApiToken,
} from './api/apiTokens';
import { changePassword } from './api/auth';
import { type Contact, getContacts, getContactsByUid } from './api/contacts';
import { updateSelfContact } from './api/users';
import { fetchAndCacheUserInfo, isAdmin } from './auth';
import { SnackbarProvider } from './context/SnackbarContext';
import { DateFormatProvider } from './DateFormatProvider';
import SettingsPage from './SettingsPage';

// This codebase's vitest setup has no auto-cleanup and no globals: true.
afterEach(cleanup);

// These child sections are separately tested (their own *.test.tsx files) and
// each fires its own config fetch on mount — stub them out so this file can
// focus on password change and API tokens without also mocking their unrelated
// endpoints. Left unmocked, their mount-time fetches reject under jsdom and the
// async `handleFetchError` console.error lands during vitest worker teardown,
// surfacing as `EnvironmentTeardownError: Closing rpc while "onUserConsoleLog"
// was pending` and failing the run even though every assertion passed.
vi.mock('./components/WebhooksSettings', () => ({ default: () => null }));
vi.mock('./components/ImmichSettings', () => ({ default: () => null }));
vi.mock('./components/NotificationSettings', () => ({ default: () => null }));
vi.mock('./components/PaperlessSettings', () => ({ default: () => null }));
vi.mock('./components/SeafileSettings', () => ({ default: () => null }));
vi.mock('./components/NextcloudSettings', () => ({ default: () => null }));
vi.mock('./components/LinkFieldTypesSettings', () => ({ default: () => null }));

vi.mock('./api/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/auth')>();
  return { ...actual, changePassword: vi.fn() };
});
vi.mock('./api/apiTokens', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/apiTokens')>();
  return {
    ...actual,
    getApiTokens: vi.fn(),
    createApiToken: vi.fn(),
    revokeApiToken: vi.fn(),
    revokeAllApiTokens: vi.fn(),
    rotateApiToken: vi.fn(),
  };
});
// T90 self-contact picker: these modules fire on mount / on interaction.
vi.mock('./api/contacts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/contacts')>();
  return { ...actual, getContacts: vi.fn(), getContactsByUid: vi.fn() };
});
vi.mock('./api/users', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/users')>();
  return { ...actual, updateSelfContact: vi.fn() };
});
vi.mock('./api/admin', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/admin')>();
  return { ...actual, getCurrentUser: vi.fn() };
});
vi.mock('./auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./auth')>();
  return { ...actual, fetchAndCacheUserInfo: vi.fn(), isAdmin: vi.fn() };
});

// jsdom doesn't implement matchMedia; AppThemeProvider's system-theme
// listener needs it to exist.
beforeEach(() => {
  window.matchMedia =
    window.matchMedia ||
    (() =>
      ({
        matches: false,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
      }) as unknown as MediaQueryList);

  vi.mocked(changePassword).mockReset();
  vi.mocked(getApiTokens).mockReset();
  vi.mocked(createApiToken).mockReset();
  vi.mocked(revokeApiToken).mockReset();
  vi.mocked(revokeAllApiTokens).mockReset();
  vi.mocked(rotateApiToken).mockReset();
  vi.mocked(getApiTokens).mockResolvedValue({ tokens: [] });

  // T90: reset the picker mocks. Default /users/me has no self contact and
  // no contacts resolve.
  vi.mocked(getCurrentUser).mockReset();
  vi.mocked(getCurrentUser).mockResolvedValue({
    id: 1,
    email: 'tester@example.com',
    username: 'tester',
    language: 'en',
    is_admin: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    self_contact_vcard_uid: null,
  });
  vi.mocked(getContacts).mockReset();
  vi.mocked(getContactsByUid).mockReset();
  vi.mocked(getContactsByUid).mockResolvedValue(new Map());
  vi.mocked(updateSelfContact).mockReset();
  vi.mocked(fetchAndCacheUserInfo).mockReset();
  vi.mocked(isAdmin).mockReset();
  vi.mocked(isAdmin).mockReturnValue(false);
});

function apiToken(overrides: Partial<ApiToken> = {}): ApiToken {
  return {
    id: 1,
    name: 'CI token',
    created_at: '2026-01-01T00:00:00Z',
    last_used_at: null,
    revoked_at: null,
    expires_at: null,
    scope: 'full',
    ...overrides,
  };
}

function contact(overrides: Partial<Contact> = {}): Contact {
  return { ID: 1, firstname: 'First', lastname: 'Last', ...overrides };
}

function renderPage() {
  return render(
    <AppThemeProvider>
      <DateFormatProvider>
        <SnackbarProvider>
          <SettingsPage />
        </SnackbarProvider>
      </DateFormatProvider>
    </AppThemeProvider>,
  );
}

// --- Password change ---------------------------------------------------

test('changing the password submits current/new and shows the success message', async () => {
  vi.mocked(changePassword).mockResolvedValue('Password updated successfully.');
  renderPage();

  fireEvent.change(screen.getByLabelText('Current password *'), { target: { value: 'old-pass' } });
  fireEvent.change(screen.getByLabelText('New password *'), { target: { value: 'new-pass-123' } });
  fireEvent.change(screen.getByLabelText('Confirm new password *'), {
    target: { value: 'new-pass-123' },
  });
  fireEvent.click(screen.getByRole('button', { name: /update password/i }));

  await waitFor(() => expect(changePassword).toHaveBeenCalledWith('old-pass', 'new-pass-123'));
  await waitFor(() =>
    expect(screen.getByText('Password updated successfully.')).toBeInTheDocument(),
  );

  // The form clears the sensitive fields after a successful change.
  expect((screen.getByLabelText('Current password *') as HTMLInputElement).value).toBe('');
  expect((screen.getByLabelText('New password *') as HTMLInputElement).value).toBe('');
});

test('a mismatched confirmation is rejected before calling the API', async () => {
  renderPage();

  fireEvent.change(screen.getByLabelText('Current password *'), { target: { value: 'old-pass' } });
  fireEvent.change(screen.getByLabelText('New password *'), { target: { value: 'new-pass-123' } });
  fireEvent.change(screen.getByLabelText('Confirm new password *'), {
    target: { value: 'does-not-match' },
  });
  fireEvent.click(screen.getByRole('button', { name: /update password/i }));

  await waitFor(() => expect(screen.getByText(/do not match/i)).toBeInTheDocument());
  expect(changePassword).not.toHaveBeenCalled();
});

test('a rejected password change surfaces the server error', async () => {
  vi.mocked(changePassword).mockRejectedValue(new Error('current password is incorrect'));
  renderPage();

  fireEvent.change(screen.getByLabelText('Current password *'), { target: { value: 'wrong' } });
  fireEvent.change(screen.getByLabelText('New password *'), { target: { value: 'new-pass-123' } });
  fireEvent.change(screen.getByLabelText('Confirm new password *'), {
    target: { value: 'new-pass-123' },
  });
  fireEvent.click(screen.getByRole('button', { name: /update password/i }));

  await waitFor(() =>
    expect(screen.getByText('current password is incorrect')).toBeInTheDocument(),
  );
});

// --- API tokens ----------------------------------------------------------

test('shows the empty state when there are no API tokens', async () => {
  renderPage();
  await waitFor(() => expect(screen.getByText(/no api tokens/i)).toBeInTheDocument());
});

test('lists an existing token with its scope and status', async () => {
  vi.mocked(getApiTokens).mockResolvedValue({
    tokens: [apiToken({ name: 'Sync script', scope: 'carddav' })],
  });
  renderPage();

  await waitFor(() => expect(screen.getByText('Sync script')).toBeInTheDocument());
  expect(screen.getByText('CardDAV only')).toBeInTheDocument();
  expect(screen.getByText('Active')).toBeInTheDocument();
});

test('a revoked token is labeled and has no revoke action', async () => {
  vi.mocked(getApiTokens).mockResolvedValue({
    tokens: [apiToken({ name: 'Old token', revoked_at: '2026-01-02T00:00:00Z' })],
  });
  renderPage();

  await waitFor(() => expect(screen.getByText('Old token')).toBeInTheDocument());
  expect(screen.getByText('Revoked')).toBeInTheDocument();
  // /revoke/i alone would also match the page-level "Revoke All" button, so
  // scope to the per-row action's own accessible name.
  expect(screen.queryByRole('button', { name: /revoke token/i })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /rotate token/i })).not.toBeInTheDocument();
});

test('creating a token shows the secret exactly once and refreshes the list', async () => {
  const created = { ...apiToken({ id: 5, name: 'New token' }), token: 'mcrh_live_abc123' };
  vi.mocked(createApiToken).mockResolvedValue(created);

  renderPage();
  await waitFor(() => expect(screen.getByText(/no api tokens/i)).toBeInTheDocument());

  fireEvent.click(screen.getByRole('button', { name: /create token/i }));
  fireEvent.change(screen.getByLabelText(/token name/i), { target: { value: 'New token' } });
  fireEvent.click(screen.getByRole('button', { name: /^create$/i }));

  await waitFor(() => expect(createApiToken).toHaveBeenCalledWith('New token', 90, 'full'));
  await waitFor(() => expect(screen.getByDisplayValue('mcrh_live_abc123')).toBeInTheDocument());
  await waitFor(() => expect(getApiTokens).toHaveBeenCalledTimes(2));
});

test('revoking a token requires confirmation before calling the API', async () => {
  vi.mocked(getApiTokens).mockResolvedValue({ tokens: [apiToken({ id: 3, name: 'To Revoke' })] });
  vi.mocked(revokeApiToken).mockResolvedValue(undefined);

  renderPage();
  await waitFor(() => expect(screen.getByText('To Revoke')).toBeInTheDocument());

  fireEvent.click(screen.getByRole('button', { name: /revoke token/i }));
  expect(revokeApiToken).not.toHaveBeenCalled();
  expect(screen.getByText(/revoke token/i)).toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: /^revoke$/i }));

  await waitFor(() => expect(revokeApiToken).toHaveBeenCalledWith(3));
});

test('revoke-all is disabled with no active tokens and requires confirmation once enabled', async () => {
  vi.mocked(getApiTokens).mockResolvedValue({
    tokens: [apiToken({ id: 9, name: 'Revoked already', revoked_at: '2026-01-02T00:00:00Z' })],
  });
  renderPage();
  await waitFor(() => expect(screen.getByText('Revoked already')).toBeInTheDocument());

  expect(screen.getByRole('button', { name: /revoke all/i })).toBeDisabled();
});

test('revoke-all confirms before calling the API and shows the revoked count', async () => {
  vi.mocked(getApiTokens).mockResolvedValue({
    tokens: [apiToken({ id: 1, name: 'One' }), apiToken({ id: 2, name: 'Two' })],
  });
  vi.mocked(revokeAllApiTokens).mockResolvedValue({ revoked: 2 });

  renderPage();
  await waitFor(() => expect(screen.getByText('One')).toBeInTheDocument());

  fireEvent.click(screen.getByRole('button', { name: /^revoke all$/i }));
  expect(revokeAllApiTokens).not.toHaveBeenCalled();
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByText(/revoke all tokens/i)).toBeInTheDocument();

  fireEvent.click(within(dialog).getByRole('button', { name: /^revoke all$/i }));

  await waitFor(() => expect(revokeAllApiTokens).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(screen.getByText(/2 tokens revoked/i)).toBeInTheDocument());
});

test('rotating a token shows the new secret exactly once and refreshes the list', async () => {
  vi.mocked(getApiTokens).mockResolvedValue({
    tokens: [apiToken({ id: 4, name: 'Long runner' })],
  });
  const rotated = { ...apiToken({ id: 8, name: 'Long runner' }), token: 'mcrh_live_rotated456' };
  vi.mocked(rotateApiToken).mockResolvedValue(rotated);

  renderPage();
  await waitFor(() => expect(screen.getByText('Long runner')).toBeInTheDocument());

  fireEvent.click(screen.getByRole('button', { name: /rotate token/i }));
  expect(rotateApiToken).not.toHaveBeenCalled();
  expect(screen.getByText(/rotating "long runner"/i)).toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: /^rotate$/i }));

  await waitFor(() => expect(rotateApiToken).toHaveBeenCalledWith(4));
  await waitFor(() => expect(screen.getByDisplayValue('mcrh_live_rotated456')).toBeInTheDocument());
  // "Token Rotated" (dialog heading) vs. the "Token rotated successfully"
  // snackbar -- match the heading specifically to avoid ambiguity.
  expect(screen.getByRole('heading', { name: 'Token Rotated' })).toBeInTheDocument();
  await waitFor(() => expect(getApiTokens).toHaveBeenCalledTimes(2));
});

// --- Self contact picker (T90) ----------------------------------------------

test('shows the current self-contact in the picker (T90)', async () => {
  const me = contact({ ID: 7, uid: 'uid-me', firstname: 'Me', lastname: 'Contact' });
  vi.mocked(getCurrentUser).mockResolvedValue({
    id: 1,
    email: 'a@b.c',
    username: 'u',
    language: 'en',
    is_admin: false,
    created_at: '',
    updated_at: '',
    self_contact_vcard_uid: 'uid-me',
  });
  vi.mocked(getContactsByUid).mockResolvedValue(new Map([['uid-me', me]]));

  renderPage();

  await waitFor(() => expect(screen.getByDisplayValue('Me Contact')).toBeInTheDocument());
  expect(screen.getByRole('button', { name: /clear/i })).toBeInTheDocument();
});

test('selecting a contact patches the self contact and refreshes the cache (T90)', async () => {
  const bob = contact({ ID: 3, uid: 'uid-bob', firstname: 'Bob', lastname: 'Smith' });
  vi.mocked(getContacts).mockResolvedValue({ contacts: [bob], next_cursor: '', limit: 100 });

  renderPage();

  const input = await screen.findByLabelText('Which contact is you?');
  fireEvent.change(input, { target: { value: 'Bob' } });
  fireEvent.click(await screen.findByText('Bob Smith'));

  await waitFor(() => expect(updateSelfContact).toHaveBeenCalledWith('uid-bob'));
  await waitFor(() => expect(fetchAndCacheUserInfo).toHaveBeenCalled());
});

test('the clear button patches a null self contact (T90)', async () => {
  const me = contact({ ID: 7, uid: 'uid-me', firstname: 'Me', lastname: 'Contact' });
  vi.mocked(getCurrentUser).mockResolvedValue({
    id: 1,
    email: 'a@b.c',
    username: 'u',
    language: 'en',
    is_admin: false,
    created_at: '',
    updated_at: '',
    self_contact_vcard_uid: 'uid-me',
  });
  vi.mocked(getContactsByUid).mockResolvedValue(new Map([['uid-me', me]]));

  renderPage();

  fireEvent.click(await screen.findByRole('button', { name: /clear/i }));

  await waitFor(() => expect(updateSelfContact).toHaveBeenCalledWith(null));
  await waitFor(() => expect(fetchAndCacheUserInfo).toHaveBeenCalled());
});

test('a failed self-contact save surfaces the error and keeps the old selection (T90)', async () => {
  const me = contact({ ID: 7, uid: 'uid-me', firstname: 'Me', lastname: 'Contact' });
  vi.mocked(getCurrentUser).mockResolvedValue({
    id: 1,
    email: 'a@b.c',
    username: 'u',
    language: 'en',
    is_admin: false,
    created_at: '',
    updated_at: '',
    self_contact_vcard_uid: 'uid-me',
  });
  vi.mocked(getContactsByUid).mockResolvedValue(new Map([['uid-me', me]]));
  vi.mocked(updateSelfContact).mockRejectedValue(new Error("Couldn't update your self contact."));

  renderPage();
  await waitFor(() => expect(screen.getByDisplayValue('Me Contact')).toBeInTheDocument());

  fireEvent.click(screen.getByRole('button', { name: /clear/i }));

  await waitFor(() => expect(updateSelfContact).toHaveBeenCalledWith(null));
  await waitFor(() =>
    expect(screen.getByText("Couldn't update your self contact.")).toBeInTheDocument(),
  );
  expect(screen.getByDisplayValue('Me Contact')).toBeInTheDocument();
  expect(fetchAndCacheUserInfo).not.toHaveBeenCalled();
});

// --- System-status cross-link (issue #649) --------------------------------

test('admins see a link to the system-status page', async () => {
  vi.mocked(isAdmin).mockReturnValue(true);

  render(
    <MemoryRouter>
      <AppThemeProvider>
        <DateFormatProvider>
          <SnackbarProvider>
            <SettingsPage />
          </SnackbarProvider>
        </DateFormatProvider>
      </AppThemeProvider>
    </MemoryRouter>,
  );

  expect(await screen.findByText('View system status')).toBeInTheDocument();
});

test('non-admins do not see the system-status link', async () => {
  renderPage();

  await waitFor(() => expect(screen.getByText('About')).toBeInTheDocument());
  expect(screen.queryByText('View system status')).not.toBeInTheDocument();
});
