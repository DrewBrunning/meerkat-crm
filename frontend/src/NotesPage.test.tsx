import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import './i18n/config';
import { AnnouncerProvider } from './context/AnnouncerContext';
import { SnackbarProvider } from './context/SnackbarContext';
import { DateFormatProvider } from './DateFormatProvider';
import NotesPage from './NotesPage';

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

const emptyNotesResponse = () => ({
  notes: [],
  next_cursor: '',
  limit: 25,
});

const twoUnfiledNotesResponse = () => ({
  notes: [
    {
      ID: 1,
      content: 'First note',
      date: '2026-01-01T00:00:00Z',
      contact_id: null,
      CreatedAt: '2026-01-01T00:00:00Z',
      UpdatedAt: '2026-01-01T00:00:00Z',
    },
    {
      ID: 2,
      content: 'Second note',
      date: '2026-01-02T00:00:00Z',
      contact_id: null,
      CreatedAt: '2026-01-02T00:00:00Z',
      UpdatedAt: '2026-01-02T00:00:00Z',
    },
  ],
  next_cursor: '',
  limit: 25,
});

function renderPage() {
  return render(
    <SnackbarProvider>
      <AnnouncerProvider>
        <DateFormatProvider>
          <NotesPage />
        </DateFormatProvider>
      </AnnouncerProvider>
    </SnackbarProvider>,
  );
}

test('renders empty inbox when no notes exist', async () => {
  mockFetchByUrl({ '/notes?': emptyNotesResponse });
  renderPage();

  await waitFor(() => {
    expect(screen.getByText('Notes')).toBeDefined();
  });

  expect(screen.getByText('0')).toBeDefined();
  expect(screen.getByText('No unfiled notes')).toBeDefined();
});

test('renders notes list and shows unfiled count', async () => {
  mockFetchByUrl({ '/notes?': twoUnfiledNotesResponse });
  renderPage();

  await waitFor(() => {
    expect(screen.getByText('First note')).toBeDefined();
  });

  expect(screen.getByText('Second note')).toBeDefined();
  expect(screen.getByText('2')).toBeDefined();

  // #196 (audit gap, N-1's sibling): a note's edit/delete actions were
  // revealed only on `&:hover .edit-actions` -- no `:focus-within` -- so
  // keyboard focus landed on a fully transparent control.
  const css = Array.from(document.querySelectorAll('style'))
    .map((s) => s.textContent || '')
    .join('\n');
  expect(css).toContain(':focus-within .edit-actions');
});

// T1: When every note has a contact_id, the inbox is empty. The backend
// filters notes by contact_id IS NULL, so all-filed = empty inbox.
// This proves the data pipeline: the frontend faithfully shows whatever
// the server returns, and the count reflects it.
test('inbox is empty when all notes are filed', async () => {
  const allFiledResponse = () => ({
    notes: [],
    next_cursor: '',
    limit: 25,
  });
  mockFetchByUrl({ '/notes?': allFiledResponse });
  renderPage();

  await waitFor(() => {
    expect(screen.getByText('Notes')).toBeDefined();
  });

  expect(screen.getByText('0')).toBeDefined();
  expect(screen.getByText('No unfiled notes')).toBeDefined();
});

// T17: with a non-empty next_cursor the Load more button appears and appends
// the next cursor page; once next_cursor is empty it disappears.
test('renders Load more and appends the next cursor page', async () => {
  const pageOne = () => ({
    notes: [
      {
        ID: 1,
        content: 'First note',
        date: '2026-01-01T00:00:00Z',
        contact_id: null,
        CreatedAt: '2026-01-01T00:00:00Z',
        UpdatedAt: '2026-01-01T00:00:00Z',
      },
    ],
    next_cursor: 'CURSOR-1',
    limit: 25,
  });
  const pageTwo = () => ({
    notes: [
      {
        ID: 2,
        content: 'Second note',
        date: '2026-01-02T00:00:00Z',
        contact_id: null,
        CreatedAt: '2026-01-02T00:00:00Z',
        UpdatedAt: '2026-01-02T00:00:00Z',
      },
    ],
    next_cursor: '',
    limit: 25,
  });
  // First request (no cursor) → pageOne; second request (?cursor=CURSOR-1) → pageTwo.
  mockFetchByUrl({
    'cursor=CURSOR-1': pageTwo,
    '/notes?': pageOne,
  });
  renderPage();

  await waitFor(() => {
    expect(screen.getByText('First note')).toBeDefined();
  });

  const loadMore = screen.getByText('Load more');
  expect(loadMore).toBeDefined();

  loadMore.click();

  await waitFor(() => {
    expect(screen.getByText('Second note')).toBeDefined();
  });
  // Both pages' notes are now shown and the button is gone (next_cursor empty).
  expect(screen.queryByText('Load more')).toBeNull();
  expect(screen.getByText('First note')).toBeDefined();
  expect(screen.getByText('2')).toBeDefined();
});

// T8: The Add Note button is present and clickable.
test('renders the Add Note button', async () => {
  mockFetchByUrl({ '/notes?': emptyNotesResponse });
  renderPage();

  await waitFor(() => {
    expect(screen.getByText('Notes')).toBeDefined();
  });

  expect(screen.getByText('Add Note')).toBeDefined();
});

// T9: The filter/search bar is rendered.
test('renders the filter and search bar', async () => {
  mockFetchByUrl({ '/notes?': emptyNotesResponse });
  renderPage();

  await waitFor(() => {
    expect(screen.getByText('Notes')).toBeDefined();
  });

  // Filter inputs exist
  expect(screen.getByLabelText('Search...')).toBeDefined();
  expect(screen.getByLabelText('From')).toBeDefined();
  expect(screen.getByLabelText('To')).toBeDefined();
});

// N4's ticket asks for a test proving that "a note that gains a contact
// disappears from the inbox". The suite covered the adjacent case (a response
// with no notes renders as empty) but never the assign action itself, so the
// filing path -- the whole point of the inbox -- was untested.
test('assigning a contact files the note and removes it from the inbox', async () => {
  // The server returns the note as unfiled first; once it has been PUT with a
  // contact_id it no longer matches `contact_id IS NULL`, so the refetch that
  // follows a save returns an empty inbox. Flipping this flag is what models
  // that server-side behaviour.
  let filed = false;
  const putBodies: string[] = [];

  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, options?: { method?: string; body?: string }) => {
      if (options?.method === 'PUT') {
        putBodies.push(options.body ?? '');
        filed = true;
        return { ok: true, json: async () => ({ message: 'ok' }) };
      }
      // The dialog's debounced contact search.
      if (url.includes('/contacts?')) {
        return {
          ok: true,
          json: async () => ({
            contacts: [{ id: 7, uid: 'uid-7', firstname: 'Charlie', lastname: 'Chaplin' }],
          }),
        };
      }
      if (url.includes('/notes')) {
        return {
          ok: true,
          json: async () =>
            filed
              ? { notes: [], next_cursor: '', limit: 25, total: 0 }
              : {
                  notes: [
                    {
                      ID: 1,
                      content: 'File me onto someone',
                      date: '2026-01-01T00:00:00Z',
                      contact_id: null,
                      CreatedAt: '2026-01-01T00:00:00Z',
                      UpdatedAt: '2026-01-01T00:00:00Z',
                    },
                  ],
                  next_cursor: '',
                  limit: 25,
                  total: 1,
                },
        };
      }
      throw new Error(`unexpected fetch: ${url}`);
    }),
  );

  renderPage();

  await waitFor(() => expect(screen.getByText('File me onto someone')).toBeDefined());
  expect(screen.getByText('1')).toBeDefined();

  // Open the note for editing (the row's icon button).
  const editButtons = screen.getAllByRole('button');
  fireEvent.click(editButtons[editButtons.length - 1]);

  await waitFor(() => expect(screen.getByRole('dialog')).toBeDefined());

  // Assign a contact through the debounced Autocomplete.
  const picker = screen.getByPlaceholderText('Search contacts...');
  fireEvent.change(picker, { target: { value: 'Charlie' } });
  await waitFor(() => expect(screen.getByText('Charlie Chaplin')).toBeDefined(), { timeout: 3000 });
  fireEvent.click(screen.getByText('Charlie Chaplin'));

  fireEvent.click(screen.getByRole('button', { name: 'Save' }));

  // The save must actually carry the contact id -- filing is the point.
  await waitFor(() => expect(putBodies.length).toBe(1));
  expect(JSON.parse(putBodies[0]).contact_id).toBe(7);

  // ...and the note must leave the inbox, with the queue depth following it.
  await waitFor(() => expect(screen.queryByText('File me onto someone')).toBeNull());
  expect(screen.getByText('No unfiled notes')).toBeDefined();
  expect(screen.getByText('0')).toBeDefined();
});

// The chip is a queue depth, so it must render the server's `total` rather
// than the number of rows on the loaded page. Rendering notes.length
// under-counted anyone with more than one page of unfiled notes, and the
// number then grew as they clicked "Load more".
test('unfiled count shows the server total, not the loaded page length', async () => {
  mockFetchByUrl({
    '/notes?': () => ({
      notes: [
        {
          ID: 1,
          content: 'First note',
          date: '2026-01-01T00:00:00Z',
          contact_id: null,
          CreatedAt: '2026-01-01T00:00:00Z',
          UpdatedAt: '2026-01-01T00:00:00Z',
        },
        {
          ID: 2,
          content: 'Second note',
          date: '2026-01-02T00:00:00Z',
          contact_id: null,
          CreatedAt: '2026-01-02T00:00:00Z',
          UpdatedAt: '2026-01-02T00:00:00Z',
        },
      ],
      next_cursor: 'more-pages-exist',
      limit: 2,
      total: 42,
    }),
  });
  renderPage();

  await waitFor(() => expect(screen.getByText('First note')).toBeDefined());

  expect(screen.getByText('42')).toBeDefined();
  expect(screen.queryByText('2'), 'must not render the page length as the queue depth').toBeNull();
});
