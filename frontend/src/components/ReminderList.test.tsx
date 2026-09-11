import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import '../i18n/config';
import type { Reminder } from '../api/reminders';
import { DateFormatProvider } from '../DateFormatProvider';
import ReminderList from './ReminderList';

// This codebase's vitest setup does not auto-cleanup between tests.
afterEach(cleanup);

function reminder(overrides: Partial<Reminder> = {}): Reminder {
  return {
    ID: 1,
    message: 'Water the plants',
    by_mail: true,
    remind_at: '2026-08-01T00:00:00Z',
    recurrence: 'once',
    reoccur_from_completion: true,
    completed: false,
    email_sent: false,
    contact_id: 3,
    ...overrides,
  };
}

function renderList(props: Partial<React.ComponentProps<typeof ReminderList>> = {}) {
  const defaults: React.ComponentProps<typeof ReminderList> = {
    reminders: [],
    onComplete: vi.fn().mockResolvedValue(undefined),
    onEdit: vi.fn(),
    onDelete: vi.fn().mockResolvedValue(undefined),
  };
  const merged = { ...defaults, ...props };
  render(
    <DateFormatProvider>
      <ReminderList {...merged} />
    </DateFormatProvider>,
  );
  return {
    onComplete: merged.onComplete,
    onEdit: merged.onEdit,
    onDelete: merged.onDelete,
  };
}

test('shows the empty state when there are no reminders', () => {
  renderList();

  expect(screen.getByText('No reminders yet')).toBeInTheDocument();
});

test('renders a reminder with its date, email and recurrence metadata', () => {
  renderList({
    reminders: [reminder({ recurrence: 'monthly' })],
  });

  expect(screen.getByText('Water the plants')).toBeInTheDocument();
  expect(screen.getByText('01.08.2026')).toBeInTheDocument();
  expect(screen.getByText('Email')).toBeInTheDocument();
  expect(screen.getByText('Monthly')).toBeInTheDocument();
  expect(screen.getByText('Flexible')).toBeInTheDocument();
});

// #196 (audit gap, N-1's sibling): revealed only on `&:hover .action-buttons`
// -- no `:focus-within` -- so keyboard focus landed on a fully transparent
// control.
test('edit/delete actions are keyboard-revealable', () => {
  renderList({ reminders: [reminder()] });
  const css = Array.from(document.querySelectorAll('style'))
    .map((s) => s.textContent || '')
    .join('\n');
  expect(css).toContain(':focus-within .action-buttons');
});

test('a one-off reminder shows neither the recurrence chip nor the flexible chip', () => {
  renderList({
    reminders: [reminder({ recurrence: 'once', reoccur_from_completion: false })],
  });

  expect(screen.queryByText('Flexible')).not.toBeInTheDocument();
  expect(screen.getByText('Email')).toBeInTheDocument();
});

test('completing a reminder calls onComplete with its id', async () => {
  const { onComplete } = renderList({
    reminders: [reminder({ ID: 5 })],
  });

  fireEvent.click(screen.getByLabelText('Complete'));

  await waitFor(() => expect(onComplete).toHaveBeenCalledWith(5));
});

test('editing a reminder passes the whole reminder through onEdit', () => {
  const theReminder = reminder({ ID: 9, message: 'Call the plumber' });
  const { onEdit } = renderList({ reminders: [theReminder] });

  fireEvent.click(screen.getByLabelText('Edit'));

  expect(onEdit).toHaveBeenCalledWith(theReminder);
});

test('deleting a reminder asks for confirmation before calling onDelete', async () => {
  const { onDelete } = renderList({ reminders: [reminder({ ID: 3 })] });

  fireEvent.click(screen.getByLabelText('Delete'));

  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByText('Delete Reminder')).toBeInTheDocument();

  fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));

  await waitFor(() => expect(onDelete).toHaveBeenCalledWith(3));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
});

test('cancelling the delete confirmation does not call onDelete', async () => {
  const { onDelete } = renderList({ reminders: [reminder({ ID: 4 })] });

  fireEvent.click(screen.getByLabelText('Delete'));
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }));

  expect(onDelete).not.toHaveBeenCalled();
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
});

test('an overdue reminder renders a warning-styled date chip', () => {
  renderList({ reminders: [reminder({ remind_at: '2020-01-01T00:00:00Z' })] });

  expect(screen.getByText('01.01.2020')).toBeInTheDocument();
  expect(screen.getByText('01.01.2020').closest('.MuiChip-colorWarning')).not.toBeNull();
});
