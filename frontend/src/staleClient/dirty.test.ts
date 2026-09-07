import { afterEach, describe, expect, test, vi } from 'vitest';
import {
  isAnythingDirty,
  nextDirtyKey,
  onDirtyChange,
  reportDirty,
  resetDirtyTrackerForTest,
} from './dirty';

afterEach(() => {
  resetDirtyTrackerForTest();
});

describe('dirty registry', () => {
  test('starts clean', () => {
    expect(isAnythingDirty()).toBe(false);
  });

  test('reporting a key dirty makes the app dirty', () => {
    reportDirty(nextDirtyKey('form'), true);
    expect(isAnythingDirty()).toBe(true);
  });

  test('reporting clean removes the key', () => {
    const key = nextDirtyKey('form');
    reportDirty(key, true);
    reportDirty(key, false);
    expect(isAnythingDirty()).toBe(false);
  });

  test('reporting the same key dirty twice is idempotent', () => {
    const key = nextDirtyKey('form');
    reportDirty(key, true);
    reportDirty(key, true);
    expect(isAnythingDirty()).toBe(true);
  });

  test('one clean surface does not clear another dirty one', () => {
    const a = nextDirtyKey('note');
    const b = nextDirtyKey('activity');
    reportDirty(a, true);
    reportDirty(b, true);
    reportDirty(a, false);
    expect(isAnythingDirty()).toBe(true);
  });

  test('only the last clean report clears everything', () => {
    const a = nextDirtyKey('note');
    const b = nextDirtyKey('activity');
    reportDirty(a, true);
    reportDirty(b, true);
    reportDirty(a, false);
    reportDirty(b, false);
    expect(isAnythingDirty()).toBe(false);
  });

  test('notifies the subscriber on every change', () => {
    const fn = vi.fn();
    onDirtyChange(fn);

    const key = nextDirtyKey('form');
    reportDirty(key, true);
    expect(fn).toHaveBeenCalledTimes(1);
    reportDirty(key, false);
    expect(fn).toHaveBeenCalledTimes(2);
  });

  test('does not notify when a report does not change anything', () => {
    const fn = vi.fn();
    onDirtyChange(fn);

    const key = nextDirtyKey('form');
    reportDirty(key, false); // already clean
    reportDirty(key, false);
    expect(fn).not.toHaveBeenCalled();
  });

  test('unsubscribe stops notifications', () => {
    const fn = vi.fn();
    const unsubscribe = onDirtyChange(fn);
    unsubscribe();

    reportDirty(nextDirtyKey('form'), true);
    expect(fn).not.toHaveBeenCalled();
  });

  test('nextDirtyKey allocates distinct stable keys', () => {
    expect(nextDirtyKey('note')).not.toBe(nextDirtyKey('note'));
  });
});
