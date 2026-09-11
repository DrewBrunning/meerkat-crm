import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import '../i18n/config';
import type { Contact } from '../api/contacts';
import type { RelationshipEdge } from '../api/relationshipEdges';
import RelationshipEdgeList from './RelationshipEdgeList';

// This codebase's vitest setup does not auto-cleanup between tests (no
// `globals: true`, setupTests.ts doesn't register it) -- without this,
// each test's render accumulates in the DOM and later tests see duplicate
// elements from earlier ones.
afterEach(cleanup);

// the household-inference engine that produces `status: suggested`
// edges (services/household_service.go) has no HTTP trigger anywhere in the
// app today (confirmed via grep during planning), so no real suggested edge
// can exist in a running instance yet. This test is the practical substitute
// for e2e coverage of the accept/reject flow until that engine is wired to
// something -- it feeds a mocked suggested-status edge directly.

const viewedContactUid = 'alice-uid';

function suggestedEdge(): RelationshipEdge {
  return {
    id: 'edge-1',
    source_id: 'alice-uid',
    target_id: 'bob-uid',
    type: 'roommate_of',
    directional: false,
    source: 'household-inferred',
    confidence: 0.7,
    status: 'suggested',
    sensitivity: 'normal',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

function bobContact(): Contact {
  return { ID: 2, uid: 'bob-uid', firstname: 'Bob', lastname: 'Brown' };
}

test('renders a suggested edge with the warning chip and working accept/reject actions', () => {
  const onAccept = vi.fn();
  const onReject = vi.fn();
  const edge = suggestedEdge();
  const contactsByUid = new Map([['bob-uid', bobContact()]]);

  vi.stubGlobal(
    'confirm',
    vi.fn(() => true),
  );

  render(
    <MemoryRouter>
      <RelationshipEdgeList
        confirmedEdges={[]}
        suggestedEdges={[edge]}
        contactsByUid={contactsByUid}
        viewedContactUid={viewedContactUid}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onAccept={onAccept}
        onReject={onReject}
      />
    </MemoryRouter>,
  );

  expect(screen.getByText('Bob Brown')).toBeInTheDocument();
  expect(screen.getByText('Suggested')).toBeInTheDocument();

  fireEvent.click(screen.getByLabelText('Accept'));
  expect(onAccept).toHaveBeenCalledWith('edge-1');

  fireEvent.click(screen.getByLabelText('Reject'));
  expect(onReject).toHaveBeenCalledWith('edge-1');

  vi.unstubAllGlobals();
});

test('confirmed edges render without a suggested chip and without accept/reject actions', () => {
  const confirmed: RelationshipEdge = {
    ...suggestedEdge(),
    id: 'edge-2',
    status: 'confirmed',
    source: 'user-confirmed',
    confidence: 1.0,
    type: 'friend_of',
  };
  const contactsByUid = new Map([['bob-uid', bobContact()]]);

  render(
    <MemoryRouter>
      <RelationshipEdgeList
        confirmedEdges={[confirmed]}
        suggestedEdges={[]}
        contactsByUid={contactsByUid}
        viewedContactUid={viewedContactUid}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onAccept={vi.fn()}
        onReject={vi.fn()}
      />
    </MemoryRouter>,
  );

  expect(screen.getByText('Bob Brown')).toBeInTheDocument();
  expect(screen.queryByText('Suggested')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('Accept')).not.toBeInTheDocument();
});

// #196 (audit gap, N-1's sibling): the edit/delete actions were revealed only
// on `&:hover .action-buttons` -- no `:focus-within` -- so keyboard focus
// landed on a fully transparent control.
test('edit/delete actions are keyboard-revealable', () => {
  const confirmed: RelationshipEdge = { ...suggestedEdge(), id: 'edge-3', status: 'confirmed' };
  const contactsByUid = new Map([['bob-uid', bobContact()]]);

  render(
    <MemoryRouter>
      <RelationshipEdgeList
        confirmedEdges={[confirmed]}
        suggestedEdges={[]}
        contactsByUid={contactsByUid}
        viewedContactUid={viewedContactUid}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onAccept={vi.fn()}
        onReject={vi.fn()}
      />
    </MemoryRouter>,
  );

  const css = Array.from(document.querySelectorAll('style'))
    .map((s) => s.textContent || '')
    .join('\n');
  expect(css).toContain(':focus-within .action-buttons');
});

test('shows the empty state when there are no edges at all', () => {
  render(
    <MemoryRouter>
      <RelationshipEdgeList
        confirmedEdges={[]}
        suggestedEdges={[]}
        contactsByUid={new Map()}
        viewedContactUid={viewedContactUid}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onAccept={vi.fn()}
        onReject={vi.fn()}
      />
    </MemoryRouter>,
  );

  expect(screen.getByText('No relationships yet')).toBeInTheDocument();
});
