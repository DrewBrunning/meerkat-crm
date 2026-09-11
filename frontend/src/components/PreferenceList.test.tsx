import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import '../i18n/config';
import type { Preference } from '../api/preferences';
import PreferenceList from './PreferenceList';

afterEach(cleanup);

function preference(overrides: Partial<Preference> = {}): Preference {
  return {
    id: 'p1',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    entity_id: 'alice-uid',
    category: 'food',
    value: 'Vegetarian',
    sensitivity: 'normal',
    ...overrides,
  };
}

test('renders preferences grouped into Food & Drink and Media sections', () => {
  render(
    <PreferenceList
      preferences={[
        preference(),
        preference({ id: 'p2', category: 'media_tv', key: 'favorite', value: 'Severance' }),
      ]}
      onEdit={vi.fn()}
      onDelete={vi.fn()}
    />,
  );

  expect(screen.getByText('Vegetarian')).toBeInTheDocument();
  expect(screen.getByText('Severance')).toBeInTheDocument();
  expect(screen.getByText('Food & Drink Preferences')).toBeInTheDocument();
  expect(screen.getByText('Media Preferences')).toBeInTheDocument();
  expect(screen.getByText('Favorite')).toBeInTheDocument();
});

// #196 (audit gap, N-1's sibling): the edit/delete actions were revealed only
// on `&:hover .preference-actions` -- no `:focus-within` -- so keyboard focus
// landed on a fully transparent control.
test('edit/delete actions are keyboard-revealable', () => {
  render(<PreferenceList preferences={[preference()]} onEdit={vi.fn()} onDelete={vi.fn()} />);

  const css = Array.from(document.querySelectorAll('style'))
    .map((s) => s.textContent || '')
    .join('\n');
  expect(css).toContain(':focus-within .preference-actions');
});

test('jewelry and gift-preference categories land in their own sections', () => {
  render(
    <PreferenceList
      preferences={[
        preference({ id: 'p1', category: 'jewelry_metal', key: 'allergy', value: 'Nickel' }),
        preference({ id: 'p2', category: 'flowers', key: 'favorite', value: 'Peonies' }),
        preference({ id: 'p3', category: 'dislike', value: 'Candles' }),
      ]}
      onEdit={vi.fn()}
      onDelete={vi.fn()}
    />,
  );

  expect(screen.getByText('Jewelry & Style')).toBeInTheDocument();
  expect(screen.getByText('Nickel')).toBeInTheDocument();
  expect(screen.getByText('Gift Preferences')).toBeInTheDocument();
  expect(screen.getByText('Peonies')).toBeInTheDocument();
  expect(screen.getByText('Gift Avoid')).toBeInTheDocument();
  expect(screen.getByText('Candles')).toBeInTheDocument();
});

test('an unrecognized category falls through to the Other section', () => {
  render(
    <PreferenceList
      preferences={[preference({ id: 'p3', category: 'ancient_category', value: 'Legacy data' })]}
      onEdit={vi.fn()}
      onDelete={vi.fn()}
    />,
  );

  expect(screen.getByText('Other Preferences')).toBeInTheDocument();
  expect(screen.getByText('Legacy data')).toBeInTheDocument();
  expect(screen.queryByText('Food & Drink Preferences')).not.toBeInTheDocument();
  expect(screen.queryByText('Media Preferences')).not.toBeInTheDocument();
});

test('shows notes when present', () => {
  render(
    <PreferenceList
      preferences={[
        preference({ key: 'dislike', value: 'Alcohol', notes: "Doesn't drink alcohol" }),
      ]}
      onEdit={vi.fn()}
      onDelete={vi.fn()}
    />,
  );

  expect(screen.getByText('Alcohol')).toBeInTheDocument();
  expect(screen.getByText("Doesn't drink alcohol")).toBeInTheDocument();
});

test('shows the sensitivity chip only for non-normal preferences', () => {
  render(
    <PreferenceList
      preferences={[
        preference(),
        preference({ id: 'p2', category: 'hobby', value: 'Secret project', sensitivity: 'secret' }),
      ]}
      onEdit={vi.fn()}
      onDelete={vi.fn()}
    />,
  );

  expect(screen.getByText('Secret')).toBeInTheDocument();
  // Only one non-normal chip across the two entries.
  expect(screen.getAllByText('Secret')).toHaveLength(1);
});

test('edit and delete call their handlers', () => {
  const onEdit = vi.fn();
  const onDelete = vi.fn();
  const pref = preference();

  render(<PreferenceList preferences={[pref]} onEdit={onEdit} onDelete={onDelete} />);

  fireEvent.click(screen.getByLabelText('Edit'));
  expect(onEdit).toHaveBeenCalledWith(pref);

  fireEvent.click(screen.getByLabelText('Delete'));
  expect(onDelete).toHaveBeenCalledWith('p1');
});

test('shows the empty state', () => {
  render(<PreferenceList preferences={[]} onEdit={vi.fn()} onDelete={vi.fn()} />);
  expect(screen.getByText('No preferences yet')).toBeInTheDocument();
});
