import { afterEach, describe, expect, it } from 'vitest'
import { clearSensitiveSession, tokenStore } from './token-store'

afterEach(clearSensitiveSession)

describe('tokenStore', () => {
  it('keeps an access token only until the session is cleared', () => {
    tokenStore.set('access-token')
    expect(tokenStore.get()).toBe('access-token')

    clearSensitiveSession()
    expect(tokenStore.get()).toBeUndefined()
  })
})
