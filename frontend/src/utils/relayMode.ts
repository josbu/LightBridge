export const RELAY_MODE_ROUTER = 'router'
export const RELAY_MODE_PASSTHROUGH = 'passthrough'
export const RELAY_MODE_FULL_PASSTHROUGH = 'full_passthrough'

export type RelayMode =
  | typeof RELAY_MODE_ROUTER
  | typeof RELAY_MODE_PASSTHROUGH
  | typeof RELAY_MODE_FULL_PASSTHROUGH

const relayModes = new Set<string>([
  RELAY_MODE_ROUTER,
  RELAY_MODE_PASSTHROUGH,
  RELAY_MODE_FULL_PASSTHROUGH
])

const legacyFullPassthroughKeys = [
  'openai_passthrough',
  'openai_oauth_passthrough',
  'anthropic_passthrough'
]

export function normalizeRelayMode(extra?: Record<string, unknown> | null): RelayMode {
  const raw = typeof extra?.relay_mode === 'string' ? extra.relay_mode.trim() : ''
  if (relayModes.has(raw)) {
    return raw as RelayMode
  }
  if (legacyFullPassthroughKeys.some(key => extra?.[key] === true)) {
    return RELAY_MODE_FULL_PASSTHROUGH
  }
  return RELAY_MODE_ROUTER
}

export function writeRelayModeToExtra(extra: Record<string, unknown>, mode: RelayMode): Record<string, unknown> {
  for (const key of legacyFullPassthroughKeys) {
    delete extra[key]
  }
  if (mode === RELAY_MODE_ROUTER) {
    delete extra.relay_mode
  } else {
    extra.relay_mode = mode
  }
  return extra
}
