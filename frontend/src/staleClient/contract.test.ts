import { afterEach, describe, expect, test } from 'vitest';
import type { HealthResponse } from '../api/health';
import { assessServerContract } from './contract';
import { resetClientBuildIdentityForTest, setClientBuildIdentityForTest } from './version';

// Builds the smallest /health payload assessServerContract reads. The
// defaults model a v0.6.10+ server that matches a stamped client exactly:
// same version, "v1" contract, no declared floor.
function health(overrides: Partial<HealthResponse> = {}): HealthResponse {
  return {
    status: 'healthy',
    timestamp: '2026-09-06T00:00:00Z',
    database: { status: 'healthy', response_time_ms: 1 },
    version: '0.6.10',
    api_contract_version: 'v1',
    ...overrides,
  };
}

afterEach(() => {
  resetClientBuildIdentityForTest();
});

describe('assessServerContract', () => {
  test('an unversioned (dev/test) build is inert — never blocked, never prompted', () => {
    expect(assessServerContract(health({ min_client_version: '99.0.0' }))).toEqual({
      severity: 'compatible',
      reason: 'inert',
    });
  });

  test('an identical server and client are compatible', () => {
    setClientBuildIdentityForTest('0.6.10');
    expect(assessServerContract(health())).toEqual({
      severity: 'compatible',
      reason: 'compatible',
    });
  });

  test('a server that predates api_contract_version is treated as v1', () => {
    setClientBuildIdentityForTest('0.6.10');
    const server = health();
    delete server.api_contract_version;
    expect(assessServerContract(server)).toEqual({
      severity: 'compatible',
      reason: 'compatible',
    });
  });

  test('a server without a floor never blocks on the floor', () => {
    setClientBuildIdentityForTest('0.6.0');
    expect(
      assessServerContract(health({ min_client_version: undefined, version: '0.6.0' })),
    ).toEqual({ severity: 'compatible', reason: 'compatible' });
  });

  test('a client below the declared floor is blocked', () => {
    setClientBuildIdentityForTest('0.6.8');
    expect(assessServerContract(health({ min_client_version: '0.6.10' }))).toEqual({
      severity: 'blocked',
      reason: 'below-floor',
    });
  });

  test('a client exactly at the declared floor is not blocked', () => {
    setClientBuildIdentityForTest('0.6.10');
    expect(assessServerContract(health({ min_client_version: '0.6.10' }))).toEqual({
      severity: 'compatible',
      reason: 'compatible',
    });
  });

  test('a different api contract generation is blocked ahead of any floor check', () => {
    setClientBuildIdentityForTest('0.6.8');
    expect(assessServerContract(health({ api_contract_version: 'v2' }))).toEqual({
      severity: 'blocked',
      reason: 'contract-mismatch',
    });
  });

  test('an unparseable floor fails open rather than blocking', () => {
    setClientBuildIdentityForTest('0.6.8');
    expect(assessServerContract(health({ min_client_version: 'not-a-version' }))).not.toEqual({
      severity: 'blocked',
      reason: 'below-floor',
    });
  });

  test('a newer server build is update-available when no floor blocks it', () => {
    setClientBuildIdentityForTest('0.6.8');
    expect(assessServerContract(health({ version: '0.6.10' }))).toEqual({
      severity: 'update-available',
      reason: 'server-newer',
    });
  });

  test('an equal server build is not update-available', () => {
    setClientBuildIdentityForTest('0.6.8');
    expect(assessServerContract(health({ version: '0.6.8' }))).toEqual({
      severity: 'compatible',
      reason: 'compatible',
    });
  });

  test('an unparseable server version does not trigger update-available', () => {
    setClientBuildIdentityForTest('0.6.8');
    expect(assessServerContract(health({ version: 'dev' }))).toEqual({
      severity: 'compatible',
      reason: 'compatible',
    });
  });

  test('blocked wins over update-available when the server is both newer and above the floor', () => {
    setClientBuildIdentityForTest('0.6.8');
    expect(
      assessServerContract(health({ version: '0.6.10', min_client_version: '0.6.10' })),
    ).toEqual({ severity: 'blocked', reason: 'below-floor' });
  });
});
