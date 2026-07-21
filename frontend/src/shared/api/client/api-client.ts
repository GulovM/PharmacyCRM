import createClient from 'openapi-fetch'
import type { paths } from '../generated/schema'
import type { ApiEnvelope } from '../envelope/types'
import { tokenStore } from './token-store'

export function createGeneratedApiClient(baseURL: string) {
  return createClient<paths>({ baseUrl: baseURL })
}

export class ApiClient {
  constructor(private readonly baseURL: string) {}

  async request<T>(path: string, init: RequestInit = {}): Promise<ApiEnvelope<T>> {
    const headers = new Headers(init.headers)
    headers.set('Accept', 'application/json')
    const token = tokenStore.get()
    if (token) headers.set('Authorization', `Bearer ${token}`)
    const response = await fetch(new URL(path, this.baseURL), { ...init, headers, signal: init.signal })
    return response.json() as Promise<ApiEnvelope<T>>
  }
}
