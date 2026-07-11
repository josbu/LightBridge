/**
 * Shared helpers for iframe-embedded pages.
 *
 * Authentication tokens must never be placed in the iframe URL because query
 * parameters are copied into browser history, reverse-proxy logs, analytics,
 * referrers, and support screenshots. Non-sensitive display context remains in
 * the URL; authentication is delivered to the exact iframe origin by
 * postMessage after the frame is loaded or explicitly announces readiness.
 */

const EMBEDDED_USER_ID_QUERY_KEY = 'user_id'
const EMBEDDED_THEME_QUERY_KEY = 'theme'
const EMBEDDED_LANG_QUERY_KEY = 'lang'
const EMBEDDED_UI_MODE_QUERY_KEY = 'ui_mode'
const EMBEDDED_UI_MODE_VALUE = 'embedded'
const EMBEDDED_SRC_HOST_QUERY_KEY = 'src_host'
const EMBEDDED_SRC_QUERY_KEY = 'src_url'

export const EMBEDDED_READY_MESSAGE_TYPE = 'lightbridge:embed-ready'
export const EMBEDDED_AUTH_MESSAGE_TYPE = 'lightbridge:embed-auth'
export const EMBEDDED_AUTH_MESSAGE_VERSION = 1 as const
export const EMBEDDED_AUTH_SCOPE = 'payment_embed' as const

export interface EmbeddedAuthMessage {
  type: typeof EMBEDDED_AUTH_MESSAGE_TYPE
  version: typeof EMBEDDED_AUTH_MESSAGE_VERSION
  token: string
  scope: typeof EMBEDDED_AUTH_SCOPE
  expires_at: number
  user_id?: number
  theme: 'light' | 'dark'
  lang?: string
  src_host?: string
}

export function buildEmbeddedUrl(
  baseUrl: string,
  userId?: number,
  theme: 'light' | 'dark' = 'light',
  lang?: string,
): string {
  if (!baseUrl) return baseUrl
  try {
    const url = new URL(baseUrl)
    if (userId) {
      url.searchParams.set(EMBEDDED_USER_ID_QUERY_KEY, String(userId))
    }
    url.searchParams.set(EMBEDDED_THEME_QUERY_KEY, theme)
    if (lang) {
      url.searchParams.set(EMBEDDED_LANG_QUERY_KEY, lang)
    }
    url.searchParams.set(EMBEDDED_UI_MODE_QUERY_KEY, EMBEDDED_UI_MODE_VALUE)

    // Source tracking intentionally excludes the current query string and hash,
    // which may contain OAuth callbacks, invitation codes, or other secrets.
    if (typeof window !== 'undefined') {
      url.searchParams.set(EMBEDDED_SRC_HOST_QUERY_KEY, window.location.origin)
      url.searchParams.set(
        EMBEDDED_SRC_QUERY_KEY,
        `${window.location.origin}${window.location.pathname}`,
      )
    }
    return url.toString()
  } catch {
    return baseUrl
  }
}

export function getEmbeddedTargetOrigin(urlValue: string): string | null {
  if (!urlValue) return null
  try {
    return new URL(urlValue).origin
  } catch {
    return null
  }
}

export function isSecureEmbeddedOrigin(origin: string): boolean {
  try {
    const url = new URL(origin)
    if (url.protocol === 'https:') return true
    if (url.protocol !== 'http:') return false
    return url.hostname === 'localhost' || url.hostname === '127.0.0.1' || url.hostname === '[::1]'
  } catch {
    return false
  }
}

export function buildEmbeddedAuthMessage(
  authToken: string | null | undefined,
  userId?: number,
  theme: 'light' | 'dark' = 'light',
  lang?: string,
  expiresAt?: number,
): EmbeddedAuthMessage | null {
  if (!authToken || !expiresAt || expiresAt <= Date.now()) return null

  const message: EmbeddedAuthMessage = {
    type: EMBEDDED_AUTH_MESSAGE_TYPE,
    version: EMBEDDED_AUTH_MESSAGE_VERSION,
    token: authToken,
    scope: EMBEDDED_AUTH_SCOPE,
    expires_at: expiresAt,
    theme,
  }
  if (userId) message.user_id = userId
  if (lang) message.lang = lang
  if (typeof window !== 'undefined') message.src_host = window.location.origin
  return message
}

export function isEmbeddedReadyMessage(value: unknown): boolean {
  if (!value || typeof value !== 'object') return false
  return (value as { type?: unknown }).type === EMBEDDED_READY_MESSAGE_TYPE
}

export function detectTheme(): 'light' | 'dark' {
  if (typeof document === 'undefined') return 'light'
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}
