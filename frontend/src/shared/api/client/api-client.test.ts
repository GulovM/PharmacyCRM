import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClient, ApiTransportError } from './api-client'
import { tokenStore } from './token-store'

afterEach(() => { tokenStore.clear(); vi.unstubAllGlobals() })

describe('ApiClient', () => {
  it('keeps request ID as transport metadata and clears a lost session', async () => {
    tokenStore.set('token')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ success: false, error: { code: 'UNAUTHENTICATED', message: 'no' }, meta: { request_id: 'body-id' } }), { status: 200, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'header-id' } })))
    const result = await new ApiClient('https://api.example').request('/protected')
    expect(result.requestID).toBe('header-id')
    expect(tokenStore.get()).toBeUndefined()
  })

  it('rejects non-JSON and failed transport responses', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('down', { status: 503, headers: { 'X-Request-ID': 'request-1' } })))
    await expect(new ApiClient('https://api.example').request('/healthz')).rejects.toEqual(expect.objectContaining({ status: 503, requestID: 'request-1' }))
  })

  it.each([
    [401, 'UNAUTHENTICATED'],
    [409, 'CONFLICT'],
    [422, 'BUSINESS_RULE_VIOLATION'],
    [503, 'SERVICE_UNAVAILABLE'],
  ])('preserves the JSON failure envelope for HTTP %i', async (status, code) => {
    tokenStore.set('token')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      success: false,
      error: { code, message: 'safe', details: [{ field: 'name', code: 'INVALID', message: 'invalid' }] },
      meta: { request_id: `body-${status}` },
    }), { status, headers: { 'Content-Type': 'application/json' } })))

    const result = await new ApiClient('https://api.example').request('/resource')
    expect(result.requestID).toBe(`body-${status}`)
    expect(result.envelope).toMatchObject({ success: false, error: { code, details: [{ field: 'name', code: 'INVALID' }] } })
    expect(tokenStore.get()).toBe(status === 401 ? undefined : 'token')
  })

  it('uses a transport error for a malformed JSON contract', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ problem: true }), { status: 503, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'request-1' } })))
    await expect(new ApiClient('https://api.example').request('/healthz')).rejects.toBeInstanceOf(ApiTransportError)
  })
})
