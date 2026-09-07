import { cleanup, render } from '@testing-library/react';
import { createElement } from 'react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import { isAnythingDirty, resetDirtyTrackerForTest } from '../staleClient/dirty';
import { useBeforeUnloadGuard } from './useBeforeUnloadGuard';

function Harness({ isDirty }: { isDirty: boolean }) {
  useBeforeUnloadGuard(isDirty);
  return null;
}

function fireBeforeUnload(): BeforeUnloadEvent {
  const event = new Event('beforeunload', { cancelable: true }) as BeforeUnloadEvent;
  window.dispatchEvent(event);
  return event;
}

afterEach(() => {
  // This codebase's vitest setup has no auto-cleanup: without unmounting,
  // each test's <Harness> (and its beforeunload listener) would stay live
  // for the rest of the file, so a later "not dirty" test would still see
  // the previous test's warning fire.
  cleanup();
  resetDirtyTrackerForTest();
  vi.restoreAllMocks();
});

describe('useBeforeUnloadGuard', () => {
  test('does nothing when not dirty', () => {
    render(createElement(Harness, { isDirty: false }));
    const event = fireBeforeUnload();
    expect(event.defaultPrevented).toBe(false);
  });

  test('warns (preventDefault + returnValue) when dirty', () => {
    render(createElement(Harness, { isDirty: true }));
    const event = fireBeforeUnload();
    expect(event.defaultPrevented).toBe(true);
    // jsdom reflects an empty-string assignment to the legacy `returnValue`
    // as boolean false, not as the string itself -- the spec-compliant part
    // that matters (and that every real browser honors) is that it's falsy.
    expect(event.returnValue).toBeFalsy();
  });

  test('stops warning once isDirty flips back to false', () => {
    const { rerender } = render(createElement(Harness, { isDirty: true }));
    rerender(createElement(Harness, { isDirty: false }));

    const event = fireBeforeUnload();
    expect(event.defaultPrevented).toBe(false);
  });

  test('removes the listener on unmount', () => {
    const { unmount } = render(createElement(Harness, { isDirty: true }));
    unmount();

    const event = fireBeforeUnload();
    expect(event.defaultPrevented).toBe(false);
  });

  test('reports dirty state to the app-wide registry while dirty', () => {
    render(createElement(Harness, { isDirty: true }));
    expect(isAnythingDirty()).toBe(true);
  });

  test('clears its registry slot when isDirty flips back to false', () => {
    const { rerender } = render(createElement(Harness, { isDirty: true }));
    rerender(createElement(Harness, { isDirty: false }));
    expect(isAnythingDirty()).toBe(false);
  });

  test('clears its registry slot on unmount while dirty', () => {
    const { unmount } = render(createElement(Harness, { isDirty: true }));
    unmount();
    expect(isAnythingDirty()).toBe(false);
  });

  test('two mounted editing surfaces are tracked independently', () => {
    const first = render(createElement(Harness, { isDirty: true }));
    const second = render(createElement(Harness, { isDirty: true }));
    expect(isAnythingDirty()).toBe(true);

    first.unmount();
    expect(isAnythingDirty()).toBe(true); // second is still dirty

    second.unmount();
    expect(isAnythingDirty()).toBe(false);
  });
});
