import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import './i18n/config';
import { listCircles } from './api/circles';
import { dismissContactSyncConflict, restoreContactSyncConflict } from './api/contactSyncConflicts';
import { type DashboardResponse, getDashboard } from './api/dashboard';
import { completeReminder, getUpcomingReminders, skipReminder } from './api/reminders';
import DashboardPage from './DashboardPage';
import { DateFormatProvider } from './DateFormatProvider';

// M3: the page
// now fetches one GET /dashboard composite instead of fanning out to four
// endpoints plus a per-reminder contact lookup. This codebase's vitest has
// no auto-cleanup (CLAUDE.md frontend trap #1).
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

vi.mock('./api/dashboard', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/dashboard')>();
  return { ...actual, getDashboard: vi.fn() };
});
vi.mock('./api/reminders', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/reminders')>();
  return {
    ...actual,
    getUpcomingReminders: vi.fn(),
    completeReminder: vi.fn(),
    skipReminder: vi.fn(),
  };
});
vi.mock('./api/circles', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/circles')>();
  return { ...actual, listCircles: vi.fn() };
});
vi.mock('./api/contactSyncConflicts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/contactSyncConflicts')>();
  return { ...actual, restoreContactSyncConflict: vi.fn(), dismissContactSyncConflict: vi.fn() };
});

const getDashboardMock = vi.mocked(getDashboard);
const getUpcomingRemindersMock = vi.mocked(getUpcomingReminders);
const completeReminderMock = vi.mocked(completeReminder);
const skipReminderMock = vi.mocked(skipReminder);
const listCirclesMock = vi.mocked(listCircles);
const restoreSyncConflictMock = vi.mocked(restoreContactSyncConflict);
const dismissSyncConflictMock = vi.mocked(dismissContactSyncConflict);

function emptyDashboard(): DashboardResponse {
  return {
    birthdays: [],
    random_contacts: [],
    upcoming_reminders: [],
    overdue: [],
    favorites: [],
    reach_out_suggestions: [],
    contact_sync_conflicts: [],
  };
}

beforeEach(() => {
  getDashboardMock.mockReset();
  getUpcomingRemindersMock.mockReset();
  completeReminderMock.mockReset();
  skipReminderMock.mockReset();
  listCirclesMock.mockReset();
  restoreSyncConflictMock.mockReset();
  dismissSyncConflictMock.mockReset();
  listCirclesMock.mockResolvedValue({
    circles: [],
    total: 0,
    next_cursor: '',
    limit: 200,
    members: [],
  });
});

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/dashboard']}>
      <DateFormatProvider>
        <Routes>
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/contacts/:id" element={<div>CONTACT DETAIL PAGE</div>} />
        </Routes>
      </DateFormatProvider>
    </MemoryRouter>,
  );
}

test('fetches the dashboard composite once and renders all four blocks', async () => {
  getDashboardMock.mockResolvedValue({
    birthdays: [{ type: 'contact', name: 'Bea Birthday', birthday: '--08-20', contact_id: 1 }],
    random_contacts: [{ ID: 2, firstname: 'Randy', lastname: 'Contact', archived: false }],
    favorites: [{ ID: 4, firstname: 'Fay', lastname: 'Vorite', is_favorite: true }],
    upcoming_reminders: [
      {
        ID: 9,
        message: 'Call Nicky',
        by_mail: false,
        remind_at: '2026-08-12T00:00:00Z',
        recurrence: 'once',
        reoccur_from_completion: true,
        completed: false,
        email_sent: false,
        contact_id: 3,
        contact_name: 'Nicky Name',
      },
    ],
    overdue: [],
    reach_out_suggestions: [],
    contact_sync_conflicts: [],
  });

  renderPage();

  await waitFor(() => expect(screen.getByText('Bea Birthday')).toBeInTheDocument());
  expect(screen.getByText('Randy Contact')).toBeInTheDocument();
  // Issue #173: the favorites block renders its contacts.
  expect(screen.getByText('Fay Vorite')).toBeInTheDocument();
  // #196: the decorative avatar initial must not double into a card link's
  // accessible name ("FFay Vorite") -- favorites, birthdays and stay-in-touch
  // all wrap an <Avatar> inside `component={Link}`.
  expect(screen.getByRole('link', { name: 'Fay Vorite' })).toBeInTheDocument();
  expect(screen.getByRole('link', { name: /^Bea Birthday/ })).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Randy Contact' })).toBeInTheDocument();
  expect(screen.getByText('Call Nicky')).toBeInTheDocument();
  // The reminder's contact_name is embedded server-side (M3 design decision
  // 2) -- no separate per-reminder contact fetch happens.
  expect(screen.getByText('Nicky Name')).toBeInTheDocument();
  expect(getDashboardMock).toHaveBeenCalledTimes(1);
});

test("empty dashboard renders each column's empty state without crashing", async () => {
  getDashboardMock.mockResolvedValue(emptyDashboard());

  renderPage();

  await waitFor(() => expect(screen.getByText('No upcoming birthdays')).toBeInTheDocument());
  expect(screen.getByText('No upcoming reminders')).toBeInTheDocument();
  // Issue #173: the favorites block's empty state.
  expect(screen.getByText('No favorites yet')).toBeInTheDocument();
});

test('completing a reminder refetches via the plain upcoming-reminders endpoint and keeps the known contact name', async () => {
  getDashboardMock.mockResolvedValue({
    ...emptyDashboard(),
    upcoming_reminders: [
      {
        ID: 9,
        message: 'Call Nicky',
        by_mail: false,
        remind_at: '2026-08-12T00:00:00Z',
        recurrence: 'once',
        reoccur_from_completion: true,
        completed: false,
        email_sent: false,
        contact_id: 3,
        contact_name: 'Nicky Name',
      },
    ],
  });
  completeReminderMock.mockResolvedValue({ message: 'ok' });
  getUpcomingRemindersMock.mockResolvedValue([
    {
      ID: 9,
      message: 'Call Nicky',
      by_mail: false,
      remind_at: '2026-08-19T00:00:00Z',
      recurrence: 'weekly',
      reoccur_from_completion: true,
      completed: false,
      email_sent: false,
      contact_id: 3,
    },
  ]);

  renderPage();

  await waitFor(() => expect(screen.getByText('Call Nicky')).toBeInTheDocument());
  fireEvent.click(screen.getByRole('button', { name: 'Complete' }));

  await waitFor(() => expect(completeReminderMock).toHaveBeenCalledWith(9));
  await waitFor(() => expect(getUpcomingRemindersMock).toHaveBeenCalledTimes(1));
  // The refetched reminder carries no contact_name from the plain endpoint;
  // the page must carry the previously-known name forward.
  await waitFor(() => expect(screen.getByText('Nicky Name')).toBeInTheDocument());
});

test('renders a CardDAV sync conflict and can restore or dismiss it (issue #395)', async () => {
  getDashboardMock.mockResolvedValue({
    ...emptyDashboard(),
    contact_sync_conflicts: [
      {
        id: 'conflict-1',
        created_at: '2026-08-24T00:00:00Z',
        updated_at: '2026-08-24T00:00:00Z',
        subscription_id: 5,
        contact_id: 42,
        field: 'phone',
        local_value: '[{"type":"work","value":"555-0100"}]',
        remote_value: '[]',
        status: 'pending',
        contact_vcard_uid: 'uid-42',
        contact_name: 'Grace Hopper',
        subscription_name: 'Work address book',
      },
    ],
  });
  restoreSyncConflictMock.mockResolvedValue();
  dismissSyncConflictMock.mockResolvedValue();
  vi.spyOn(window, 'confirm').mockReturnValue(true);

  renderPage();

  await waitFor(() => expect(screen.getByText('Grace Hopper')).toBeInTheDocument());
  // The conflict notice names the field and offers the local value back
  // (the caption renders "Phone: 555-0100 → —").
  expect(screen.getByText(/Phone: 555-0100/)).toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: 'Restore local value' }));
  await waitFor(() => expect(restoreSyncConflictMock).toHaveBeenCalledWith('conflict-1'));
  await waitFor(() => expect(screen.queryByText('Grace Hopper')).not.toBeInTheDocument());
});

test('dismissing a sync conflict removes the notice', async () => {
  getDashboardMock.mockResolvedValue({
    ...emptyDashboard(),
    contact_sync_conflicts: [
      {
        id: 'conflict-2',
        created_at: '2026-08-24T00:00:00Z',
        updated_at: '2026-08-24T00:00:00Z',
        subscription_id: 5,
        contact_id: 43,
        field: 'job_title',
        local_value: 'Local Title',
        remote_value: 'Remote Title',
        status: 'pending',
        contact_vcard_uid: 'uid-43',
        contact_name: 'Ada Lovelace',
        subscription_name: 'Work address book',
      },
    ],
  });
  dismissSyncConflictMock.mockResolvedValue();

  renderPage();

  await waitFor(() => expect(screen.getByText('Ada Lovelace')).toBeInTheDocument());
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
  await waitFor(() => expect(dismissSyncConflictMock).toHaveBeenCalledWith('conflict-2'));
  await waitFor(() => expect(screen.queryByText('Ada Lovelace')).not.toBeInTheDocument());
});
