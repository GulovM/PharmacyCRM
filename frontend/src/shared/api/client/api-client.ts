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
    let response: Response
    try {
      response = await authenticatedFetch(new URL(path, this.baseURL), init)
    } catch {
      throw new ApiTransportError('API network request failed', 0)
    }
    const headerRequestID = response.headers.get('X-Request-ID') ?? ''
    if (!response.headers.get('content-type')?.toLowerCase().includes('application/json')) {
      throw new ApiTransportError('API response is not JSON', response.status, headerRequestID)
    }
    let body: unknown
    try {
      body = await response.json()
    } catch {
      throw new ApiTransportError('API response JSON is invalid', response.status, headerRequestID)
    }
    const parsed = envelopeSchema.safeParse(body)
    if (!parsed.success) {
      throw new ApiTransportError('API response violates the envelope contract', response.status, headerRequestID)
    }
    const envelope = parsed.data as ApiEnvelope<T>
    const requestID = headerRequestID || envelope.meta.request_id
    if (!envelope.success) {
      if (response.status === 401 || envelope.error.code === 'UNAUTHENTICATED') clearSensitiveSession()
    }
    if (response.ok !== envelope.success) {
      throw new ApiTransportError('API response status contradicts success envelope', response.status, requestID)
    }
    return { envelope, requestID }
  }
}
