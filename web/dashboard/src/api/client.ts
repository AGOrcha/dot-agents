/**
 * Thin fetch wrapper for the R2 observability dashboard API.
 *
 * Base URL: import.meta.env.VITE_API_BASE (default '' → same-origin).
 * API root: /api/v1/observability  (per API.md §1.1 [[api-conventions]]).
 *
 * R5 seam — Bearer-token plumbing:
 *   When a token is stored in localStorage under AUTH_TOKEN_KEY, every request
 *   carries `Authorization: Bearer <token>`. When absent (v1 default, anonymous
 *   same-origin), no Authorization header is added.
 *   R5's review-ui should call `setAuthToken(token)` after obtaining a token
 *   and `clearAuthToken()` on logout. The fetch wrapper picks up the stored
 *   value on each call — no React state needed for this cross-cutting concern.
 */

export const AUTH_TOKEN_KEY = 'da:auth:token'

/** R5 seam: store a Bearer token so all subsequent API calls include it. */
export function setAuthToken(token: string): void {
  localStorage.setItem(AUTH_TOKEN_KEY, token)
}

/** R5 seam: remove the stored Bearer token (revert to anonymous same-origin). */
export function clearAuthToken(): void {
  localStorage.removeItem(AUTH_TOKEN_KEY)
}

function getAuthHeaders(): HeadersInit {
  const token = localStorage.getItem(AUTH_TOKEN_KEY)
  return token ? { Authorization: `Bearer ${token}` } : {}
}

const BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? ''
export const API_ROOT = `${BASE}/api/v1/observability`

export interface ApiError extends Error {
  code: string
  status: number
}

/** Unwraps the `{ data, meta }` response envelope and returns `data`. */
export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_ROOT}${path}`, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...getAuthHeaders(),
      ...(init?.headers ?? {}),
    },
  })

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
