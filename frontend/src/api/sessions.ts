import { API_BASE_URL, apiFetch, getAuthHeaders } from './client';
import { handleResponse } from './errorHandling';

// Issue #866: the server-side session inventory (ASVS 3.3.4). One row per
// interactive login; `current` marks the session making the request.
export interface Session {
  id: string;
  created_at: string;
  last_seen_at: string;
  expires_at: string;
  user_agent: string;
  ip: string;
  current: boolean;
}

export interface SessionsListResponse {
  sessions: Session[];
}

export async function getSessions(): Promise<SessionsListResponse> {
  const response = await apiFetch(`${API_BASE_URL}/sessions`, {
    method: 'GET',
    headers: getAuthHeaders(),
  });
  const data = await handleResponse(response, 'Unable to load sessions.');
  return { sessions: data?.sessions || [] };
}

/** Revoke one session by id. Revoking the current session logs this device out. */
export async function revokeSession(id: string): Promise<void> {
  const response = await apiFetch(`${API_BASE_URL}/sessions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: getAuthHeaders(),
  });
  await handleResponse(response, 'Unable to revoke session.');
}

export interface RevokeOtherSessionsResponse {
  revoked: number;
}

/** "Log out everywhere else" — revoke every session except the current one. */
export async function revokeOtherSessions(): Promise<RevokeOtherSessionsResponse> {
  const response = await apiFetch(`${API_BASE_URL}/sessions`, {
    method: 'DELETE',
    headers: getAuthHeaders(),
  });
  const data = await handleResponse(response, 'Unable to revoke other sessions.');
  return data as RevokeOtherSessionsResponse;
}
