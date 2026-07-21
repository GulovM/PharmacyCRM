import createClient from 'openapi-fetch'
import type { paths } from '../generated/schema'
import { envelopeSchema, type ApiEnvelope } from '../envelope/types'
import { clearSensitiveSession, tokenStore } from './token-store'

export class ApiTransportError extends Error {
  constructor(message: string, readonly status: number, readonly requestID?: string) {
    super(message)
  }
}

async function authenticatedFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers)
  headers.set('Accept', 'application/json')
  const token = tokenStore.get()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch(input, { ...init, headers })
  if (response.status === 401) clearSensitiveSession()
  return response
}

export function createGeneratedApiClient(baseURL: string) {
  return createClient<paths>({ baseUrl: baseURL, fetch: authenticatedFetch })
}

export type ApiResult<T> = { envelope: ApiEnvelope<T>; requestID: string }

export class ApiClient {
  constructor(private readonly baseURL: string) {}

  async request<T>(path: string, init: RequestInit = {}): Promise<ApiResult<T>> {
    const response = await authenticatedFetch(new URL(path, this.baseURL), init)
    const requestID = response.headers.get('X-Request-ID') ?? ''
    if (!response.ok) throw new ApiTransportError('API request failed', response.status, requestID)
    if (!response.headers.get('content-type')?.toLowerCase().includes('application/json')) {
      throw new ApiTransportError('API response is not JSON', response.status, requestID)
    }
    const envelope = envelopeSchema.parse(await response.json()) as ApiEnvelope<T>
    if (!envelope.success && envelope.error.code === 'UNAUTHENTICATED') clearSensitiveSession()
    return { envelope, requestID: requestID || envelope.meta.request_id }
  }
}
