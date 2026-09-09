import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import './i18n/config';
import { getImportHistory } from './api/import';
import DataSettingsPage from './DataSettingsPage';

// This codebase's vitest has no auto-cleanup (CLAUDE.md frontend trap #1).
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

// The page composes several settings panels that each fetch on mount; stub
// them so this test only exercises the issue #651 import-history table.
vi.mock('./components/CalendarSyncSettings', () => ({ default: () => null }));
vi.mock('./components/ContactAddressSuggestions', () => ({ default: () => null }));
vi.mock('./components/ContactFieldSettings', () => ({ default: () => null }));
vi.mock('./components/CustomFieldsSettings', () => ({ default: () => null }));
vi.mock('./components/ExportFieldPickerDialog', () => ({ default: () => null }));
vi.mock('./components/ImportContactsDialog', () => ({ default: () => null }));
vi.mock('./components/MonicaImportDialog', () => ({ default: () => null }));
vi.mock('./components/MeerkatImportDialog', () => ({ default: () => null }));
vi.mock('./components/RelationshipSuggestionsInbox', () => ({ default: () => null }));

vi.mock('./api/import', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/import')>();
  return { ...actual, getImportHistory: vi.fn() };
});

const historyMock = vi.mocked(getImportHistory);

beforeEach(() => {
  historyMock.mockReset();
});

function renderPage() {
  return render(
    <MemoryRouter>
      <DataSettingsPage />
    </MemoryRouter>,
  );
}

test('shows the empty state when there is no import history', async () => {
  historyMock.mockResolvedValue([]);
  renderPage();

  expect(await screen.findByText('No imports yet.')).toBeInTheDocument();
  expect(historyMock).toHaveBeenCalledTimes(1);
});

test('renders one row per import run with its counts', async () => {
  historyMock.mockResolvedValue([
    {
      id: 7,
      format: 'jscontact',
      total_processed: 9,
      created: 5,
      updated: 3,
      skipped: 1,
      error_count: 2,
      created_at: '2026-08-27T12:00:00Z',
    },
  ]);
  renderPage();

  const cell = await screen.findByText('JSContact');
  const row = cell.closest('tr');
  expect(row).not.toBeNull();
  // created / updated / skipped / errors
  expect(row).toHaveTextContent('5');
  expect(row).toHaveTextContent('3');
  expect(row).toHaveTextContent('1');
  expect(row).toHaveTextContent('2');
});

test('surfaces a load error', async () => {
  historyMock.mockRejectedValue(new Error('boom'));
  renderPage();

  await waitFor(() => expect(screen.getByText('boom')).toBeInTheDocument());
});

test('offers the Monica import assistant alongside the file import (issue #549)', async () => {
  historyMock.mockResolvedValue([]);
  renderPage();

  expect(await screen.findByRole('button', { name: 'Import from Monica' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Import from Meerkat' })).toBeInTheDocument();
});

// Issue #861: the CSV export is the user's own full backup, so it carries the
// private/secret items and unconfirmed relationships the vCard/JSContact
// exports hold back. A user who reads "private" as "not in my backup file"
// would hand out more than they meant to, so the panel must say what the file
// contains *before* the download button, not in a doc they never open.
test('warns that the CSV backup includes private and secret data before downloading', async () => {
  historyMock.mockResolvedValue([]);
  renderPage();

  const notice = await screen.findByText(/private or secret/i);
  expect(notice).toBeInTheDocument();
  expect(notice).toHaveTextContent(/vCard or JSContact/i);

  // Positioned ahead of the control it qualifies.
  const download = screen.getByRole('button', { name: 'Download CSV' });
  expect(notice.compareDocumentPosition(download) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});
