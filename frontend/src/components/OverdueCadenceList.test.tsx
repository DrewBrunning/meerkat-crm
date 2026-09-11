import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test } from 'vitest';
import '../i18n/config';
import type { OverdueCadence } from '../api/cadencePolicies';
import { DateFormatProvider } from '../DateFormatProvider';
import OverdueCadenceList from './OverdueCadenceList';

afterEach(cleanup);

function overdueItem(overrides: Partial<OverdueCadence> = {}): OverdueCadence {
  return {
    policy: {
      id: 'policy-1',
      entity_id: 'alice-uid',
      target_interval_days: 30,
      qualifying_types: ['call'],
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
    health: { has_qualifying_interaction: true, overdue_by: 12, next_due: '2026-01-20T00:00:00Z' },
    contact_id: 7,
    contact_name: 'Alice Smith',
    ...overrides,
  };
}

function renderList(props: Partial<React.ComponentProps<typeof OverdueCadenceList>> = {}) {
  const defaults: React.ComponentProps<typeof OverdueCadenceList> = {
    overdue: [],
    loading: false,
    error: null,
    ...props,
  };
  return render(
    <MemoryRouter>
      <DateFormatProvider>
        <OverdueCadenceList {...defaults} />
      </DateFormatProvider>
    </MemoryRouter>,
  );
}

test('renders each overdue contact with the overdue badge and a link to the contact', () => {
  renderList({ overdue: [overdueItem()] });

  expect(screen.getByText('Alice Smith')).toBeInTheDocument();
  expect(screen.getByText('12 days overdue')).toBeInTheDocument();
  // The row is a router Link to /contacts/<numeric id>.
  const link = screen.getByRole('link');
  expect(link.getAttribute('href')).toBe('/contacts/7');
  // #196: the decorative avatar initial must not double into the link name.
  expect(link).toHaveAccessibleName(/^Alice Smith/); // not "AAlice Smith"
});

test('shows the empty state when nothing is overdue', () => {
  renderList({ overdue: [] });
  expect(screen.getByText("Nothing overdue. You're all caught up.")).toBeInTheDocument();
});

test('falls back to the unknown-contact label when a contact name is missing', () => {
  renderList({ overdue: [overdueItem({ contact_name: '' })] });
  expect(screen.getByText('Unknown contact')).toBeInTheDocument();
});
