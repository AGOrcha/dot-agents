/**
 * Tests for the API client auth seam (in-memory token + HttpOnly cookie refresh).
 *
 * Positive path: token set → Authorization header included; refresh on 401.
 * Negative path: no token → no Authorization header; failed refresh throws ApiError.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  setAuthToken,
  clearAuthToken,
  getAccessToken,
  refreshAccessToken,
  apiFetch,
  API_ROOT,
} from './client'

// Reset module state between tests
beforeEach(() => {
  clearAuthToken()
  vi.restoreAllMocks()
})

// ---------------------------------------------------------------------------
// In-memory token store
// ---------------------------------------------------------------------------

describe('setAuthToken / clearAuthToken / getAccessToken', () => {
  it('stores a token in memory', () => {
    setAuthToken('tok-abc')
    expect(getAccessToken()).toBe('tok-abc')
  })

  it('clears the token from memory', () => {
    setAuthToken('tok-abc')
    clearAuthToken()
    expect(getAccessToken()).toBeNull()
  })

  it('never touches localStorage', () => {
    const setSpy = vi.spyOn(Storage.prototype, 'setItem')
    const getSpy = vi.spyOn(Storage.prototype, 'getItem')
    setAuthToken('tok-abc')
    clearAuthToken()
    expect(setSpy).not.toHaveBeenCalled()
    expect(getSpy).not.toHaveBeenCalled()
  })
})

// ---------------------------------------------------------------------------
// Authorization header injection
// ---------------------------------------------------------------------------

describe('apiFetch Authorization header', () => {
  function mockFetch(status: number, body: unknown) {
    return vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(body), { status }),
    )
  }

  it('includes Authorization header when token is set', async () => {
    setAuthToken('bearer-tok-123')
    const spy = mockFetch(200, { data: { ok: true } })

    await apiFetch('/test')

    const [, init] = spy.mock.calls[0] as [string, RequestInit]
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer bearer-tok-123')
  })

  it('omits Authorization header when no token is set', async () => {
    const spy = mockFetch(200, { data: { ok: true } })

    await apiFetch('/test')

    const [, init] = spy.mock.calls[0] as [string, RequestInit]
    expect((init.headers as Record<string, string>)['Authorization']).toBeUndefined()
  })

  it('uses the API_ROOT path', async () => {
    const spy = mockFetch(200, { data: 'result' })
    await apiFetch('/runs')
    expect(spy.mock.calls[0][0]).toBe(`${API_ROOT}/runs`)
  })
})

// ---------------------------------------------------------------------------
// 401 → refresh → retry flow
// ---------------------------------------------------------------------------

describe('apiFetch 401 refresh flow', () => {
  it('retries with new token after successful refresh', async () => {
    setAuthToken('old-tok')

    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        // First call: 401
        new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'expired' } }), {
          status: 401,
        }),
      )
      .mockResolvedValueOnce(
        // Refresh endpoint returns new access token
        new Response(JSON.stringify({ data: { access_token: 'new-tok' } }), { status: 200 }),
      )
      .mockResolvedValueOnce(
        // Retry succeeds
        new Response(JSON.stringify({ data: { value: 42 } }), { status: 200 }),
      )

    const result = await apiFetch<{ value: number }>('/runs')
    expect(result).toEqual({ value: 42 })

    // New token was stored in memory
    expect(getAccessToken()).toBe('new-tok')

    // Three fetch calls: original + refresh + retry
    expect(fetchSpy).toHaveBeenCalledTimes(3)

    // Retry carries the new token
    const [, retryInit] = fetchSpy.mock.calls[2] as [string, RequestInit]
    expect((retryInit.headers as Record<string, string>)['Authorization']).toBe('Bearer new-tok')
  })

  it('throws ApiError when refresh also fails', async () => {
    setAuthToken('old-tok')

    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'expired' } }), {
          status: 401,
        }),
      )
      .mockResolvedValueOnce(
        // Refresh endpoint returns 401 (refresh token also expired)
        new Response('', { status: 401 }),
      )

    await expect(apiFetch('/runs')).rejects.toMatchObject({
      code: 'unauthorized',
      status: 401,
    })
  })
})

// ---------------------------------------------------------------------------
// refreshAccessToken — direct tests
// ---------------------------------------------------------------------------

describe('refreshAccessToken', () => {
  it('posts with credentials:include', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ data: { access_token: 'fresh-tok' } }), { status: 200 }),
    )

    const ok = await refreshAccessToken()
    expect(ok).toBe(true)
    expect(getAccessToken()).toBe('fresh-tok')
    expect(spy.mock.calls[0][1]).toMatchObject({ method: 'POST', credentials: 'include' })
  })

  it('returns false and leaves token unchanged when server returns non-200', async () => {
    setAuthToken('existing-tok')
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('', { status: 401 }),
    )

    const ok = await refreshAccessToken()
    expect(ok).toBe(false)
    // Existing token stays — caller decides whether to clear
    expect(getAccessToken()).toBe('existing-tok')
  })

  it('returns false when fetch throws (network error)', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network down'))
    const ok = await refreshAccessToken()
    expect(ok).toBe(false)
  })
})
