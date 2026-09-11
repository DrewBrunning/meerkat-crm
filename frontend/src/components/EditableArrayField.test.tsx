import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, test } from 'vitest';
import '../i18n/config';
import EditableArrayField from './EditableArrayField';

afterEach(cleanup);

function renderField() {
  return render(
    <EditableArrayField<string>
      icon={<span data-testid="icon" />}
      label="Emails"
      value="a@example.com"
      renderDisplay={(v) => <span>{v}</span>}
      renderEditor={(draft, setDraft) => (
        <input aria-label="edit emails" value={draft} onChange={(e) => setDraft(e.target.value)} />
      )}
      cloneValue={(v) => v}
      onSave={async () => {}}
    />,
  );
}

// #196 (audit gap N-1): the hover-reveal edit pencil was keyboard-invisible
// (opacity:0 with no :focus-within reveal) and measured 22x22, under the
// 2.5.8 (AA) 24x24 minimum. It must now mirror EditableField.tsx.
test('edit pencil is named, keyboard-revealable, and >= 24x24', () => {
  renderField();

  const btn = screen.getByRole('button', { name: 'Edit' });
  expect(btn).toHaveClass('edit-button');

  const css = Array.from(document.querySelectorAll('style'))
    .map((s) => s.textContent || '')
    .join('\n');

  // keyboard focus (not just mouse hover) reveals the pencil
  expect(css).toContain(':focus-within .edit-button');
  // hit area floored to the 2.5.8 minimum
  expect(css).toContain('min-width:24px');
  expect(css).toContain('min-height:24px');
});
