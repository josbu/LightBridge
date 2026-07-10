/**
 * Authconv integration for LightBridge
 * Provides format detection, normalization, rendering, and conversion between
 * authconv formats and LightBridge's AdminDataPayload/AdminDataAccount format.
 *
 * Based on https://github.com/ltxgit/authconv (MIT License)
 */

import type { AdminDataAccount, AdminDataPayload } from '@/types'
import { detectInputFormat, normalizeInput } from './normalize'
import { renderFormat } from './renderers'
import type {
  InputFormat,
  NormalizedAccount,
  OutputFormat,
  RenderOptions
} from './types'

export { detectInputFormat, normalizeInput } from './normalize'
export { renderFormat } from './renderers'
export type { AdminDataPayload } from '@/types'
export type {
  InputFormat,
  NormalizedAccount,
  OutputFormat,
  RenderOptions,
  CpaRenderedAccount,
  Codex2ApiRenderedAccount,
  Sub2ApiRenderedData,
  CodexManagerRenderedAccount,
  CodexRenderedAuth
} from './types'
export { ALL_FORMATS } from './types'

const FORMAT_LABELS: Record<OutputFormat, string> = {
  cpa: 'CPA',
  sub2api: 'sub2api',
  codex2api: 'codex2api',
  codexmanager: 'Codex Manager',
  codex: 'Codex Auth'
}

const INPUT_FORMAT_LABELS: Record<InputFormat, string> = {
  session: 'ChatGPT Session',
  sub2api: 'sub2api',
  cpa: 'CPA',
  codexmanager: 'Codex Manager',
  codex2api: 'Codex2Api',
  codex: 'Codex Auth',
  unknown: 'Unknown'
}

export function getFormatLabel(format: OutputFormat): string {
  return FORMAT_LABELS[format]
}

export function getInputFormatLabel(format: InputFormat): string {
  return INPUT_FORMAT_LABELS[format]
}

/**
 * Detect the input format of a parsed JSON value.
 */
export function detectFormat(input: unknown): InputFormat {
  return detectInputFormat(input)
}

/**
 * Convert any recognized authconv input format into LightBridge's AdminDataPayload.
 * Returns null if the input is not a recognized credential format.
 */
export function convertToPayload(
  input: unknown,
  options?: {
    concurrency?: number
    priority?: number
    rate_multiplier?: number
    auto_pause_on_expired?: boolean
    inputFormat?: InputFormat
  }
): AdminDataPayload | null {
  const result = normalizeInput(input, 'input', 'input', options?.inputFormat)
  if (result.accounts.length === 0) {
    return null
  }

  const accounts: AdminDataAccount[] = result.accounts.map((account) =>
    normalizedToAdminDataAccount(account, options)
  )

  return {
    type: 'LightBridge-data',
    version: 1,
    exported_at: new Date().toISOString(),
    proxies: [],
    accounts
  }
}

/**
 * Convert a LightBridge AdminDataAccount back to a NormalizedAccount.
 */
export function normalizedFromAdminDataAccount(account: AdminDataAccount): NormalizedAccount {
  const creds = account.credentials ?? {}
  const expiresAt = normalizeAdminExpiresAt(account.expires_at ?? creds.expires_at)
  return {
    accessToken: typeof creds.access_token === 'string' ? creds.access_token : undefined,
    refreshToken: typeof creds.refresh_token === 'string' ? creds.refresh_token : undefined,
    idToken: typeof creds.id_token === 'string' ? creds.id_token : undefined,
    sessionToken: typeof creds.session_token === 'string' ? creds.session_token : undefined,
    accountId: typeof creds.account_id === 'string' ? creds.account_id : undefined,
    chatgptAccountId: typeof creds.chatgpt_account_id === 'string' ? creds.chatgpt_account_id : undefined,
    chatgptUserId: typeof creds.chatgpt_user_id === 'string' ? creds.chatgpt_user_id : undefined,
    email: typeof creds.email === 'string' ? creds.email : undefined,
    name: account.name,
    planType: typeof creds.plan_type === 'string' ? creds.plan_type : undefined,
    issuer: account.platform === 'openai' ? 'https://auth.openai.com' : undefined,
    expiresAt,
    sourceName: account.name,
    sourcePath: account.name,
    warnings: []
  }
}

function normalizeAdminExpiresAt(value: unknown): string | undefined {
  if (value === null || value === undefined || value === '') {
    return undefined
  }
  if (typeof value === 'number' && Number.isFinite(value)) {
    return new Date(value > 1_000_000_000_000 ? value : value * 1000).toISOString()
  }
  if (typeof value !== 'string') {
    return undefined
  }
  const trimmed = value.trim()
  if (!trimmed) {
    return undefined
  }
  if (/^\d+$/.test(trimmed)) {
    return normalizeAdminExpiresAt(Number(trimmed))
  }
  const date = new Date(trimmed)
  if (!Number.isNaN(date.getTime())) {
    return date.toISOString()
  }
  return trimmed
}

/**
 * Convert a NormalizedAccount to LightBridge's AdminDataAccount format.
 */
function normalizedToAdminDataAccount(
  account: NormalizedAccount,
  options?: {
    concurrency?: number
    priority?: number
    rate_multiplier?: number
    auto_pause_on_expired?: boolean
  }
): AdminDataAccount {
  const credentials: Record<string, unknown> = {}
  if (account.accessToken) credentials.access_token = account.accessToken
  if (account.refreshToken) credentials.refresh_token = account.refreshToken
  if (account.idToken) credentials.id_token = account.idToken
  if (account.sessionToken) credentials.session_token = account.sessionToken
  if (account.email) credentials.email = account.email
  if (account.accountId) credentials.account_id = account.accountId
  if (account.chatgptAccountId) credentials.chatgpt_account_id = account.chatgptAccountId
  if (account.chatgptUserId) credentials.chatgpt_user_id = account.chatgptUserId
  if (account.planType) credentials.plan_type = account.planType

  const expiresAt = account.expiresAt
    ? Math.floor(new Date(account.expiresAt).getTime() / 1000)
    : null

  return {
    name: account.name ?? account.email ?? account.accountId ?? 'imported-account',
    platform: 'openai',
    type: 'oauth',
    credentials,
    concurrency: options?.concurrency ?? 10,
    priority: options?.priority ?? 1,
    rate_multiplier: options?.rate_multiplier ?? 1,
    expires_at: expiresAt,
    auto_pause_on_expired: options?.auto_pause_on_expired ?? true
  }
}

/**
 * Convert LightBridge AdminDataPayload accounts to a target authconv format.
 * Returns the rendered output as a JSON-serializable object.
 */
export function convertFromPayload(
  payload: AdminDataPayload,
  targetFormat: OutputFormat,
  options?: RenderOptions
): unknown {
  const accounts: NormalizedAccount[] = payload.accounts.map((account) =>
    normalizedFromAdminDataAccount(account)
  )

  if (accounts.length === 0) {
    return null
  }

  return renderFormat(accounts, targetFormat, options)
}
