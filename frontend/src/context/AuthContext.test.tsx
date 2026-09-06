import { renderHook } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import type { ReactNode } from 'react';
import { AuthContext, useAuth } from './AuthContext';

describe('AuthContext', () => {
  test('useAuth returns the provided token and setToken', () => {
    const setToken = vi.fn();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <AuthContext.Provider value={{ token: 'cookie-auth', setToken }}>
        {children}
      </AuthContext.Provider>
    );

    const { result } = renderHook(() => useAuth(), { wrapper });

    expect(result.current.token).toBe('cookie-auth');
    expect(result.current.setToken).toBe(setToken);
  });

  test('useAuth throws outside an AuthContext.Provider', () => {
    // The app-shell always renders inside the provider (App owns the token
    // state and provides it around <RouterProvider>); a bare call is a
    // wiring bug and should fail loudly rather than render with a null token.
    expect(() => renderHook(() => useAuth())).toThrow(
      'useAuth must be used within an AuthContext.Provider',
    );
  });
});
