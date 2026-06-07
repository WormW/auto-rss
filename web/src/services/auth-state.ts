export interface TokenPair {
  access_token: string
  refresh_token: string
  token_type: string
  expires_in: number
}

const accessTokenKey = 'access_token'
const refreshTokenKey = 'refresh_token'

export const getAccessToken = (): string => localStorage.getItem(accessTokenKey) || ''

export const getRefreshToken = (): string => localStorage.getItem(refreshTokenKey) || ''

export const hasAuthTokens = (): boolean => Boolean(getAccessToken() && getRefreshToken())

export const setAuthTokens = (tokens: TokenPair): void => {
  localStorage.setItem(accessTokenKey, tokens.access_token)
  localStorage.setItem(refreshTokenKey, tokens.refresh_token)
  window.dispatchEvent(new CustomEvent('auth:changed'))
}

export const clearAuthTokens = (): void => {
  localStorage.removeItem(accessTokenKey)
  localStorage.removeItem(refreshTokenKey)
  window.dispatchEvent(new CustomEvent('auth:changed'))
}
