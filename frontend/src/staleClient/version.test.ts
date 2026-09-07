import { afterEach, describe, expect, test } from 'vitest';
import {
  compareClientVersions,
  getClientContractVersion,
  parseClientVersion,
  resetClientBuildIdentityForTest,
} from './version';

afterEach(() => {
  resetClientBuildIdentityForTest();
});

describe('parseClientVersion', () => {
  test.each([
    ['1', { major: 1, minor: 0, patch: 0 }],
    ['0.6', { major: 0, minor: 6, patch: 0 }],
    ['0.6.0', { major: 0, minor: 6, patch: 0 }],
    ['0.6.10', { major: 0, minor: 6, patch: 10 }],
    ['v0.6.0', { major: 0, minor: 6, patch: 0 }],
    ['0.6.0-rc.1', { major: 0, minor: 6, patch: 0 }],
    ['0.7.0+build.7', { major: 0, minor: 7, patch: 0 }],
  ])('parses %s', (input, expected) => {
    const parsed = parseClientVersion(input);
    expect(parsed).toMatchObject(expected);
  });

  test.each(['', 'banana', 'latest', '0,6', '0.6.0.1', '-rc.1', '0.6.', '0.6.0/../../etc', 'dev'])(
    'rejects %s',
    (input) => {
      expect(parseClientVersion(input)).toBeNull();
    },
  );

  test('captures prerelease identifiers separately', () => {
    expect(parseClientVersion('1.2.3-alpha.1.rc')?.prerelease).toEqual(['alpha', '1', 'rc']);
  });

  test('build metadata does not participate in the parsed identity', () => {
    expect(parseClientVersion('1.2.3-rc.1+build.5')?.prerelease).toEqual(['rc', '1']);
  });
});

describe('compareClientVersions', () => {
  test.each([
    ['0.6.0', '0.6.0', 0],
    ['0.6', '0.6.0', 0],
    ['v0.6.0', '0.6.0', 0],
    ['0.6.1', '0.6.0', 1],
    ['0.6', '0.5.9', 1],
    ['0.10.0', '0.9.0', 1], // numeric, not lexical: 10 > 9
    ['0.6.0', '0.6.1', -1],
    ['0.6.0-rc.1', '0.6.0', -1], // a prerelease is below the release it precedes
    ['0.6.0', '0.6.0-rc.1', 1],
    ['0.6.0-alpha', '0.6.0-beta', -1], // lexical among identifiers
    ['0.6.0-rc.1', '0.6.0-rc.2', -1], // numeric identifiers compare numerically
    ['0.6.0-rc.1', '0.6.0-rc.10', -1],
    ['0.6.0-alpha', '0.6.0-alpha.1', -1], // shorter identifier list sorts below a longer equal-prefix one
    ['0.6.0-1', '0.6.0-alpha', -1], // numeric identifiers sort below alphanumeric ones
    ['0.6.0-rc.1+build.5', '0.6.0-rc.1+build.9', 0], // build metadata is ignored
  ])('compares %s vs %s', (a, b, expected) => {
    expect(compareClientVersions(a, b)).toBe(expected);
  });

  test('returns null when either side is unparseable', () => {
    expect(compareClientVersions('0.6.0', 'banana')).toBeNull();
    expect(compareClientVersions('latest', '0.6.0')).toBeNull();
  });
});

describe('client build identity', () => {
  test('the bundle always speaks the v1 contract while the API is on /api/v1', () => {
    expect(getClientContractVersion()).toBe('v1');
  });
});
