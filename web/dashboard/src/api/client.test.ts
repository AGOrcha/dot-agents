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
    setAuthToken('fake-test-token')
    const spy = mockFetch(200, { data: { ok: true } })

    await apiFetch('/test')

    const [, init] = spy.mock.calls[0] as [string, RequestInit]
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer fake-test-token')
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

  it('returns false when response body is missing access_token', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      // 200 OK but data.access_token is absent
      new Response(JSON.stringify({ data: {} }), { status: 200 }),
    )
    const ok = await refreshAccessToken()
    expect(ok).toBe(false)
    expect(getAccessToken()).toBeNull()
  })
})

// ---------------------------------------------------------------------------
// apiFetch error-body edge cases
// ---------------------------------------------------------------------------

describe('apiFetch error body edge cases', () => {
  it('falls back to statusText when error body is not valid JSON', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('not json', { status: 502, statusText: 'Bad Gateway' }),
    )

    await expect(apiFetch('/runs')).rejects.toMatchObject({
      message: 'Bad Gateway',
      status: 502,
    })
  })

  it('falls back to "internal" code when error body code field is absent', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { message: 'oops' } }), { status: 500 }),
    )

    await expect(apiFetch('/runs')).rejects.toMatchObject({
      code: 'internal',
      message: 'oops',
    })
  })

  it('falls back to statusText message when error message field is absent', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'not_found' } }), {
        status: 404,
        statusText: 'Not Found',
      }),
    )

    await expect(apiFetch('/runs')).rejects.toMatchObject({
      code: 'not_found',
      message: 'Not Found',
    })
  })

  it('does not attempt a refresh on 401 when no access token is held', async () => {
    // _accessToken is null (cleared in beforeEach); 401 should throw directly without refresh
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'no token' } }), {
        status: 401,
      }),
    )

    await expect(apiFetch('/runs')).rejects.toMatchObject({ status: 401 })
    // Only one fetch call — no refresh attempt
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })

  it('forwards custom request headers when init.headers are provided', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ data: { ok: true } }), { status: 200 }),
    )

    await apiFetch('/runs', { headers: { 'X-Request-ID': 'trace-42' } })

    const [, init] = spy.mock.calls[0] as [string, RequestInit]
    // Custom header must be forwarded (init?.headers ?? {} branch covered)
    expect((init.headers as Record<string, string>)['X-Request-ID']).toBe('trace-42')
  })
})
