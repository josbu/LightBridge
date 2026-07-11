import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  buildEmbeddedAuthMessage,
  buildEmbeddedUrl,
  detectTheme,
  EMBEDDED_AUTH_MESSAGE_TYPE,
  EMBEDDED_AUTH_SCOPE,
  EMBEDDED_READY_MESSAGE_TYPE,
  getEmbeddedTargetOrigin,
  isEmbeddedReadyMessage,
  isSecureEmbeddedOrigin,
} from '../embedded-url'

describe('embedded-url', () => {
  const originalLocation = window.location

  beforeEach(() => {
    Object.defineProperty(window, 'location', {
      value: {
        origin: 'https://app.example.com',
        pathname: '/user/purchase',
        href: 'https://app.example.com/user/purchase?oauth_code=secret#fragment',
      },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
      configurable: true,
    })
    document.documentElement.classList.remove('dark')
    vi.restoreAllMocks()
  })

  it('adds only non-sensitive query parameters and strips source query/hash', () => {
    const result = buildEmbeddedUrl(
      'https://pay.example.com/checkout?plan=pro',
      42,
      'dark',
      'zh-CN',
    )

    const url = new URL(result)
    expect(url.searchParams.get('plan')).toBe('pro')
    expect(url.searchParams.get('user_id')).toBe('42')
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.searchParams.get('theme')).toBe('dark')
    expect(url.searchParams.get('lang')).toBe('zh-CN')
    expect(url.searchParams.get('ui_mode')).toBe('embedded')
    expect(url.searchParams.get('src_host')).toBe('https://app.example.com')
    expect(url.searchParams.get('src_url')).toBe('https://app.example.com/user/purchase')
  })

  it('omits optional params when they are empty', () => {
    const result = buildEmbeddedUrl('https://pay.example.com/checkout', undefined, 'light')

    const url = new URL(result)
    expect(url.searchParams.get('theme')).toBe('light')
    expect(url.searchParams.get('ui_mode')).toBe('embedded')
    expect(url.searchParams.has('user_id')).toBe(false)
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.searchParams.has('lang')).toBe(false)
  })

  it('returns original string for invalid url input', () => {
    expect(buildEmbeddedUrl('not a url', 1, 'dark')).toBe('not a url')
    expect(getEmbeddedTargetOrigin('not a url')).toBeNull()
  })

  it('builds the authentication message separately from the URL', () => {
    const expiresAt = Date.now() + 60_000
    expect(buildEmbeddedAuthMessage('token-123', 42, 'dark', 'zh-CN', expiresAt)).toEqual({
      type: EMBEDDED_AUTH_MESSAGE_TYPE,
      version: 1,
      token: 'token-123',
      scope: EMBEDDED_AUTH_SCOPE,
      expires_at: expiresAt,
      user_id: 42,
      theme: 'dark',
      lang: 'zh-CN',
      src_host: 'https://app.example.com',
    })
    expect(buildEmbeddedAuthMessage('', 42, 'light', undefined, expiresAt)).toBeNull()
    expect(buildEmbeddedAuthMessage('expired', 42, 'light', undefined, Date.now() - 1)).toBeNull()
  })

  it('validates exact target origins and ready messages', () => {
    expect(getEmbeddedTargetOrigin('https://pay.example.com/checkout')).toBe('https://pay.example.com')
    expect(isSecureEmbeddedOrigin('https://pay.example.com')).toBe(true)
    expect(isSecureEmbeddedOrigin('http://localhost:5173')).toBe(true)
    expect(isSecureEmbeddedOrigin('http://pay.example.com')).toBe(false)
    expect(isEmbeddedReadyMessage({ type: EMBEDDED_READY_MESSAGE_TYPE })).toBe(true)
    expect(isEmbeddedReadyMessage({ type: 'other' })).toBe(false)
  })

  it('detects dark mode from document root class', () => {
    document.documentElement.classList.add('dark')
    expect(detectTheme()).toBe('dark')
  })
})
