/**
 * Thin fetch wrapper for the R2 observability dashboard API.
 *
 * Base URL: import.meta.env.VITE_API_BASE (default '' → same-origin).
 * API root: /api/v1/observability  (per API.md §1.1 [[api-conventions]]).
 *
 * R5 auth seam — secure SPA token handling:
 *   Access token: held in a module-scoped variable (app runtime memory only).
 *   It is NEVER written to localStorage or sessionStorage; XSS cannot read it.
 *
 *   Refresh token: stored as an HttpOnly cookie by the R5 backend on login.
 *   JS cannot read it (by design). When the access token expires the client
 *   calls refreshAccessToken(), which posts to the refresh endpoint with
 *   `credentials: 'include'` so the browser automatically attaches the HttpOnly
 *   cookie. A fresh access token is returned and stored in memory.
 *
 *   R5's auth layer should call `setAuthToken(token)` after login and
 *   `clearAuthToken()` on logout. The fetch wrapper picks up the in-memory
 *   value on each call — no React state needed for this cross-cutting concern.
 */

// Module-scoped in-memory access token. Never persisted to Web Storage.
let _accessToken: string | null = null

/** R5 seam: store the Bearer access token in memory. */
export function setAuthToken(token: string): void {
  _accessToken = token
}

/** R5 seam: clear the in-memory access token (revert to anonymous same-origin). */
export function clearAuthToken(): void {
  _accessToken = null
}

/** Exported for tests only — do not call in production code. */
export function getAccessToken(): string | null {
  return _accessToken
}

function getAuthHeaders(): HeadersInit {
  return _accessToken ? { Authorization: `Bearer ${_accessToken}` } : {}
}

const BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? ''
export const API_ROOT = `${BASE}/api/v1/observability`

/**
 * Refresh endpoint (R5 backend contract).
 * POST /api/v1/auth/refresh with credentials:'include' — the browser sends the
 * HttpOnly refresh-token cookie automatically; the server returns a new access
 * token in the response body: { data: { access_token: string } }.
 */
const REFRESH_URL = `${BASE}/api/v1/auth/refresh`

/**
 * Attempt to obtain a new access token using the HttpOnly refresh cookie.
 * Returns true if a new token was stored; false if the refresh failed (user
 * must re-authenticate).
 */
export async function refreshAccessToken(): Promise<boolean> {
  try {
    const res = await fetch(REFRESH_URL, {
      method: 'POST',
      credentials: 'include', // browser sends HttpOnly refresh-token cookie
    })
    if (!res.ok) return false
    const body = await res.json() as { data: { access_token: string } }
    const token = body?.data?.access_token
    if (!token) return false
    _accessToken = token
    return true
  } catch {
    return false
  }
}

export interface ApiError extends Error {
  code: string
  status: number
}

/** Unwraps the `{ data, meta }` response envelope and returns `data`. */
export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const doFetch = () =>
    fetch(`${API_ROOT}${path}`, {
      ...init,
      headers: {
        Accept: 'application/json',
        ...getAuthHeaders(),
        ...(init?.headers ?? {}),
      },
    })

  let res = await doFetch()

  // On 401 attempt a single silent token refresh, then retry once.
  if (res.status === 401 && _accessToken !== null) {
    const refreshed = await refreshAccessToken()
    if (refreshed) {
      res = await doFetch()
    }
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({
      error: { code: 'internal', message: res.statusText },
    })) as { error: { code: string; message: string } }
    const err = new Error(body.error?.message ?? res.statusText) as ApiError
    err.code = body.error?.code ?? 'internal'
    err.status = res.status
    throw err
  }

  const envelope = await res.json() as { data: T }
  return envelope.data
}
