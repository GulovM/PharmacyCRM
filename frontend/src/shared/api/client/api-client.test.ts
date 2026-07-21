import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClient, ApiTransportError } from './api-client'
import { tokenStore } from './token-store'

afterEach(() => { tokenStore.clear(); vi.unstubAllGlobals() })

describe('ApiClient', () => {
  it('keeps request ID as transport metadata and clears a lost session', async () => {
    tokenStore.set('token')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ success: false, error: { code: 'UNAUTHENTICATED', message: 'no' }, meta: { request_id: 'body-id' } }), { status: 401, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'header-id' } })))
    const result = await new ApiClient('https://api.example').request('/protected')
    expect(result.requestID).toBe('header-id')
    expect(result.envelope).toMatchObject({ success: false, error: { code: 'UNAUTHENTICATED' } })
    expect(tokenStore.get()).toBeUndefined()
  })

  it('returns a valid success envelope', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ success: true, data: { id: '1' }, meta: { request_id: 'body-id' } }), { status: 200, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'header-id' } })))
    const result = await new ApiClient('https://api.example').request<{ id: string }>('/resource')
    expect(result).toEqual({ envelope: { success: true, data: { id: '1' }, meta: { request_id: 'body-id' } }, requestID: 'header-id' })
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

  it.each([
    [200, false, 'body-id'],
    [500, true, 'body-id'],
  ])('rejects HTTP %i with contradictory success=%s', async (status, success, requestID) => {
    const body = success
      ? { success: true, data: {}, meta: { request_id: requestID } }
      : { success: false, error: { code: 'CONFLICT', message: 'safe' }, meta: { request_id: requestID } }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })))
    await expect(new ApiClient('https://api.example').request('/resource')).rejects.toEqual(expect.objectContaining({ status, requestID, message: 'API response status contradicts success envelope' }))
  })

  it('clears the session before rejecting a contradictory HTTP 401 response', async () => {
    tokenStore.set('token')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ success: true, data: {}, meta: { request_id: 'body-id' } }), { status: 401, headers: { 'Content-Type': 'application/json' } })))
    await expect(new ApiClient('https://api.example').request('/resource')).rejects.toEqual(expect.objectContaining({ status: 401, requestID: 'body-id' }))
    expect(tokenStore.get()).toBeUndefined()
  })

  it('rejects invalid JSON without exposing its body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{not-json}', { status: 500, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'request-1' } })))
    await expect(new ApiClient('https://api.example').request('/resource')).rejects.toEqual(expect.objectContaining({ status: 500, requestID: 'request-1', message: 'API response JSON is invalid' }))
  })
})
