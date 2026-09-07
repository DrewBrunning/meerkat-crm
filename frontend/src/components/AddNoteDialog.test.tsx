import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import '../i18n/config';
import { SnackbarProvider } from '../context/SnackbarContext';
import { DateFormatProvider } from '../DateFormatProvider';
import AddNoteDialog from './AddNoteDialog';

beforeEach(() => {
  localStorage.setItem(
    'user_info',
    JSON.stringify({ user_id: 1, username: 'test', is_admin: false }),
  );
});

afterEach(() => {
  cleanup();
  localStorage.clear();
  // Issue #557: AddNoteDialog persists a draft to sessionStorage while
  // dirty. jsdom's sessionStorage is shared across tests in this file, so a
  // draft left behind by one test would otherwise leak into the next test's
  // fresh render.
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

function mockFetchByUrl(handlers: Record<string, () => unknown>) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      for (const [pattern, respond] of Object.entries(handlers)) {
        if (url.includes(pattern)) {
          return { ok: true, json: async () => respond() };
        }
      }
      throw new Error(`unexpected fetch: ${url}`);
    }),
  );
}

const contactsResponse = () => ({
  contacts: [
    { ID: 5, uid: 'uid-5', firstname: 'Alice', lastname: 'Anderson' },
    { ID: 6, uid: 'uid-6', firstname: 'Bob', lastname: 'Brown' },
  ],
  next_cursor: '',
  limit: 40,
});

function renderDialog(props: Partial<React.ComponentProps<typeof AddNoteDialog>> = {}) {
  const defaults: React.ComponentProps<typeof AddNoteDialog> = {
    open: true,
    onClose: vi.fn(),
    onSave: vi.fn().mockResolvedValue(undefined),
    ...props,
  };
  return render(
    <SnackbarProvider>
      <DateFormatProvider>
        <AddNoteDialog {...defaults} />
      </DateFormatProvider>
    </SnackbarProvider>,
  );
}

// T7: AddNoteDialog renders the contact Autocomplete for optional assignment
// at creation time.
test('renders contact autocomplete for assigning a contact at creation', async () => {
  mockFetchByUrl({ '/contacts?': contactsResponse });

  render(
    <SnackbarProvider>
      <DateFormatProvider>
        <AddNoteDialog
          open={true}
          onClose={vi.fn()}
          onSave={vi.fn().mockResolvedValue(undefined)}
        />
      </DateFormatProvider>
    </SnackbarProvider>,
  );

  // The contact Autocomplete placeholder is rendered
  await waitFor(() => {
    expect(screen.getByPlaceholderText('Search contacts...')).toBeDefined();
  });

  // Content and date fields exist
  expect(screen.getByLabelText('Content *')).toBeDefined();
  expect(screen.getByLabelText('Date')).toBeDefined();

  // Save and cancel buttons exist
  expect(screen.getByText('Save')).toBeDefined();
  expect(screen.getByText('Cancel')).toBeDefined();
});

// T7b: Dialog title is present.
test('renders the dialog title', async () => {
  mockFetchByUrl({ '/contacts?': contactsResponse });

  render(
    <SnackbarProvider>
      <DateFormatProvider>
        <AddNoteDialog
          open={true}
          onClose={vi.fn()}
          onSave={vi.fn().mockResolvedValue(undefined)}
        />
      </DateFormatProvider>
    </SnackbarProvider>,
  );

  await waitFor(() => {
    expect(screen.getByText('Add Note')).toBeDefined();
  });
});

// Issue #557: Cancel on a clean (untouched) form closes immediately -- no
// confirmation for a form with nothing to lose.
test('cancel with no content closes immediately, without a confirmation prompt', async () => {
  mockFetchByUrl({ '/contacts?': contactsResponse });
  const onClose = vi.fn();
  renderDialog({ onClose });
  await waitFor(() => expect(screen.getByLabelText('Content *')).toBeInTheDocument());

  fireEvent.click(screen.getByText('Cancel'));

  expect(onClose).toHaveBeenCalledTimes(1);
  expect(screen.queryByText('Discard unsaved changes?')).not.toBeInTheDocument();
});

// Issue #557 item 3: a dirty note is not silently discarded on Cancel.
test('cancel with typed content asks for confirmation before discarding', async () => {
  mockFetchByUrl({ '/contacts?': contactsResponse });
  const onClose = vi.fn();
  renderDialog({ onClose });
  await waitFor(() => expect(screen.getByLabelText('Content *')).toBeInTheDocument());

  fireEvent.change(screen.getByLabelText('Content *'), {
    target: { value: 'Called to check in, all is well.' },
  });
  fireEvent.click(screen.getByText('Cancel'));

  expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();
  expect(onClose).not.toHaveBeenCalled();

  // "Keep editing" backs out without losing what was typed.
  fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));
  await waitFor(() =>
    expect(screen.queryByText('Discard unsaved changes?')).not.toBeInTheDocument(),
  );
  expect(onClose).not.toHaveBeenCalled();
  expect(screen.getByLabelText('Content *')).toHaveValue('Called to check in, all is well.');

  // Confirming the discard actually closes.
  fireEvent.click(screen.getByText('Cancel'));
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  expect(onClose).toHaveBeenCalledTimes(1);
});

// Issue #557 item 4: a draft persisted to sessionStorage turns a killed tab
// or a crash-and-retry into an interruption, not a loss. This simulates that
// by unmounting (losing all in-memory React state, the same as a fresh page
// load) and re-rendering a fresh instance of the dialog.
test('a dirty draft survives an unmount/remount (tab close, crash recovery)', async () => {
  mockFetchByUrl({ '/contacts?': contactsResponse });
  const { unmount } = renderDialog();
  await waitFor(() => expect(screen.getByLabelText('Content *')).toBeInTheDocument());

  fireEvent.change(screen.getByLabelText('Content *'), {
    target: { value: 'Draft note content' },
  });
  await waitFor(() => expect(screen.getByLabelText('Content *')).toHaveValue('Draft note content'));

  unmount();

  renderDialog();
  await waitFor(() => expect(screen.getByLabelText('Content *')).toHaveValue('Draft note content'));
});

// A successful save must not leave a stale draft behind to reappear the next
// time the dialog opens.
test('a successful save clears the draft', async () => {
  mockFetchByUrl({ '/contacts?': contactsResponse });
  const onSave = vi.fn().mockResolvedValue(undefined);
  const { unmount } = renderDialog({ onSave });
  await waitFor(() => expect(screen.getByLabelText('Content *')).toBeInTheDocument());

  fireEvent.change(screen.getByLabelText('Content *'), { target: { value: 'Saved note' } });
  fireEvent.click(screen.getByText('Save'));
  await waitFor(() => expect(onSave).toHaveBeenCalled());

  unmount();

  renderDialog();
  await waitFor(() => expect(screen.getByLabelText('Content *')).toBeInTheDocument());
  expect(screen.getByLabelText('Content *')).toHaveValue('');
});

// Issue #805: with the app on a data router, a dirty note also guards *in-app
// route navigation* -- the blocker intercepts a drawer link / programmatic
// navigate / browser Back and asks before discarding, mirroring the
// Cancel/Escape path. These render the dialog under a real data router so the
// blocker machinery exists.
describe('AddNoteDialog in-app route navigation guard (issue #805)', () => {
  const draftKey = 'mycorrhizal:draft:note-dialog:unassigned';

  function renderDialogInRouter() {
    const onClose = vi.fn();
    const router = createMemoryRouter(
      [
        {
          path: '/notes',
          element: (
            <AddNoteDialog open onClose={onClose} onSave={vi.fn().mockResolvedValue(undefined)} />
          ),
        },
        { path: '/contacts', element: <div>contacts page</div> },
      ],
      { initialEntries: ['/notes'] },
    );
    render(
      <SnackbarProvider>
        <DateFormatProvider>
          <RouterProvider router={router} />
        </DateFormatProvider>
      </SnackbarProvider>,
    );
    return { router, onClose };
  }

  async function typeDirtyNote() {
    await waitFor(() => expect(screen.getByLabelText('Content *')).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText('Content *'), {
      target: { value: 'A note I have not saved yet.' },
    });
    await waitFor(() =>
      expect(screen.getByLabelText('Content *')).toHaveValue('A note I have not saved yet.'),
    );
  }

  test('in-app navigation away from a dirty note is suspended and asks before discarding', async () => {
    mockFetchByUrl({ '/contacts?': contactsResponse });
    const { router } = renderDialogInRouter();
    await typeDirtyNote();
    expect(sessionStorage.getItem(draftKey)).not.toBeNull();

    // Simulate the browser Back button / a programmatic in-app navigation
    // while the dirty dialog is open.
    await act(async () => {
      router.navigate('/contacts');
    });

    expect(router.state.location.pathname).toBe('/notes');
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();
    expect(screen.getByLabelText('Content *')).toHaveValue('A note I have not saved yet.');
  });

  test('Keep editing cancels the navigation and leaves the draft intact', async () => {
    mockFetchByUrl({ '/contacts?': contactsResponse });
    const { router, onClose } = renderDialogInRouter();
    await typeDirtyNote();

    await act(async () => {
      router.navigate('/contacts');
    });
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));

    expect(router.state.location.pathname).toBe('/notes');
    await waitFor(() => expect(screen.queryByText('Discard unsaved changes?')).toBeNull());
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Content *')).toHaveValue('A note I have not saved yet.');
    expect(sessionStorage.getItem(draftKey)).not.toBeNull();
  });

  test('Discard closes the dialog, clears the draft, and lets the navigation through', async () => {
    mockFetchByUrl({ '/contacts?': contactsResponse });
    const { router, onClose } = renderDialogInRouter();
    await typeDirtyNote();

    await act(async () => {
      router.navigate('/contacts');
    });
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/contacts'));
    expect(onClose).toHaveBeenCalledTimes(1);
    // A navigation discard is a real discard -- the draft must not reappear
    // the next time the dialog opens.
    expect(sessionStorage.getItem(draftKey)).toBeNull();
  });

  test('a clean note does not block navigation', async () => {
    mockFetchByUrl({ '/contacts?': contactsResponse });
    const { router } = renderDialogInRouter();
    await waitFor(() => expect(screen.getByLabelText('Content *')).toBeInTheDocument());

    await act(async () => {
      router.navigate('/contacts');
    });

    expect(router.state.location.pathname).toBe('/contacts');
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();
  });
});
