import { createContext, useContext } from 'react';

// Issue #805: the token state that decides which branch of the app-shell
// renders (logged-in chrome vs. the bare login/register box) used to flow from
// <App> down through props. A data router's route tree is created once, at
// module scope, so it cannot receive per-render props -- the router's root
// element reads the auth state from this context instead. <App> owns the
// state (and the session-restore / storage-event effects) and provides it here.
export interface AuthState {
  token: string | null;
  setToken: (token: string | null) => void;
}

export const AuthContext = createContext<AuthState | null>(null);

export function useAuth(): AuthState {
  const auth = useContext(AuthContext);
  if (!auth) {
    throw new Error('useAuth must be used within an AuthContext.Provider');
  }
  return auth;
}
