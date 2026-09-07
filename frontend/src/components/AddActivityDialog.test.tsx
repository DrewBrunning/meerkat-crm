import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import '../i18n/config';
import { type Contact, getContacts } from '../api/contacts';
import AddActivityDialog from './AddActivityDialog';

// This codebase's vitest setup has no auto-cleanup and no globals: true.
afterEach(() => {
  cleanup();
  // Issue #557: AddActivityDialog persists a draft to sessionStorage while
  // dirty. jsdom's sessionStorage is shared across tests in this file, so a
  // draft left behind by one test (e.g. a failed save, which deliberately
  // keeps the dialog open with its content intact) would otherwise leak into
  // the next test's fresh render.
  sessionStorage.clear();
});

vi.mock('../api/contacts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/contacts')>();
  return { ...actual, getContacts: vi.fn() };
});

function contact(overrides: Partial<Contact> = {}): Contact {
  return { ID: 1, firstname: 'Alice', lastname: 'Johnson', ...overrides };
}

beforeEach(() => {
  vi.mocked(getContacts).mockReset();
  vi.mocked(getContacts).mockResolvedValue({ contacts: [contact()], next_cursor: '' } as never);
});

function renderDialog(props: Partial<React.ComponentProps<typeof AddActivityDialog>> = {}) {
  const defaults: React.ComponentProps<typeof AddActivityDialog> = {
    open: true,
    onClose: vi.fn(),
    onSave: vi.fn().mockResolvedValue(undefined),
    ...props,
  };
  return render(<AddActivityDialog {...defaults} />);
}

test('renders the title, description, location, date, and contacts fields', async () => {
  renderDialog();
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  expect(screen.getByLabelText('Title *')).toBeInTheDocument();
  expect(screen.getByLabelText('Description')).toBeInTheDocument();
  expect(screen.getByLabelText('Location')).toBeInTheDocument();
  expect(screen.getByLabelText('Date *')).toBeInTheDocument();
  expect(screen.getByLabelText('Contacts')).toBeInTheDocument();
});

test('requires a title and date before saving', async () => {
  const onSave = vi.fn();
  renderDialog({ onSave });
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('Title *'), { target: { value: '' } });
  fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

  expect(screen.getByText('Title and date are required')).toBeInTheDocument();
  expect(onSave).not.toHaveBeenCalled();
});

test('saves with the entered title, description, location, and date', async () => {
  const onSave = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn();
  renderDialog({ onSave, onClose });
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('Title *'), { target: { value: 'Coffee catchup' } });
  fireEvent.change(screen.getByLabelText('Description'), {
    target: { value: 'Talked about the new job' },
  });
  fireEvent.change(screen.getByLabelText('Location'), { target: { value: 'Blue Bottle' } });
  fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

  await waitFor(() =>
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'Coffee catchup',
        description: 'Talked about the new job',
        location: 'Blue Bottle',
        contact_ids: [],
      }),
    ),
  );
  // The dialog closes itself (via the parent's onClose) once the save resolves.
  await waitFor(() => expect(onClose).toHaveBeenCalled());
});

test('preselects the contact passed via preselectedContactId', async () => {
  vi.mocked(getContacts).mockResolvedValue({
    contacts: [
      contact({ ID: 1, firstname: 'Alice', lastname: 'Johnson' }),
      contact({ ID: 2, firstname: 'Bob', lastname: 'Smith' }),
    ],
    next_cursor: '',
  } as never);

  renderDialog({ preselectedContactId: 2 });

  await waitFor(() => expect(screen.getByText('Bob Smith')).toBeInTheDocument());
});

test('selecting a contact from the autocomplete includes it in contact_ids', async () => {
  vi.mocked(getContacts).mockResolvedValue({
    contacts: [contact({ ID: 3, firstname: 'Carol', lastname: 'Diaz' })],
    next_cursor: '',
  } as never);
  const onSave = vi.fn().mockResolvedValue(undefined);

  renderDialog({ onSave });
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('Title *'), { target: { value: 'Lunch' } });
  fireEvent.mouseDown(screen.getByLabelText('Contacts'));
  fireEvent.click(await screen.findByRole('option', { name: 'Carol Diaz' }));

  fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

  await waitFor(() =>
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ contact_ids: [3] })),
  );
});

test('a save failure keeps the dialog open and shows an error', async () => {
  const onSave = vi.fn().mockRejectedValue(new Error('boom'));
  const onClose = vi.fn();
  renderDialog({ onSave, onClose });
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('Title *'), { target: { value: 'Will fail' } });
  fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

  await waitFor(() => expect(screen.getByText('Failed to save activity')).toBeInTheDocument());
  expect(onClose).not.toHaveBeenCalled();
});

test('cancel closes the dialog without saving', async () => {
  const onSave = vi.fn();
  const onClose = vi.fn();
  renderDialog({ onSave, onClose });
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

  expect(onSave).not.toHaveBeenCalled();
  expect(onClose).toHaveBeenCalled();
});

// Issue #557 item 3: a dirty activity is not silently discarded on Cancel.
test('cancel with typed fields asks for confirmation before discarding', async () => {
  const onClose = vi.fn();
  renderDialog({ onClose });
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('Title *'), { target: { value: 'Coffee catch-up' } });
  fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

  expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();
  expect(onClose).not.toHaveBeenCalled();

  // "Keep editing" backs out without losing what was typed.
  fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));
  await waitFor(() =>
    expect(screen.queryByText('Discard unsaved changes?')).not.toBeInTheDocument(),
  );
  expect(onClose).not.toHaveBeenCalled();
  expect(screen.getByLabelText('Title *')).toHaveValue('Coffee catch-up');

  // Confirming the discard actually closes.
  fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  expect(onClose).toHaveBeenCalledTimes(1);
});

// Issue #557 item 4: a draft persisted to sessionStorage turns a killed tab
// or a crash-and-retry into an interruption, not a loss.
test('a dirty draft survives an unmount/remount (tab close, crash recovery)', async () => {
  const { unmount } = renderDialog();
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('Title *'), { target: { value: 'Draft activity' } });
  await waitFor(() => expect(screen.getByLabelText('Title *')).toHaveValue('Draft activity'));

  unmount();

  renderDialog();
  await waitFor(() => expect(screen.getByLabelText('Title *')).toHaveValue('Draft activity'));
});

// A successful save must not leave a stale draft behind to reappear the next
// time the dialog opens.
test('a successful save clears the draft', async () => {
  const onSave = vi.fn().mockResolvedValue(undefined);
  const { unmount } = renderDialog({ onSave });
  await waitFor(() => expect(getContacts).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText('Title *'), { target: { value: 'Saved activity' } });
  fireEvent.change(screen.getByLabelText('Date *'), { target: { value: '2026-01-01' } });
  fireEvent.click(screen.getByRole('button', { name: /^save$/i }));
  await waitFor(() => expect(onSave).toHaveBeenCalled());

  unmount();

  renderDialog();
  await waitFor(() => expect(screen.getByLabelText('Title *')).toBeInTheDocument());
  expect(screen.getByLabelText('Title *')).toHaveValue('');
});

// Issue #805: with the app on a data router, a dirty activity also guards
// *in-app route navigation* -- the blocker intercepts a drawer link /
// programmatic navigate / browser Back and asks before discarding, mirroring
// the Cancel/Escape path. These render the dialog under a real data router so
// the blocker machinery exists.
describe('AddActivityDialog in-app route navigation guard (issue #805)', () => {
  const draftKey = 'mycorrhizal:draft:activity-dialog:unassigned';

  function renderDialogInRouter() {
    const onClose = vi.fn();
    const router = createMemoryRouter(
      [
        {
          path: '/activities',
          element: (
            <AddActivityDialog
              open
              onClose={onClose}
              onSave={vi.fn().mockResolvedValue(undefined)}
            />
          ),
        },
        { path: '/notes', element: <div>notes page</div> },
      ],
      { initialEntries: ['/activities'] },
    );
    render(<RouterProvider router={router} />);
    return { router, onClose };
  }

  async function typeDirtyActivity() {
    await waitFor(() => expect(screen.getByLabelText('Title *')).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText('Title *'), {
      target: { value: 'Unplanned coffee catch-up' },
    });
    await waitFor(() =>
      expect(screen.getByLabelText('Title *')).toHaveValue('Unplanned coffee catch-up'),
    );
  }

  test('in-app navigation away from a dirty activity is suspended and asks before discarding', async () => {
    const { router } = renderDialogInRouter();
    await typeDirtyActivity();
    expect(sessionStorage.getItem(draftKey)).not.toBeNull();

    // Simulate the browser Back button / a programmatic in-app navigation
    // while the dirty dialog is open.
    await act(async () => {
      router.navigate('/notes');
    });

    expect(router.state.location.pathname).toBe('/activities');
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();
    expect(screen.getByLabelText('Title *')).toHaveValue('Unplanned coffee catch-up');
  });

  test('Keep editing cancels the navigation and leaves the draft intact', async () => {
    const { router, onClose } = renderDialogInRouter();
    await typeDirtyActivity();

    await act(async () => {
      router.navigate('/notes');
    });
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));

    expect(router.state.location.pathname).toBe('/activities');
    await waitFor(() => expect(screen.queryByText('Discard unsaved changes?')).toBeNull());
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Title *')).toHaveValue('Unplanned coffee catch-up');
    expect(sessionStorage.getItem(draftKey)).not.toBeNull();
  });

  test('Discard closes the dialog, clears the draft, and lets the navigation through', async () => {
    const { router, onClose } = renderDialogInRouter();
    await typeDirtyActivity();

    await act(async () => {
      router.navigate('/notes');
    });
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/notes'));
    expect(onClose).toHaveBeenCalledTimes(1);
    // A navigation discard is a real discard -- the draft must not reappear
    // the next time the dialog opens.
    expect(sessionStorage.getItem(draftKey)).toBeNull();
  });

  test('a clean activity does not block navigation', async () => {
    const { router } = renderDialogInRouter();
    await waitFor(() => expect(screen.getByLabelText('Title *')).toBeInTheDocument());

    await act(async () => {
      router.navigate('/notes');
    });

    expect(router.state.location.pathname).toBe('/notes');
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();
  });
});
