import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import '../i18n/config';
import type { Gift } from '../api/gifts';
import { DateFormatProvider } from '../DateFormatProvider';
import ContactTimeline from './ContactTimeline';

// This codebase's vitest setup does not auto-cleanup between tests.
afterEach(cleanup);

const givenGift: Gift = {
  id: 'gift-1',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  entity_id: 'alice-uid',
  status: 'given',
  description: 'The espresso machine',
  occasion: 'birthday',
  date: '2026-08-03T12:00:00Z',
};

function renderTimeline(items: React.ComponentProps<typeof ContactTimeline>['timelineItems']) {
  return render(
    <MemoryRouter>
      <DateFormatProvider>
        <ContactTimeline timelineItems={items} onEditItem={vi.fn()} />
      </DateFormatProvider>
    </MemoryRouter>,
  );
}

// #196 (audit gap N-1): a timeline item's edit/delete icon was revealed only
// on `&:hover .action-icon` -- no `:focus-within` -- so keyboard focus landed
// on a fully transparent control.
test('edit/delete action icon is keyboard-revealable', () => {
  renderTimeline([
    {
      type: 'note',
      date: '2026-01-01T00:00:00Z',
      data: {
        ID: 1,
        content: 'Met at the conference',
        date: '2026-01-01T00:00:00Z',
        CreatedAt: '2026-01-01T00:00:00Z',
        UpdatedAt: '2026-01-01T00:00:00Z',
      },
    },
  ]);

  const css = Array.from(document.querySelectorAll('style'))
    .map((s) => s.textContent || '')
    .join('\n');
  expect(css).toContain(':focus-within .action-icon');
});

test('renders a given gift with its label, description, and occasion', () => {
  renderTimeline([{ type: 'gift', data: givenGift, date: givenGift.date! }]);

  expect(screen.getByText('Gift given')).toBeInTheDocument();
  expect(screen.getByText('The espresso machine')).toBeInTheDocument();
  expect(screen.getByText('birthday')).toBeInTheDocument();
});

test('renders a received gift with its own label', () => {
  renderTimeline([
    {
      type: 'gift',
      data: { ...givenGift, status: 'received', description: 'The scarf' },
      date: givenGift.date!,
    },
  ]);

  expect(screen.getByText('Gift received')).toBeInTheDocument();
  expect(screen.getByText('The scarf')).toBeInTheDocument();
});

test('a gift without an occasion does not render the occasion line', () => {
  const { description, ...rest } = givenGift;
  renderTimeline([
    { type: 'gift', data: { ...rest, description, occasion: undefined }, date: givenGift.date! },
  ]);

  expect(screen.getByText('Gift given')).toBeInTheDocument();
  expect(screen.getByText(description)).toBeInTheDocument();
  expect(screen.queryByText('birthday')).not.toBeInTheDocument();
});
