let accessToken: string | undefined

// The access token deliberately lives only in JavaScript memory.
export const tokenStore = {
  get: () => accessToken,
  set: (token: string) => {
    accessToken = token
  },
  clear: () => {
    accessToken = undefined
  },
}

export function clearSensitiveSession(): void {
  tokenStore.clear()
}
