<template src="./templates/EditAccountModal.template.html"></template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { adminAPI } from '@/api/admin'
import { useQuotaNotifyState } from '@/composables/useQuotaNotifyState'
import type {
  Account,
  Proxy,
  AdminGroup,
  CheckMixedChannelResponse,
  OpenAICompactMode,
  OpenAIResponsesMode,
  OpenAIEndpointCapability
} from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import ProxyAdBanner from '@/components/common/ProxyAdBanner.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import ProxyPolicyPanel from '@/components/admin/proxy/ProxyPolicyPanel.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import QuotaLimitCard from '@/components/account/QuotaLimitCard.vue'
import { applyInterceptWarmup } from '@/components/account/credentialsBuilder'
import { formatDateTime, formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/utils/format'
import { createStableObjectKeyResolver } from '@/utils/stableObjectKey'
import { VERTEX_LOCATION_OPTIONS } from '@/constants/account'
import {
  OPENAI_WS_MODE_CTX_POOL,
  OPENAI_WS_MODE_HTTP_BRIDGE,
  OPENAI_WS_MODE_OFF,
  OPENAI_WS_MODE_PASSTHROUGH,
  isOpenAIWSModeEnabled,
  resolveOpenAIWSModeConcurrencyHintKey,
  type OpenAIWSMode,
  resolveOpenAIWSModeFromExtra
} from '@/utils/openaiWsMode'
import {
  RELAY_MODE_FULL_PASSTHROUGH,
  RELAY_MODE_PASSTHROUGH,
  RELAY_MODE_ROUTER,
  normalizeRelayMode,
  writeRelayModeToExtra,
  type RelayMode
} from '@/utils/relayMode'
import {
  getPresetMappingsByPlatform,
  commonErrorCodes,
  buildModelMappingObject,
  splitModelMappingObject,
  isValidWildcardPattern
} from '@/composables/useModelWhitelist'

interface Props {
  show: boolean
  account: Account | null
  proxies: Proxy[]
  groups: AdminGroup[]
}

const props = defineProps<Props>()
const emit = defineEmits<{
  close: []
  updated: [account: Account]
}>()

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()


// 是否为 AIStudio 反代（Bearer）账号：Gemini 平台 + APIKey + auth_header==bearer。
const isGeminiProxyAccount = computed(() => {
  const acc = props.account
  if (!acc || acc.platform !== 'gemini' || acc.type !== 'apikey') return false
  const cred = (acc.credentials as Record<string, unknown>) || {}
  return cred.auth_header === 'bearer'
})

const antigravityPresetMappings = computed(() => getPresetMappingsByPlatform('antigravity'))
const bedrockPresets = computed(() => getPresetMappingsByPlatform('bedrock'))

// Model mapping type
interface ModelMapping {
  from: string
  to: string
}

interface TempUnschedRuleForm {
  error_code: number | null
  keywords: string
  duration_minutes: number | null
  description: string
}

// State
const submitting = ref(false)
const editBaseUrl = ref('https://api.anthropic.com')
const editApiKey = ref('')
// Bedrock credentials
const editBedrockAccessKeyId = ref('')
const editBedrockSecretAccessKey = ref('')
const editBedrockSessionToken = ref('')
const editBedrockRegion = ref('')
const editBedrockForceGlobal = ref(false)
const editBedrockApiKeyValue = ref('')
const editVertexProjectId = ref('')
const editVertexClientEmail = ref('')
const editVertexLocation = ref('us-central1')
const isBedrockAPIKeyMode = computed(() =>
  props.account?.type === 'bedrock' &&
  (props.account?.credentials as Record<string, unknown>)?.auth_mode === 'apikey'
)
const modelMappings = ref<ModelMapping[]>([])
const openAICompactModelMappings = ref<ModelMapping[]>([])
const modelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')
const allowedModels = ref<string[]>([])
const restrictToModelList = ref(false)
const DEFAULT_POOL_MODE_RETRY_COUNT = 3
const MAX_POOL_MODE_RETRY_COUNT = 10
const DEFAULT_POOL_MODE_RETRY_STATUS_CODES = [401, 403, 429]
const poolModeEnabled = ref(false)
const poolModeRetryCount = ref(DEFAULT_POOL_MODE_RETRY_COUNT)
const poolModeRetryStatusCodesInput = ref('')

function parsePoolModeRetryStatusCodes(input: string): number[] {
  if (!input || !input.trim()) return []
  const seen = new Set<number>()
  const out: number[] = []
  for (const token of input.split(/[,\s]+/)) {
    const trimmed = token.trim()
    if (!trimmed) continue
    const n = Number(trimmed)
    if (!Number.isFinite(n) || !Number.isInteger(n)) continue
    if (n < 100 || n > 599) continue
    if (seen.has(n)) continue
    seen.add(n)
    out.push(n)
  }
  return out.sort((a, b) => a - b)
}

function formatPoolModeRetryStatusCodes(value: unknown): string {
  if (!Array.isArray(value)) return ''
  const out: number[] = []
  const seen = new Set<number>()
  for (const v of value) {
    const n = typeof v === 'string' ? Number(v.trim()) : Number(v)
    if (!Number.isFinite(n) || !Number.isInteger(n)) continue
    if (n < 100 || n > 599) continue
    if (seen.has(n)) continue
    seen.add(n)
    out.push(n)
  }
  return out.sort((a, b) => a - b).join(', ')
}
const customErrorCodesEnabled = ref(false)
const selectedErrorCodes = ref<number[]>([])
const customErrorCodeInput = ref<number | null>(null)
const interceptWarmupRequests = ref(false)
const autoPauseOnExpired = ref(false)
const autoPause5hThreshold = ref<number | null>(null)
const autoPause7dThreshold = ref<number | null>(null)
const autoPause5hDisabled = ref(false)
const autoPause7dDisabled = ref(false)
const mixedScheduling = ref(false) // For antigravity accounts: enable mixed scheduling
const allowOverages = ref(false) // For antigravity accounts: enable AI Credits overages
const antigravityModelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')
const antigravityWhitelistModels = ref<string[]>([])
const antigravityModelMappings = ref<ModelMapping[]>([])
const isSyncingAntigravityUpstream = ref(false)
const tempUnschedEnabled = ref(false)
const tempUnschedRules = ref<TempUnschedRuleForm[]>([])
const getModelMappingKey = createStableObjectKeyResolver<ModelMapping>('edit-model-mapping')
const getOpenAICompactModelMappingKey = createStableObjectKeyResolver<ModelMapping>('edit-openai-compact-model-mapping')
const getAntigravityModelMappingKey = createStableObjectKeyResolver<ModelMapping>('edit-antigravity-model-mapping')
const getTempUnschedRuleKey = createStableObjectKeyResolver<TempUnschedRuleForm>('edit-temp-unsched-rule')

const showMixedChannelWarning = ref(false)
const mixedChannelWarningDetails = ref<{ groupName: string; currentPlatform: string; otherPlatform: string } | null>(
  null
)
const mixedChannelWarningRawMessage = ref('')
const mixedChannelWarningAction = ref<(() => Promise<void>) | null>(null)
const antigravityMixedChannelConfirmed = ref(false)

// Quota control state (Anthropic OAuth/SetupToken only)
const windowCostEnabled = ref(false)
const windowCostLimit = ref<number | null>(null)
const windowCostStickyReserve = ref<number | null>(null)
const sessionLimitEnabled = ref(false)
const maxSessions = ref<number | null>(null)
const sessionIdleTimeout = ref<number | null>(null)
const rpmLimitEnabled = ref(false)
const baseRpm = ref<number | null>(null)
const rpmStrategy = ref<'tiered' | 'sticky_exempt'>('tiered')
const rpmStickyBuffer = ref<number | null>(null)
const userMsgQueueMode = ref('')
const umqModeOptions = computed(() => [
  { value: '', label: t('admin.accounts.quotaControl.rpmLimit.umqModeOff') },
  { value: 'throttle', label: t('admin.accounts.quotaControl.rpmLimit.umqModeThrottle') },
  { value: 'serialize', label: t('admin.accounts.quotaControl.rpmLimit.umqModeSerialize') },
])
const tlsFingerprintEnabled = ref(false)
const tlsFingerprintProfileId = ref<number | null>(null)
const tlsFingerprintProfiles = ref<{ id: number; name: string }[]>([])
const sessionIdMaskingEnabled = ref(false)
const cacheTTLOverrideEnabled = ref(false)
const cacheTTLOverrideTarget = ref<string>('5m')
const customBaseUrlEnabled = ref(false)
const customBaseUrl = ref('')

const openaiRelayMode = ref<RelayMode>(RELAY_MODE_ROUTER)
const openAICompactMode = ref<OpenAICompactMode>('auto')
const openAIResponsesMode = ref<OpenAIResponsesMode>('auto')
const openAIEndpointCapabilities = ref<OpenAIEndpointCapability[]>(['chat_completions', 'embeddings'])
const openaiOAuthResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
const openaiAPIKeyResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
const codexCLIOnlyEnabled = ref(false)
const codexCLIOnlyAllowClaudeCodeEnabled = ref(false)
type CodexImageGenerationBridgeMode = 'inherit' | 'enabled' | 'disabled'
const codexImageGenerationBridgeMode = ref<CodexImageGenerationBridgeMode>('inherit')
const anthropicRelayMode = ref<RelayMode>(RELAY_MODE_ROUTER)
const geminiRelayMode = ref<RelayMode>(RELAY_MODE_ROUTER)
const customRelayMode = ref<RelayMode>(RELAY_MODE_ROUTER)
const editCustomProtocol = ref('')
const editCustomBaseUrl = ref('')
const editCustomApiKey = ref('')
const openaiPassthroughEnabled = computed({
  get: () => openaiRelayMode.value === RELAY_MODE_FULL_PASSTHROUGH,
  set: (enabled: boolean) => {
    openaiRelayMode.value = enabled ? RELAY_MODE_FULL_PASSTHROUGH : RELAY_MODE_ROUTER
  }
})
const webSearchEmulationMode = ref('default')
const webSearchGlobalEnabled = ref(false)
const {
  globalEnabled: quotaNotifyGlobalEnabled,
  state: quotaNotifyState,
  loadGlobalState: loadQuotaNotifyGlobal,
  loadFromExtra: loadQuotaNotifyFromExtra,
  writeToExtra: writeQuotaNotifyToExtra,
  reset: resetQuotaNotify,
} = useQuotaNotifyState()

// Load global feature states once
adminAPI.settings.getWebSearchEmulationConfig().then(cfg => {
  webSearchGlobalEnabled.value = cfg?.enabled === true && (cfg?.providers?.length ?? 0) > 0
}).catch(() => { webSearchGlobalEnabled.value = false })

loadQuotaNotifyGlobal()
const editQuotaLimit = ref<number | null>(null)
const editQuotaDailyLimit = ref<number | null>(null)
const editQuotaWeeklyLimit = ref<number | null>(null)
const editDailyResetMode = ref<'rolling' | 'fixed' | null>(null)
const editDailyResetHour = ref<number | null>(null)
const editWeeklyResetMode = ref<'rolling' | 'fixed' | null>(null)
const editWeeklyResetDay = ref<number | null>(null)
const editWeeklyResetHour = ref<number | null>(null)
const editResetTimezone = ref<string | null>(null)
const openAIWSModeOptions = computed(() => [
  { value: OPENAI_WS_MODE_OFF, label: t('admin.accounts.openai.wsModeOff') },
  { value: OPENAI_WS_MODE_CTX_POOL, label: t('admin.accounts.openai.wsModeCtxPool') },
  { value: OPENAI_WS_MODE_PASSTHROUGH, label: t('admin.accounts.openai.wsModePassthrough') },
  { value: OPENAI_WS_MODE_HTTP_BRIDGE, label: t('admin.accounts.openai.wsModeHttpBridge') }
])
const relayModeOptions = computed(() => [
  { value: RELAY_MODE_ROUTER, label: t('admin.accounts.relayMode.router') },
  { value: RELAY_MODE_PASSTHROUGH, label: t('admin.accounts.relayMode.passthrough') },
  { value: RELAY_MODE_FULL_PASSTHROUGH, label: t('admin.accounts.relayMode.fullPassthrough') }
])
const relayModeHintKey = (mode: RelayMode) => {
  if (mode === RELAY_MODE_PASSTHROUGH) return 'admin.accounts.relayMode.passthroughDesc'
  if (mode === RELAY_MODE_FULL_PASSTHROUGH) return 'admin.accounts.relayMode.fullPassthroughDesc'
  return 'admin.accounts.relayMode.routerDesc'
}
const customProtocolOptions = computed(() => [
  { value: 'openai_responses', label: t('admin.accounts.custom.protocolOptions.openai_responses') },
  { value: 'openai_chat_completions', label: t('admin.accounts.custom.protocolOptions.openai_chat_completions') },
  { value: 'openai_embeddings', label: t('admin.accounts.custom.protocolOptions.openai_embeddings') },
  { value: 'anthropic_messages', label: t('admin.accounts.custom.protocolOptions.anthropic_messages') },
  { value: 'gemini', label: t('admin.accounts.custom.protocolOptions.gemini') }
])
const openaiResponsesWebSocketV2Mode = computed({
  get: () => {
    if (props.account?.type === 'apikey') {
      return openaiAPIKeyResponsesWebSocketV2Mode.value
    }
    return openaiOAuthResponsesWebSocketV2Mode.value
  },
  set: (mode: OpenAIWSMode) => {
    if (props.account?.type === 'apikey') {
      openaiAPIKeyResponsesWebSocketV2Mode.value = mode
      return
    }
    openaiOAuthResponsesWebSocketV2Mode.value = mode
  }
})
const openAIWSModeConcurrencyHintKey = computed(() =>
  resolveOpenAIWSModeConcurrencyHintKey(openaiResponsesWebSocketV2Mode.value)
)
const codexImageGenerationBridgeOptions = computed<Array<{
  value: CodexImageGenerationBridgeMode
  label: string
  description: string
}>>(() => [
  {
    value: 'inherit',
    label: t('admin.accounts.openai.codexImageGenerationBridgeInherit'),
    description: t('admin.accounts.openai.codexImageGenerationBridgeInheritDesc')
  },
  {
    value: 'enabled',
    label: t('admin.accounts.openai.codexImageGenerationBridgeEnabled'),
    description: t('admin.accounts.openai.codexImageGenerationBridgeEnabledDesc')
  },
  {
    value: 'disabled',
    label: t('admin.accounts.openai.codexImageGenerationBridgeDisabled'),
    description: t('admin.accounts.openai.codexImageGenerationBridgeDisabledDesc')
  }
])
const codexImageGenerationBridgeBadgeLabel = computed(() => {
  switch (codexImageGenerationBridgeMode.value) {
    case 'enabled':
      return t('admin.accounts.openai.codexImageGenerationBridgeBadgeEnabled')
    case 'disabled':
      return t('admin.accounts.openai.codexImageGenerationBridgeBadgeDisabled')
    default:
      return t('admin.accounts.openai.codexImageGenerationBridgeBadgeInherit')
  }
})
const codexImageGenerationBridgeBadgeClass = computed(() => {
  switch (codexImageGenerationBridgeMode.value) {
    case 'enabled':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'disabled':
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
    default:
      return 'bg-slate-100 text-slate-600 dark:bg-dark-600 dark:text-slate-300'
  }
})
const openAICompactModeOptions = computed(() => [
  { value: 'auto', label: t('admin.accounts.openai.compactModeAuto') },
  { value: 'force_on', label: t('admin.accounts.openai.compactModeForceOn') },
  { value: 'force_off', label: t('admin.accounts.openai.compactModeForceOff') }
])
const openAIResponsesModeOptions = computed(() => [
  { value: 'auto', label: t('admin.accounts.openai.responsesModeAuto') },
  { value: 'force_responses', label: t('admin.accounts.openai.responsesModeForceResponses') },
  { value: 'force_chat_completions', label: t('admin.accounts.openai.responsesModeForceChatCompletions') }
])
const openAITextEndpointCapabilityLabel = computed(() => {
  if (openAIResponsesMode.value === 'force_responses') {
    return t('admin.accounts.openai.capabilityResponses')
  }
  if (openAIResponsesMode.value === 'force_chat_completions') {
    return t('admin.accounts.openai.capabilityChatCompletions')
  }
  const extra = props.account?.extra as Record<string, unknown> | undefined
  if (extra?.openai_responses_supported === true) {
    return t('admin.accounts.openai.capabilityResponsesAuto')
  }
  if (extra?.openai_responses_supported === false) {
    return t('admin.accounts.openai.capabilityChatCompletionsAuto')
  }
  return t('admin.accounts.openai.capabilityTextAuto')
})
const openAIEndpointCapabilityOptions = computed<{ value: OpenAIEndpointCapability; label: string }[]>(() => [
  { value: 'chat_completions', label: openAITextEndpointCapabilityLabel.value },
  { value: 'embeddings', label: t('admin.accounts.openai.capabilityEmbeddings') }
])
const openAITextGenerationCapabilityEnabled = computed(() =>
  openAIEndpointCapabilities.value.includes('chat_completions')
)

const normalizeOpenAIEndpointCapabilities = (values: OpenAIEndpointCapability[]) => {
  const allowed: OpenAIEndpointCapability[] = ['chat_completions', 'embeddings']
  const selected = allowed.filter((value) => values.includes(value))
  return selected.length > 0 ? selected : allowed
}

const readOpenAIEndpointCapabilities = (credentials?: Record<string, unknown>): OpenAIEndpointCapability[] => {
  const raw = credentials?.openai_capabilities
  if (Array.isArray(raw)) {
    return normalizeOpenAIEndpointCapabilities(
      raw.filter((value): value is OpenAIEndpointCapability =>
        value === 'chat_completions' || value === 'embeddings'
      )
    )
  }
  if (raw !== null && typeof raw === 'object') {
    const capabilityMap = raw as Record<string, unknown>
    return normalizeOpenAIEndpointCapabilities(
      openAIEndpointCapabilityOptions.value
        .map((option) => option.value)
        .filter((value) => capabilityMap[value] === true)
    )
  }
  return ['chat_completions', 'embeddings']
}

const toggleOpenAIEndpointCapability = (capability: OpenAIEndpointCapability, event?: Event) => {
  if (openAIEndpointCapabilities.value.includes(capability)) {
    if (openAIEndpointCapabilities.value.length <= 1) {
      const input = event?.target as HTMLInputElement | null
      if (input) input.checked = true
      return
    }
    openAIEndpointCapabilities.value = openAIEndpointCapabilities.value.filter(
      (value) => value !== capability
    )
    if (!openAITextGenerationCapabilityEnabled.value) {
      openAIResponsesMode.value = 'auto'
    }
    return
  }
  openAIEndpointCapabilities.value = normalizeOpenAIEndpointCapabilities([
    ...openAIEndpointCapabilities.value,
    capability
  ])
}

const applyOpenAIEndpointCapabilities = (credentials: Record<string, unknown>) => {
  const capabilities = normalizeOpenAIEndpointCapabilities(openAIEndpointCapabilities.value)
  if (capabilities.length === 2) {
    delete credentials.openai_capabilities
    return
  }
  credentials.openai_capabilities = capabilities
}
const normalizeOpenAIResponsesMode = (mode: unknown): OpenAIResponsesMode => {
  if (mode === 'force_responses' || mode === 'force_chat_completions') {
    return mode
  }
  return 'auto'
}
const isOpenAIModelRestrictionDisabled = computed(() =>
  props.account?.platform === 'openai' && openaiPassthroughEnabled.value
)
const openAIResponsesStatusKey = computed(() => {
  if (openAIResponsesMode.value === 'force_responses') {
    return 'admin.accounts.openai.responsesStatusForcedResponses'
  }
  if (openAIResponsesMode.value === 'force_chat_completions') {
    return 'admin.accounts.openai.responsesStatusForcedChatCompletions'
  }
  const extra = props.account?.extra as Record<string, unknown> | undefined
  if (extra?.openai_responses_supported === true) {
    return 'admin.accounts.openai.responsesStatusAutoSupported'
  }
  if (extra?.openai_responses_supported === false) {
    return 'admin.accounts.openai.responsesStatusAutoUnsupported'
  }
  return 'admin.accounts.openai.responsesStatusAutoUnknown'
})
const openAICompactStatusKey = computed(() => {
  const extra = props.account?.extra as Record<string, unknown> | undefined
  if (!props.account || props.account.platform !== 'openai') return ''
  const mode = typeof extra?.openai_compact_mode === 'string' ? extra.openai_compact_mode : 'auto'
  if (mode === 'force_on') return 'admin.accounts.openai.compactSupported'
  if (mode === 'force_off') return 'admin.accounts.openai.compactUnsupported'
  if (typeof extra?.openai_compact_supported === 'boolean') {
    return extra.openai_compact_supported
      ? 'admin.accounts.openai.compactSupported'
      : 'admin.accounts.openai.compactUnsupported'
  }
  return 'admin.accounts.openai.compactAuto'
})

// Computed: current preset mappings based on platform
const presetMappings = computed(() => getPresetMappingsByPlatform(props.account?.platform || 'anthropic'))
const tempUnschedPresets = computed(() => [
  {
    label: t('admin.accounts.tempUnschedulable.presets.overloadLabel'),
    rule: {
      error_code: 529,
      keywords: 'overloaded, too many',
      duration_minutes: 60,
      description: t('admin.accounts.tempUnschedulable.presets.overloadDesc')
    }
  },
  {
    label: t('admin.accounts.tempUnschedulable.presets.rateLimitLabel'),
    rule: {
      error_code: 429,
      keywords: 'rate limit, too many requests',
      duration_minutes: 10,
      description: t('admin.accounts.tempUnschedulable.presets.rateLimitDesc')
    }
  },
  {
    label: t('admin.accounts.tempUnschedulable.presets.unavailableLabel'),
    rule: {
      error_code: 503,
      keywords: 'unavailable, maintenance',
      duration_minutes: 30,
      description: t('admin.accounts.tempUnschedulable.presets.unavailableDesc')
    }
  }
])

// Computed: default base URL based on platform
const defaultBaseUrl = computed(() => {
  if (props.account?.platform === 'openai') return 'https://api.openai.com'
  if (props.account?.platform === 'gemini') return 'https://generativelanguage.googleapis.com'
  return 'https://api.anthropic.com'
})

const mixedChannelWarningMessageText = computed(() => {
  if (mixedChannelWarningDetails.value) {
    return t('admin.accounts.mixedChannelWarning', mixedChannelWarningDetails.value)
  }
  return mixedChannelWarningRawMessage.value
})

const form = reactive({
  name: '',
  notes: '',
  proxy_id: null as number | null,
  concurrency: 1,
  load_factor: null as number | null,
  priority: 1,
  rate_multiplier: 1,
  status: 'active' as 'active' | 'inactive' | 'error',
  group_ids: [] as number[],
  expires_at: null as number | null
})

const statusOptions = computed(() => {
  const options = [
    { value: 'active', label: t('common.active') },
    { value: 'inactive', label: t('common.inactive') }
  ]
  if (form.status === 'error') {
    options.push({ value: 'error', label: t('admin.accounts.status.error') })
  }
  return options
})

const expiresAtInput = computed({
  get: () => formatDateTimeLocal(form.expires_at),
  set: (value: string) => {
    form.expires_at = parseDateTimeLocal(value)
  }
})

// Watchers
const normalizePoolModeRetryCount = (value: number) => {
  if (!Number.isFinite(value)) {
    return DEFAULT_POOL_MODE_RETRY_COUNT
  }
  const normalized = Math.trunc(value)
  if (normalized < 0) {
    return 0
  }
  if (normalized > MAX_POOL_MODE_RETRY_COUNT) {
    return MAX_POOL_MODE_RETRY_COUNT
  }
  return normalized
}

const stringArrayFromUnknown = (raw: unknown): string[] => {
  if (!Array.isArray(raw)) return []
  return raw
    .map((value) => String(value || '').trim())
    .filter(Boolean)
}

const loadModelRestrictionFromMapping = (
  rawMapping?: Record<string, unknown>,
  extra?: Record<string, unknown>
) => {
  const parsed = splitModelMappingObject(rawMapping)
  const listedModels = stringArrayFromUnknown(extra?.supported_models)
  allowedModels.value = listedModels.length > 0 ? listedModels : parsed.allowedModels
  modelMappings.value = parsed.modelMappings
  restrictToModelList.value = extra?.restrict_to_model_list === true
  modelRestrictionMode.value =
    parsed.modelMappings.length > 0 && parsed.allowedModels.length === 0
      ? 'mapping'
      : 'whitelist'
}

const buildModelRestrictionMapping = () =>
  buildModelMappingObject('mapping', [], modelMappings.value)

const normalizedModelList = () => {
  const seen = new Set<string>()
  const models: string[] = []
  for (const raw of allowedModels.value) {
    const model = raw.trim()
    const key = model.toLowerCase()
    if (!model || seen.has(key)) continue
    seen.add(key)
    models.push(model)
  }
  return models
}

const applyModelListExtra = (payload: Record<string, unknown>) => {
  const currentExtra = (payload.extra as Record<string, unknown>) ||
    (props.account?.extra as Record<string, unknown>) ||
    {}
  payload.extra = {
    ...currentExtra,
    supported_models: normalizedModelList(),
    restrict_to_model_list: restrictToModelList.value
  }
}

const syncFormFromAccount = (newAccount: Account | null) => {
  if (!newAccount) {
    return
  }
  antigravityMixedChannelConfirmed.value = false
  showMixedChannelWarning.value = false
  mixedChannelWarningDetails.value = null
  mixedChannelWarningRawMessage.value = ''
  mixedChannelWarningAction.value = null
  form.name = newAccount.name
  form.notes = newAccount.notes || ''
  form.proxy_id = newAccount.proxy_id
  form.concurrency = newAccount.concurrency
  form.load_factor = newAccount.load_factor ?? null
  form.priority = newAccount.priority
  form.rate_multiplier = newAccount.rate_multiplier ?? 1
  form.status = (newAccount.status === 'active' || newAccount.status === 'inactive' || newAccount.status === 'error')
    ? newAccount.status
    : 'active'
  form.group_ids = newAccount.group_ids || []
  form.expires_at = newAccount.expires_at ?? null

  // Load intercept warmup requests setting (applies to all account types)
  const credentials = newAccount.credentials as Record<string, unknown> | undefined
  interceptWarmupRequests.value = credentials?.intercept_warmup_requests === true
  autoPauseOnExpired.value = newAccount.auto_pause_on_expired === true
  editVertexProjectId.value = ''
  editVertexClientEmail.value = ''
  editVertexLocation.value = 'us-central1'

  // Load mixed scheduling setting (only for antigravity accounts)
  mixedScheduling.value = false
  allowOverages.value = false
	const extra = newAccount.extra as Record<string, unknown> | undefined
	mixedScheduling.value = extra?.mixed_scheduling === true
	allowOverages.value = extra?.allow_overages === true
	autoPause5hThreshold.value = typeof extra?.auto_pause_5h_threshold === 'number' ? extra.auto_pause_5h_threshold * 100 : null
	autoPause7dThreshold.value = typeof extra?.auto_pause_7d_threshold === 'number' ? extra.auto_pause_7d_threshold * 100 : null
	autoPause5hDisabled.value = extra?.auto_pause_5h_disabled === true
	autoPause7dDisabled.value = extra?.auto_pause_7d_disabled === true

  openaiRelayMode.value = RELAY_MODE_ROUTER
  openAICompactMode.value = 'auto'
  openAIResponsesMode.value = 'auto'
  openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
  openAICompactModelMappings.value = []
  openaiOAuthResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
  openaiAPIKeyResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
  codexCLIOnlyEnabled.value = false
  codexCLIOnlyAllowClaudeCodeEnabled.value = false
  codexImageGenerationBridgeMode.value = 'inherit'
  anthropicRelayMode.value = RELAY_MODE_ROUTER
  geminiRelayMode.value = RELAY_MODE_ROUTER
  webSearchEmulationMode.value = 'default'
  customRelayMode.value = RELAY_MODE_ROUTER
  editCustomProtocol.value = ''
  editCustomBaseUrl.value = ''
  editCustomApiKey.value = ''
  if (newAccount.platform === 'custom') {
    customRelayMode.value = normalizeRelayMode(extra)
    const customCredentials = newAccount.credentials as Record<string, unknown> | undefined
    editCustomProtocol.value =
      newAccount.protocol ||
      (extra?.protocol as string) ||
      (customCredentials?.protocol as string) ||
      ''
    editCustomBaseUrl.value = (customCredentials?.base_url as string) || ''
  }
  if (newAccount.platform === 'openai' && (newAccount.type === 'oauth' || newAccount.type === 'apikey')) {
    openaiRelayMode.value = normalizeRelayMode(extra)
    openAICompactMode.value = (extra?.openai_compact_mode as OpenAICompactMode) || 'auto'
    if (newAccount.type === 'apikey') {
      openAIResponsesMode.value = normalizeOpenAIResponsesMode(extra?.openai_responses_mode)
      openAIEndpointCapabilities.value = readOpenAIEndpointCapabilities(
        newAccount.credentials as Record<string, unknown> | undefined
      )
      if (!openAITextGenerationCapabilityEnabled.value) {
        openAIResponsesMode.value = 'auto'
      }
    }
    const codexImageGenerationBridgeValue = typeof extra?.codex_image_generation_bridge === 'boolean'
      ? extra.codex_image_generation_bridge
      : extra?.codex_image_generation_bridge_enabled
    if (codexImageGenerationBridgeValue === true) {
      codexImageGenerationBridgeMode.value = 'enabled'
    } else if (codexImageGenerationBridgeValue === false) {
      codexImageGenerationBridgeMode.value = 'disabled'
    }
    openaiOAuthResponsesWebSocketV2Mode.value = resolveOpenAIWSModeFromExtra(extra, {
      modeKey: 'openai_oauth_responses_websockets_v2_mode',
      enabledKey: 'openai_oauth_responses_websockets_v2_enabled',
      fallbackEnabledKeys: ['responses_websockets_v2_enabled', 'openai_ws_enabled'],
      defaultMode: OPENAI_WS_MODE_OFF
    })
    openaiAPIKeyResponsesWebSocketV2Mode.value = resolveOpenAIWSModeFromExtra(extra, {
      modeKey: 'openai_apikey_responses_websockets_v2_mode',
      enabledKey: 'openai_apikey_responses_websockets_v2_enabled',
      fallbackEnabledKeys: ['responses_websockets_v2_enabled', 'openai_ws_enabled'],
      defaultMode: OPENAI_WS_MODE_OFF
    })
    if (newAccount.type === 'oauth') {
      codexCLIOnlyEnabled.value = extra?.codex_cli_only === true
      codexCLIOnlyAllowClaudeCodeEnabled.value =
        Array.isArray(extra?.codex_cli_only_allowed_clients) &&
        (extra.codex_cli_only_allowed_clients as unknown[]).includes('claude_code')
    }
    const credentials = newAccount.credentials as Record<string, unknown> | undefined
    const compactMappings = credentials?.compact_model_mapping as Record<string, string> | undefined
    if (compactMappings && typeof compactMappings === 'object') {
      openAICompactModelMappings.value = Object.entries(compactMappings).map(([from, to]) => ({ from, to }))
    }
  }
  if (newAccount.platform === 'anthropic' && newAccount.type === 'apikey') {
    anthropicRelayMode.value = normalizeRelayMode(extra)
    // 三态：string "default"/"enabled"/"disabled"，向后兼容旧 bool
    const wsVal = extra?.web_search_emulation
    if (wsVal === 'enabled' || wsVal === 'disabled') {
      webSearchEmulationMode.value = wsVal
    } else if (wsVal === true) {
      webSearchEmulationMode.value = 'enabled'
    } else {
      webSearchEmulationMode.value = 'default'
    }
  }
  if (newAccount.platform === 'gemini' && newAccount.type !== 'service_account') {
    geminiRelayMode.value = normalizeRelayMode(extra)
  }

  // Load quota limit for apikey/bedrock accounts (bedrock quota is also loaded in its own branch above)
  if (newAccount.type === 'apikey' || newAccount.type === 'bedrock') {
    const quotaVal = extra?.quota_limit as number | undefined
    editQuotaLimit.value = (quotaVal && quotaVal > 0) ? quotaVal : null
    const dailyVal = extra?.quota_daily_limit as number | undefined
    editQuotaDailyLimit.value = (dailyVal && dailyVal > 0) ? dailyVal : null
    const weeklyVal = extra?.quota_weekly_limit as number | undefined
    editQuotaWeeklyLimit.value = (weeklyVal && weeklyVal > 0) ? weeklyVal : null
    // Load quota reset mode config
    editDailyResetMode.value = (extra?.quota_daily_reset_mode as 'rolling' | 'fixed') || null
    editDailyResetHour.value = (extra?.quota_daily_reset_hour as number) ?? null
    editWeeklyResetMode.value = (extra?.quota_weekly_reset_mode as 'rolling' | 'fixed') || null
    editWeeklyResetDay.value = (extra?.quota_weekly_reset_day as number) ?? null
    editWeeklyResetHour.value = (extra?.quota_weekly_reset_hour as number) ?? null
    editResetTimezone.value = (extra?.quota_reset_timezone as string) || null
    // Load quota notify config
    loadQuotaNotifyFromExtra(extra)
  } else {
    editQuotaLimit.value = null
    editQuotaDailyLimit.value = null
    editQuotaWeeklyLimit.value = null
    editDailyResetMode.value = null
    editDailyResetHour.value = null
    editWeeklyResetMode.value = null
    editWeeklyResetDay.value = null
    editWeeklyResetHour.value = null
    editResetTimezone.value = null
    resetQuotaNotify()
  }

  // Load antigravity model mapping (Antigravity 只支持映射模式)
  if (newAccount.platform === 'antigravity') {
    const credentials = newAccount.credentials as Record<string, unknown> | undefined

    // Antigravity 始终使用映射模式
    antigravityModelRestrictionMode.value = 'mapping'
    antigravityWhitelistModels.value = []

    // 从 model_mapping 读取映射配置
    const rawAgMapping = credentials?.model_mapping as Record<string, string> | undefined
    if (rawAgMapping && typeof rawAgMapping === 'object') {
      const entries = Object.entries(rawAgMapping)
      // 无论是白名单样式(key===value)还是真正的映射，都统一转换为映射列表
      antigravityModelMappings.value = entries.map(([from, to]) => ({ from, to }))
    } else {
      // 兼容旧数据：从 model_whitelist 读取，转换为映射格式
      const rawWhitelist = credentials?.model_whitelist
      if (Array.isArray(rawWhitelist) && rawWhitelist.length > 0) {
        antigravityModelMappings.value = rawWhitelist
          .map((v) => String(v).trim())
          .filter((v) => v.length > 0)
          .map((m) => ({ from: m, to: m }))
      } else {
        antigravityModelMappings.value = []
      }
    }
  } else {
    antigravityModelRestrictionMode.value = 'mapping'
    antigravityWhitelistModels.value = []
    antigravityModelMappings.value = []
  }

  // Load quota control settings (Anthropic OAuth/SetupToken only)
  loadQuotaControlSettings(newAccount)

  loadTempUnschedRules(credentials)

  // Initialize API Key fields for apikey type
  if (newAccount.type === 'apikey' && newAccount.credentials) {
    const credentials = newAccount.credentials as Record<string, unknown>
    const platformDefaultUrl =
      newAccount.platform === 'openai'
        ? 'https://api.openai.com'
        : newAccount.platform === 'gemini'
          ? 'https://generativelanguage.googleapis.com'
          : newAccount.platform === 'grok'
            ? 'https://api.x.ai'
            : 'https://api.anthropic.com'
    editBaseUrl.value = (credentials.base_url as string) || platformDefaultUrl

    // Load model mappings and detect mode
    loadModelRestrictionFromMapping(
      credentials.model_mapping as Record<string, unknown> | undefined,
      newAccount.extra as Record<string, unknown> | undefined
    )

    // Load pool mode
    poolModeEnabled.value = credentials.pool_mode === true
    poolModeRetryCount.value = normalizePoolModeRetryCount(
      Number(credentials.pool_mode_retry_count ?? DEFAULT_POOL_MODE_RETRY_COUNT)
    )
    poolModeRetryStatusCodesInput.value = formatPoolModeRetryStatusCodes(credentials.pool_mode_retry_status_codes)

    // Load custom error codes
    customErrorCodesEnabled.value = credentials.custom_error_codes_enabled === true
    const existingErrorCodes = credentials.custom_error_codes as number[] | undefined
    if (existingErrorCodes && Array.isArray(existingErrorCodes)) {
      selectedErrorCodes.value = [...existingErrorCodes]
    } else {
      selectedErrorCodes.value = []
    }
  } else if (newAccount.type === 'bedrock' && newAccount.credentials) {
    const bedrockCreds = newAccount.credentials as Record<string, unknown>
    const authMode = (bedrockCreds.auth_mode as string) || 'sigv4'
    editBedrockRegion.value = (bedrockCreds.aws_region as string) || ''
    editBedrockForceGlobal.value = (bedrockCreds.aws_force_global as string) === 'true'

    if (authMode === 'apikey') {
      editBedrockApiKeyValue.value = ''
    } else {
      editBedrockAccessKeyId.value = (bedrockCreds.aws_access_key_id as string) || ''
      editBedrockSecretAccessKey.value = ''
      editBedrockSessionToken.value = ''
    }

    // Load pool mode for bedrock
    poolModeEnabled.value = bedrockCreds.pool_mode === true
    const retryCount = bedrockCreds.pool_mode_retry_count
    poolModeRetryCount.value = (typeof retryCount === 'number' && retryCount >= 0) ? retryCount : DEFAULT_POOL_MODE_RETRY_COUNT
    poolModeRetryStatusCodesInput.value = formatPoolModeRetryStatusCodes(bedrockCreds.pool_mode_retry_status_codes)

    // Load quota limits for bedrock
    const bedrockExtra = (newAccount.extra as Record<string, unknown>) || {}
    editQuotaLimit.value = typeof bedrockExtra.quota_limit === 'number' ? bedrockExtra.quota_limit : null
    editQuotaDailyLimit.value = typeof bedrockExtra.quota_daily_limit === 'number' ? bedrockExtra.quota_daily_limit : null
    editQuotaWeeklyLimit.value = typeof bedrockExtra.quota_weekly_limit === 'number' ? bedrockExtra.quota_weekly_limit : null
    // Load quota notify for bedrock
    loadQuotaNotifyFromExtra(bedrockExtra)

    // Load model mappings for bedrock
    loadModelRestrictionFromMapping(
      bedrockCreds.model_mapping as Record<string, unknown> | undefined,
      newAccount.extra as Record<string, unknown> | undefined
    )
  } else if (newAccount.type === 'upstream' && newAccount.credentials) {
    const credentials = newAccount.credentials as Record<string, unknown>
    editBaseUrl.value = (credentials.base_url as string) || ''
  } else if ((newAccount.platform === 'gemini' || newAccount.platform === 'anthropic') && newAccount.type === 'service_account' && newAccount.credentials) {
    const credentials = newAccount.credentials as Record<string, unknown>
    editVertexProjectId.value = (credentials.project_id as string) || ''
    editVertexClientEmail.value = (credentials.client_email as string) || ''
    editVertexLocation.value = (credentials.location as string) || (credentials.vertex_location as string) || 'us-central1'

    // Load model mappings for service_account
    loadModelRestrictionFromMapping(
      credentials.model_mapping as Record<string, unknown> | undefined,
      newAccount.extra as Record<string, unknown> | undefined
    )
  } else {
    const platformDefaultUrl =
      newAccount.platform === 'openai'
        ? 'https://api.openai.com'
        : newAccount.platform === 'gemini'
          ? 'https://generativelanguage.googleapis.com'
          : 'https://api.anthropic.com'
    editBaseUrl.value = platformDefaultUrl

    // Load model mappings for OpenAI OAuth accounts
    if (newAccount.platform === 'openai' && newAccount.credentials) {
      const oauthCredentials = newAccount.credentials as Record<string, unknown>
      loadModelRestrictionFromMapping(
        oauthCredentials.model_mapping as Record<string, unknown> | undefined,
        newAccount.extra as Record<string, unknown> | undefined
      )
    } else if (newAccount.platform === 'grok' && newAccount.credentials) {
      // Load model mappings for Grok OAuth accounts
      const grokCredentials = newAccount.credentials as Record<string, unknown>
      loadModelRestrictionFromMapping(
        grokCredentials.model_mapping as Record<string, unknown> | undefined,
        newAccount.extra as Record<string, unknown> | undefined
      )
    } else {
      modelRestrictionMode.value = 'whitelist'
      modelMappings.value = []
      allowedModels.value = []
      restrictToModelList.value = false
    }
    poolModeEnabled.value = false
    poolModeRetryCount.value = DEFAULT_POOL_MODE_RETRY_COUNT
    poolModeRetryStatusCodesInput.value = ''
    customErrorCodesEnabled.value = false
    selectedErrorCodes.value = []
  }
  editApiKey.value = ''
}

async function loadTLSProfiles() {
  try {
    const profiles = await adminAPI.tlsFingerprintProfiles.list()
    tlsFingerprintProfiles.value = profiles.map(p => ({ id: p.id, name: p.name }))
  } catch {
    tlsFingerprintProfiles.value = []
  }
}

watch(
  [() => props.show, () => props.account],
  ([show, newAccount], [wasShow, previousAccount]) => {
    if (!show || !newAccount) {
      return
    }
    if (!wasShow || newAccount !== previousAccount) {
      syncFormFromAccount(newAccount)
      loadTLSProfiles()
    }
  },
  { immediate: true }
)

// Model mapping helpers
const addModelMapping = () => {
  modelMappings.value.push({ from: '', to: '' })
}

const removeModelMapping = (index: number) => {
  modelMappings.value.splice(index, 1)
}

const addPresetMapping = (from: string, to: string) => {
  const exists = modelMappings.value.some((m) => m.from === from)
  if (exists) {
    appStore.showInfo(t('admin.accounts.mappingExists', { model: from }))
    return
  }
  modelMappings.value.push({ from, to })
}

const addAntigravityModelMapping = () => {
  antigravityModelMappings.value.push({ from: '', to: '' })
}

const addOpenAICompactModelMapping = () => {
  openAICompactModelMappings.value.push({ from: '', to: '' })
}

const removeOpenAICompactModelMapping = (index: number) => {
  openAICompactModelMappings.value.splice(index, 1)
}

const removeAntigravityModelMapping = (index: number) => {
  antigravityModelMappings.value.splice(index, 1)
}

const addAntigravityPresetMapping = (from: string, to: string) => {
  const exists = antigravityModelMappings.value.some((m) => m.from === from)
  if (exists) {
    appStore.showInfo(t('admin.accounts.mappingExists', { model: from }))
    return
  }
  antigravityModelMappings.value.push({ from, to })
}

const syncAntigravityUpstreamModels = async () => {
  if (!props.account?.id || isSyncingAntigravityUpstream.value) return

  isSyncingAntigravityUpstream.value = true
  try {
    const result = await adminAPI.accounts.syncUpstreamModels(props.account.id)
    const upstreamModels = result.models.map((model) => model.trim()).filter(Boolean)
    if (upstreamModels.length === 0) {
      appStore.showInfo(t('admin.accounts.syncUpstreamModelsEmpty'))
      return
    }

    let addedCount = 0
    for (const model of upstreamModels) {
      const exists = antigravityModelMappings.value.some((mapping) => mapping.from === model)
      if (!exists) {
        antigravityModelMappings.value.push({ from: model, to: model })
        addedCount += 1
      }
    }

    if (addedCount > 0) {
      appStore.showSuccess(t('admin.accounts.syncUpstreamModelsSuccess', { count: addedCount, total: upstreamModels.length }))
    } else {
      appStore.showInfo(t('admin.accounts.syncUpstreamModelsNoChanges', { count: upstreamModels.length }))
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : t('admin.accounts.syncUpstreamModelsFailed')
    appStore.showError(t('admin.accounts.syncUpstreamModelsError', { message }))
  } finally {
    isSyncingAntigravityUpstream.value = false
  }
}

// Error code toggle helper
const toggleErrorCode = (code: number) => {
  const index = selectedErrorCodes.value.indexOf(code)
  if (index === -1) {
    // Adding code - check for 429/529 warning
    if (code === 429) {
      if (!confirm(t('admin.accounts.customErrorCodes429Warning'))) {
        return
      }
    } else if (code === 529) {
      if (!confirm(t('admin.accounts.customErrorCodes529Warning'))) {
        return
      }
    }
    selectedErrorCodes.value.push(code)
  } else {
    selectedErrorCodes.value.splice(index, 1)
  }
}

// Add custom error code from input
const addCustomErrorCode = () => {
  const code = customErrorCodeInput.value
  if (code === null || code < 100 || code > 599) {
    appStore.showError(t('admin.accounts.invalidErrorCode'))
    return
  }
  if (selectedErrorCodes.value.includes(code)) {
    appStore.showInfo(t('admin.accounts.errorCodeExists'))
    return
  }
  // Check for 429/529 warning
  if (code === 429) {
    if (!confirm(t('admin.accounts.customErrorCodes429Warning'))) {
      return
    }
  } else if (code === 529) {
    if (!confirm(t('admin.accounts.customErrorCodes529Warning'))) {
      return
    }
  }
  selectedErrorCodes.value.push(code)
  customErrorCodeInput.value = null
}

// Remove error code
const removeErrorCode = (code: number) => {
  const index = selectedErrorCodes.value.indexOf(code)
  if (index !== -1) {
    selectedErrorCodes.value.splice(index, 1)
  }
}

const addTempUnschedRule = (preset?: TempUnschedRuleForm) => {
  if (preset) {
    tempUnschedRules.value.push({ ...preset })
    return
  }
  tempUnschedRules.value.push({
    error_code: null,
    keywords: '',
    duration_minutes: 30,
    description: ''
  })
}

const removeTempUnschedRule = (index: number) => {
  tempUnschedRules.value.splice(index, 1)
}

const moveTempUnschedRule = (index: number, direction: number) => {
  const target = index + direction
  if (target < 0 || target >= tempUnschedRules.value.length) return
  const rules = tempUnschedRules.value
  const current = rules[index]
  rules[index] = rules[target]
  rules[target] = current
}

const buildTempUnschedRules = (rules: TempUnschedRuleForm[]) => {
  const out: Array<{
    error_code: number
    keywords: string[]
    duration_minutes: number
    description: string
  }> = []

  for (const rule of rules) {
    const errorCode = Number(rule.error_code)
    const duration = Number(rule.duration_minutes)
    const keywords = splitTempUnschedKeywords(rule.keywords)
    if (!Number.isFinite(errorCode) || errorCode < 100 || errorCode > 599) {
      continue
    }
    if (!Number.isFinite(duration) || duration <= 0) {
      continue
    }
    if (keywords.length === 0) {
      continue
    }
    out.push({
      error_code: Math.trunc(errorCode),
      keywords,
      duration_minutes: Math.trunc(duration),
      description: rule.description.trim()
    })
  }

  return out
}

const applyTempUnschedConfig = (credentials: Record<string, unknown>) => {
  if (!tempUnschedEnabled.value) {
    delete credentials.temp_unschedulable_enabled
    delete credentials.temp_unschedulable_rules
    return true
  }

  const rules = buildTempUnschedRules(tempUnschedRules.value)
  if (rules.length === 0) {
    appStore.showError(t('admin.accounts.tempUnschedulable.rulesInvalid'))
    return false
  }

  credentials.temp_unschedulable_enabled = true
  credentials.temp_unschedulable_rules = rules
  return true
}

function loadTempUnschedRules(credentials?: Record<string, unknown>) {
  tempUnschedEnabled.value = credentials?.temp_unschedulable_enabled === true
  const rawRules = credentials?.temp_unschedulable_rules
  if (!Array.isArray(rawRules)) {
    tempUnschedRules.value = []
    return
  }

  tempUnschedRules.value = rawRules.map((rule) => {
    const entry = rule as Record<string, unknown>
    return {
      error_code: toPositiveNumber(entry.error_code),
      keywords: formatTempUnschedKeywords(entry.keywords),
      duration_minutes: toPositiveNumber(entry.duration_minutes),
      description: typeof entry.description === 'string' ? entry.description : ''
    }
  })
}

// Load quota control settings from account (Anthropic OAuth/SetupToken only)
function loadQuotaControlSettings(account: Account) {
  // Reset all quota control state first
  windowCostEnabled.value = false
  windowCostLimit.value = null
  windowCostStickyReserve.value = null
  sessionLimitEnabled.value = false
  maxSessions.value = null
  sessionIdleTimeout.value = null
  rpmLimitEnabled.value = false
  baseRpm.value = null
  rpmStrategy.value = 'tiered'
  rpmStickyBuffer.value = null
  userMsgQueueMode.value = ''
  tlsFingerprintEnabled.value = false
  tlsFingerprintProfileId.value = null
  sessionIdMaskingEnabled.value = false
  cacheTTLOverrideEnabled.value = false
  cacheTTLOverrideTarget.value = '5m'
  customBaseUrlEnabled.value = false
  customBaseUrl.value = ''

  // Remaining quota control settings only apply to Anthropic accounts
  if (account.platform !== 'anthropic') {
    return
  }

  // Window cost / session limit only apply to Anthropic OAuth/SetupToken accounts
  if (account.type !== 'oauth' && account.type !== 'setup-token') {
    return
  }

  // Load from extra field (via backend DTO fields)
  if (account.window_cost_limit != null && account.window_cost_limit > 0) {
    windowCostEnabled.value = true
    windowCostLimit.value = account.window_cost_limit
    windowCostStickyReserve.value = account.window_cost_sticky_reserve ?? 10
  }

  if (account.max_sessions != null && account.max_sessions > 0) {
    sessionLimitEnabled.value = true
    maxSessions.value = account.max_sessions
    sessionIdleTimeout.value = account.session_idle_timeout_minutes ?? 5
  }

  // RPM limit
  if (account.base_rpm != null && account.base_rpm > 0) {
    rpmLimitEnabled.value = true
    baseRpm.value = account.base_rpm
    rpmStrategy.value = (account.rpm_strategy as 'tiered' | 'sticky_exempt') || 'tiered'
    rpmStickyBuffer.value = account.rpm_sticky_buffer ?? null
  }

  // UMQ mode（独立于 RPM 加载，防止编辑无 RPM 账号时丢失已有配置）
  userMsgQueueMode.value = account.user_msg_queue_mode ?? ''

  // Load TLS fingerprint setting
  if (account.enable_tls_fingerprint === true) {
    tlsFingerprintEnabled.value = true
  }
  tlsFingerprintProfileId.value = account.tls_fingerprint_profile_id ?? null

  // Load session ID masking setting
  if (account.session_id_masking_enabled === true) {
    sessionIdMaskingEnabled.value = true
  }

  // Load cache TTL override setting
  if (account.cache_ttl_override_enabled === true) {
    cacheTTLOverrideEnabled.value = true
    cacheTTLOverrideTarget.value = account.cache_ttl_override_target || '5m'
  }

  // Load custom base URL setting
  if (account.custom_base_url_enabled === true) {
    customBaseUrlEnabled.value = true
    customBaseUrl.value = account.custom_base_url || ''
  }
}

function formatTempUnschedKeywords(value: unknown) {
  if (Array.isArray(value)) {
    return value
      .filter((item): item is string => typeof item === 'string')
      .map((item) => item.trim())
      .filter((item) => item.length > 0)
      .join(', ')
  }
  if (typeof value === 'string') {
    return value
  }
  return ''
}

const splitTempUnschedKeywords = (value: string) => {
  return value
    .split(/[,;]/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

function toPositiveNumber(value: unknown) {
  const num = Number(value)
  if (!Number.isFinite(num) || num <= 0) {
    return null
  }
  return Math.trunc(num)
}

const needsMixedChannelCheck = () => props.account?.platform === 'antigravity' || props.account?.platform === 'anthropic'

const buildMixedChannelDetails = (resp?: CheckMixedChannelResponse) => {
  const details = resp?.details
  if (!details) {
    return null
  }
  return {
    groupName: details.group_name || 'Unknown',
    currentPlatform: details.current_platform || 'Unknown',
    otherPlatform: details.other_platform || 'Unknown'
  }
}

const clearMixedChannelDialog = () => {
  showMixedChannelWarning.value = false
  mixedChannelWarningDetails.value = null
  mixedChannelWarningRawMessage.value = ''
  mixedChannelWarningAction.value = null
}

const openMixedChannelDialog = (opts: {
  response?: CheckMixedChannelResponse
  message?: string
  onConfirm: () => Promise<void>
}) => {
  mixedChannelWarningDetails.value = buildMixedChannelDetails(opts.response)
  mixedChannelWarningRawMessage.value =
    opts.message || opts.response?.message || t('admin.accounts.failedToUpdate')
  mixedChannelWarningAction.value = opts.onConfirm
  showMixedChannelWarning.value = true
}

const withAntigravityConfirmFlag = (payload: Record<string, unknown>) => {
  if (needsMixedChannelCheck() && antigravityMixedChannelConfirmed.value) {
    return {
      ...payload,
      confirm_mixed_channel_risk: true
    }
  }
  const cloned = { ...payload }
  delete cloned.confirm_mixed_channel_risk
  return cloned
}

const ensureAntigravityMixedChannelConfirmed = async (onConfirm: () => Promise<void>): Promise<boolean> => {
  if (!needsMixedChannelCheck()) {
    return true
  }
  if (antigravityMixedChannelConfirmed.value) {
    return true
  }
  if (!props.account) {
    return false
  }

  try {
    const result = await adminAPI.accounts.checkMixedChannelRisk({
      platform: props.account.platform,
      group_ids: form.group_ids,
      account_id: props.account.id
    })
    if (!result.has_risk) {
      return true
    }
    openMixedChannelDialog({
      response: result,
      onConfirm: async () => {
        antigravityMixedChannelConfirmed.value = true
        await onConfirm()
      }
    })
    return false
  } catch (error: any) {
    appStore.showError(error.message || t('admin.accounts.failedToUpdate'))
    return false
  }
}

const formatDateTimeLocal = formatDateTimeLocalInput
const parseDateTimeLocal = parseDateTimeLocalInput

// Methods
const handleClose = () => {
  antigravityMixedChannelConfirmed.value = false
  clearMixedChannelDialog()
  emit('close')
}

const submitUpdateAccount = async (accountID: number, updatePayload: Record<string, unknown>) => {
  submitting.value = true
  try {
    const updatedAccount = await adminAPI.accounts.update(accountID, withAntigravityConfirmFlag(updatePayload))
    appStore.showSuccess(t('admin.accounts.accountUpdated'))
    emit('updated', updatedAccount)
    handleClose()
  } catch (error: any) {
    if (error.status === 409 && error.error === 'mixed_channel_warning' && needsMixedChannelCheck()) {
      openMixedChannelDialog({
        message: error.message,
        onConfirm: async () => {
          antigravityMixedChannelConfirmed.value = true
          await submitUpdateAccount(accountID, updatePayload)
        }
      })
      return
    }
    appStore.showError(error.message || t('admin.accounts.failedToUpdate'))
  } finally {
    submitting.value = false
  }
}

const handleSubmit = async () => {
  if (!props.account) return
  const accountID = props.account.id

  if (form.status !== 'active' && form.status !== 'inactive' && form.status !== 'error') {
    appStore.showError(t('admin.accounts.pleaseSelectStatus'))
    return
  }

  const updatePayload: Record<string, unknown> = { ...form }
  try {
    // 后端期望 proxy_id: 0 表示清除代理，而不是 null
    if (updatePayload.proxy_id === null) {
      updatePayload.proxy_id = 0
    }
    if (form.expires_at === null) {
      updatePayload.expires_at = 0
    }
    // load_factor: 空值/NaN/0/负数 时发送 0（后端约定 <= 0 = 清除）
    const lf = form.load_factor
    if (lf == null || Number.isNaN(lf) || lf <= 0) {
      updatePayload.load_factor = 0
    }
    updatePayload.auto_pause_on_expired = autoPauseOnExpired.value

    // For apikey type, handle credentials update
    if (props.account.type === 'apikey') {
      const currentCredentials = (props.account.credentials as Record<string, unknown>) || {}
      const newBaseUrl = editBaseUrl.value.trim() || defaultBaseUrl.value
      const shouldApplyModelMapping = !(props.account.platform === 'openai' && openaiPassthroughEnabled.value)

      // Always update credentials for apikey type to handle model mapping changes
      const newCredentials: Record<string, unknown> = {
        ...currentCredentials,
        base_url: newBaseUrl
      }

      // Handle API key
      // 后端响应已脱敏：currentCredentials 不会再包含 api_key 原文。
      // 用户填入新值则覆盖；留空时优先看 credentials_status.has_api_key；
      // 若后端尚未升级（无 credentials_status），回退读旧结构 currentCredentials.api_key。
      // 两者都无才报错。
      const hasExistingApiKey =
        props.account.credentials_status?.has_api_key ?? Boolean(currentCredentials.api_key)
      if (editApiKey.value.trim()) {
        newCredentials.api_key = editApiKey.value.trim()
      } else if (!hasExistingApiKey) {
        appStore.showError(t('admin.accounts.apiKeyIsRequired'))
        return
      }

      // Add model mapping if configured（OpenAI 开启自动透传时保留现有映射，不再编辑）
      if (shouldApplyModelMapping) {
        const modelMapping = buildModelRestrictionMapping()
        if (modelMapping) {
          newCredentials.model_mapping = modelMapping
        } else {
          delete newCredentials.model_mapping
        }
      } else if (currentCredentials.model_mapping) {
        newCredentials.model_mapping = currentCredentials.model_mapping
      }
      if (props.account.platform === 'openai') {
        applyOpenAIEndpointCapabilities(newCredentials)
        const compactModelMapping = buildModelMappingObject('mapping', [], openAICompactModelMappings.value)
        if (compactModelMapping) {
          newCredentials.compact_model_mapping = compactModelMapping
        } else {
          delete newCredentials.compact_model_mapping
        }
      }

      // Add pool mode if enabled
      if (poolModeEnabled.value) {
        newCredentials.pool_mode = true
        newCredentials.pool_mode_retry_count = normalizePoolModeRetryCount(poolModeRetryCount.value)
        const parsedRetryStatusCodes = parsePoolModeRetryStatusCodes(poolModeRetryStatusCodesInput.value)
        if (parsedRetryStatusCodes.length > 0) {
          newCredentials.pool_mode_retry_status_codes = parsedRetryStatusCodes
        } else {
          delete newCredentials.pool_mode_retry_status_codes
        }
      } else {
        delete newCredentials.pool_mode
        delete newCredentials.pool_mode_retry_count
        delete newCredentials.pool_mode_retry_status_codes
      }

      // Add custom error codes if enabled
      if (customErrorCodesEnabled.value) {
        newCredentials.custom_error_codes_enabled = true
        newCredentials.custom_error_codes = [...selectedErrorCodes.value]
      } else {
        delete newCredentials.custom_error_codes_enabled
        delete newCredentials.custom_error_codes
      }

      // Add intercept warmup requests setting
      applyInterceptWarmup(newCredentials, interceptWarmupRequests.value, 'edit')
      if (!applyTempUnschedConfig(newCredentials)) {
        return
      }

      updatePayload.credentials = newCredentials
    } else if (props.account.type === 'upstream') {
      const currentCredentials = (props.account.credentials as Record<string, unknown>) || {}
      const newCredentials: Record<string, unknown> = { ...currentCredentials }

      newCredentials.base_url = editBaseUrl.value.trim()

      if (editApiKey.value.trim()) {
        newCredentials.api_key = editApiKey.value.trim()
      }

      // Add intercept warmup requests setting
      applyInterceptWarmup(newCredentials, interceptWarmupRequests.value, 'edit')

      if (!applyTempUnschedConfig(newCredentials)) {
        return
      }

      updatePayload.credentials = newCredentials
    } else if ((props.account.platform === 'gemini' || props.account.platform === 'anthropic') && props.account.type === 'service_account') {
      const currentCredentials = (props.account.credentials as Record<string, unknown>) || {}
      const newCredentials: Record<string, unknown> = { ...currentCredentials }

      if (!editVertexProjectId.value.trim()) {
        appStore.showError(t('admin.accounts.vertexSaJsonMissingProjectId'))
        return
      }
      if (!editVertexClientEmail.value.trim()) {
        appStore.showError(t('admin.accounts.vertexSaJsonMissingClientEmail'))
        return
      }
      if (!editVertexLocation.value.trim()) {
        appStore.showError(t('admin.accounts.vertexLocationRequired'))
        return
      }

      // SA JSON 已脱敏不再随 credentials 返回，存在性优先读 credentials_status。
      // 若后端尚未升级（无 credentials_status），回退读旧结构 service_account_json / service_account。
      const credentialsStatus = props.account.credentials_status
      const hasExistingServiceAccountJson = credentialsStatus
        ? Boolean(
            credentialsStatus.has_service_account_json || credentialsStatus.has_service_account
          )
        : Boolean(currentCredentials.service_account_json || currentCredentials.service_account)
      if (!hasExistingServiceAccountJson) {
        appStore.showError(t('admin.accounts.vertexSaJsonRequired'))
        return
      }
      newCredentials.project_id = editVertexProjectId.value.trim()
      newCredentials.client_email = editVertexClientEmail.value.trim()
      newCredentials.location = editVertexLocation.value.trim()
      newCredentials.tier_id = 'vertex'

      // Add model mapping if configured
      const modelMapping = buildModelRestrictionMapping()
      if (modelMapping) {
        newCredentials.model_mapping = modelMapping
      } else {
        delete newCredentials.model_mapping
      }

      applyInterceptWarmup(newCredentials, interceptWarmupRequests.value, 'edit')
      if (!applyTempUnschedConfig(newCredentials)) {
        return
      }

      updatePayload.credentials = newCredentials
    } else if (props.account.type === 'bedrock') {
      const currentCredentials = (props.account.credentials as Record<string, unknown>) || {}
      const newCredentials: Record<string, unknown> = { ...currentCredentials }

      newCredentials.aws_region = editBedrockRegion.value.trim()
      if (editBedrockForceGlobal.value) {
        newCredentials.aws_force_global = 'true'
      } else {
        delete newCredentials.aws_force_global
      }

      if (isBedrockAPIKeyMode.value) {
        // API Key mode: only update api_key if user provided new value
        if (editBedrockApiKeyValue.value.trim()) {
          newCredentials.api_key = editBedrockApiKeyValue.value.trim()
        }
      } else {
        // SigV4 mode
        newCredentials.aws_access_key_id = editBedrockAccessKeyId.value.trim()
        if (editBedrockSecretAccessKey.value.trim()) {
          newCredentials.aws_secret_access_key = editBedrockSecretAccessKey.value.trim()
        }
        if (editBedrockSessionToken.value.trim()) {
          newCredentials.aws_session_token = editBedrockSessionToken.value.trim()
        }
      }

      // Pool mode
      if (poolModeEnabled.value) {
        newCredentials.pool_mode = true
        newCredentials.pool_mode_retry_count = normalizePoolModeRetryCount(poolModeRetryCount.value)
        const parsedRetryStatusCodes = parsePoolModeRetryStatusCodes(poolModeRetryStatusCodesInput.value)
        if (parsedRetryStatusCodes.length > 0) {
          newCredentials.pool_mode_retry_status_codes = parsedRetryStatusCodes
        } else {
          delete newCredentials.pool_mode_retry_status_codes
        }
      } else {
        delete newCredentials.pool_mode
        delete newCredentials.pool_mode_retry_count
        delete newCredentials.pool_mode_retry_status_codes
      }

      // Model mapping
      const modelMapping = buildModelRestrictionMapping()
      if (modelMapping) {
        newCredentials.model_mapping = modelMapping
      } else {
        delete newCredentials.model_mapping
      }

      applyInterceptWarmup(newCredentials, interceptWarmupRequests.value, 'edit')
      if (!applyTempUnschedConfig(newCredentials)) {
        return
      }

      updatePayload.credentials = newCredentials
    } else {
      // For oauth/setup-token types, only update intercept_warmup_requests if changed
      const currentCredentials = (props.account.credentials as Record<string, unknown>) || {}
      const newCredentials: Record<string, unknown> = { ...currentCredentials }

      applyInterceptWarmup(newCredentials, interceptWarmupRequests.value, 'edit')
      if (!applyTempUnschedConfig(newCredentials)) {
        return
      }

      updatePayload.credentials = newCredentials
    }

    // OpenAI OAuth: persist model mapping to credentials
    if (props.account.platform === 'openai' && props.account.type === 'oauth') {
      const currentCredentials = (updatePayload.credentials as Record<string, unknown>) ||
        ((props.account.credentials as Record<string, unknown>) || {})
      const newCredentials: Record<string, unknown> = { ...currentCredentials }
      const shouldApplyModelMapping = !openaiPassthroughEnabled.value

      if (shouldApplyModelMapping) {
        const modelMapping = buildModelRestrictionMapping()
        if (modelMapping) {
          newCredentials.model_mapping = modelMapping
        } else {
          delete newCredentials.model_mapping
        }
      } else if (currentCredentials.model_mapping) {
        // 透传模式保留现有映射
        newCredentials.model_mapping = currentCredentials.model_mapping
      }
      const compactModelMapping = buildModelMappingObject('mapping', [], openAICompactModelMappings.value)
      if (compactModelMapping) {
        newCredentials.compact_model_mapping = compactModelMapping
      } else {
        delete newCredentials.compact_model_mapping
      }

      updatePayload.credentials = newCredentials
    }

    // Grok OAuth: persist model mapping to credentials
    if (props.account.platform === 'grok' && props.account.type === 'oauth') {
      const currentCredentials = (updatePayload.credentials as Record<string, unknown>) ||
        ((props.account.credentials as Record<string, unknown>) || {})
      const newCredentials: Record<string, unknown> = { ...currentCredentials }

      const modelMapping = buildModelRestrictionMapping()
      if (modelMapping) {
        newCredentials.model_mapping = modelMapping
      } else {
        delete newCredentials.model_mapping
      }

      updatePayload.credentials = newCredentials
    }

    // Antigravity: persist model mapping to credentials (applies to all antigravity types)
    // Antigravity 只支持映射模式
    if (props.account.platform === 'antigravity') {
      const currentCredentials = (updatePayload.credentials as Record<string, unknown>) ||
        ((props.account.credentials as Record<string, unknown>) || {})
      const newCredentials: Record<string, unknown> = { ...currentCredentials }

      // 移除旧字段
      delete newCredentials.model_whitelist
      delete newCredentials.model_mapping

      // 只使用映射模式
      const antigravityModelMapping = buildModelMappingObject(
        'mapping',
        [],
        antigravityModelMappings.value
      )
      if (antigravityModelMapping) {
        newCredentials.model_mapping = antigravityModelMapping
      }

      updatePayload.credentials = newCredentials
    }

    // For antigravity accounts, handle mixed_scheduling and allow_overages in extra
    if (props.account.platform === 'antigravity') {
      const currentExtra = (props.account.extra as Record<string, unknown>) || {}
      const newExtra: Record<string, unknown> = { ...currentExtra }
      if (mixedScheduling.value) {
        newExtra.mixed_scheduling = true
      } else {
        delete newExtra.mixed_scheduling
      }
      if (allowOverages.value) {
        newExtra.allow_overages = true
      } else {
        delete newExtra.allow_overages
      }
      updatePayload.extra = newExtra
    }

    // For Anthropic OAuth/SetupToken accounts, handle quota control settings in extra
    if (props.account.platform === 'anthropic' && (props.account.type === 'oauth' || props.account.type === 'setup-token')) {
      const currentExtra = (updatePayload.extra as Record<string, unknown>) || (props.account.extra as Record<string, unknown>) || {}
      const newExtra: Record<string, unknown> = { ...currentExtra }

      // Window cost limit settings
      if (windowCostEnabled.value && windowCostLimit.value != null && windowCostLimit.value > 0) {
        newExtra.window_cost_limit = windowCostLimit.value
        newExtra.window_cost_sticky_reserve = windowCostStickyReserve.value ?? 10
      } else {
        delete newExtra.window_cost_limit
        delete newExtra.window_cost_sticky_reserve
      }

      // Session limit settings
      if (sessionLimitEnabled.value && maxSessions.value != null && maxSessions.value > 0) {
        newExtra.max_sessions = maxSessions.value
        newExtra.session_idle_timeout_minutes = sessionIdleTimeout.value ?? 5
      } else {
        delete newExtra.max_sessions
        delete newExtra.session_idle_timeout_minutes
      }

      // RPM limit settings
      if (rpmLimitEnabled.value) {
        const DEFAULT_BASE_RPM = 15
        newExtra.base_rpm = (baseRpm.value != null && baseRpm.value > 0)
          ? baseRpm.value
          : DEFAULT_BASE_RPM
        newExtra.rpm_strategy = rpmStrategy.value
        if (rpmStickyBuffer.value != null && rpmStickyBuffer.value > 0) {
          newExtra.rpm_sticky_buffer = rpmStickyBuffer.value
        } else {
          delete newExtra.rpm_sticky_buffer
        }
      } else {
        delete newExtra.base_rpm
        delete newExtra.rpm_strategy
        delete newExtra.rpm_sticky_buffer
      }

      // UMQ mode（独立于 RPM 保存）
      if (userMsgQueueMode.value) {
        newExtra.user_msg_queue_mode = userMsgQueueMode.value
      } else {
        delete newExtra.user_msg_queue_mode
      }
      delete newExtra.user_msg_queue_enabled  // 清理旧字段

      // TLS fingerprint setting
      if (tlsFingerprintEnabled.value) {
        newExtra.enable_tls_fingerprint = true
        if (tlsFingerprintProfileId.value) {
          newExtra.tls_fingerprint_profile_id = tlsFingerprintProfileId.value
        } else {
          delete newExtra.tls_fingerprint_profile_id
        }
      } else {
        delete newExtra.enable_tls_fingerprint
        delete newExtra.tls_fingerprint_profile_id
      }

      // Session ID masking setting
      if (sessionIdMaskingEnabled.value) {
        newExtra.session_id_masking_enabled = true
      } else {
        delete newExtra.session_id_masking_enabled
      }

      // Cache TTL override setting
      if (cacheTTLOverrideEnabled.value) {
        newExtra.cache_ttl_override_enabled = true
        newExtra.cache_ttl_override_target = cacheTTLOverrideTarget.value
      } else {
        delete newExtra.cache_ttl_override_enabled
        delete newExtra.cache_ttl_override_target
      }

      // Custom base URL relay setting
      if (customBaseUrlEnabled.value && customBaseUrl.value.trim()) {
        newExtra.custom_base_url_enabled = true
        newExtra.custom_base_url = customBaseUrl.value.trim()
      } else {
        delete newExtra.custom_base_url_enabled
        delete newExtra.custom_base_url
      }

      updatePayload.extra = newExtra
    }

    // For Anthropic API Key accounts, handle relay mode + web search emulation in extra
    if (props.account.platform === 'anthropic' && props.account.type === 'apikey') {
      const currentExtra = (updatePayload.extra as Record<string, unknown>) || (props.account.extra as Record<string, unknown>) || {}
      const newExtra: Record<string, unknown> = { ...currentExtra }
      writeRelayModeToExtra(newExtra, anthropicRelayMode.value)
      if (webSearchEmulationMode.value === 'default') {
        delete newExtra.web_search_emulation
      } else {
        newExtra.web_search_emulation = webSearchEmulationMode.value
      }
      updatePayload.extra = newExtra
    }

    // For OpenAI OAuth/API Key accounts, handle relay mode in extra
	if (props.account.platform === 'openai' && (props.account.type === 'oauth' || props.account.type === 'apikey')) {
		const currentExtra = (props.account.extra as Record<string, unknown>) || {}
		const newExtra: Record<string, unknown> = { ...currentExtra }
      const hadCodexCLIOnlyEnabled = currentExtra.codex_cli_only === true
      if (props.account.type === 'oauth') {
        newExtra.openai_oauth_responses_websockets_v2_mode = openaiOAuthResponsesWebSocketV2Mode.value
        newExtra.openai_oauth_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(openaiOAuthResponsesWebSocketV2Mode.value)
      } else if (props.account.type === 'apikey') {
        newExtra.openai_apikey_responses_websockets_v2_mode = openaiAPIKeyResponsesWebSocketV2Mode.value
        newExtra.openai_apikey_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(openaiAPIKeyResponsesWebSocketV2Mode.value)
      }
      delete newExtra.responses_websockets_v2_enabled
      delete newExtra.openai_ws_enabled
      writeRelayModeToExtra(newExtra, openaiRelayMode.value)
      if (openAICompactMode.value === 'auto') {
        delete newExtra.openai_compact_mode
      } else {
        newExtra.openai_compact_mode = openAICompactMode.value
      }
		if (props.account.type === 'apikey') {
        if (!openAITextGenerationCapabilityEnabled.value || openAIResponsesMode.value === 'auto') {
          delete newExtra.openai_responses_mode
        } else {
          newExtra.openai_responses_mode = openAIResponsesMode.value
        }
		}
		if (autoPause5hThreshold.value != null && autoPause5hThreshold.value > 0) {
			newExtra.auto_pause_5h_threshold = autoPause5hThreshold.value / 100
		} else {
			delete newExtra.auto_pause_5h_threshold
		}
		if (autoPause7dThreshold.value != null && autoPause7dThreshold.value > 0) {
			newExtra.auto_pause_7d_threshold = autoPause7dThreshold.value / 100
		} else {
			delete newExtra.auto_pause_7d_threshold
		}
		if (autoPause5hDisabled.value) {
			newExtra.auto_pause_5h_disabled = true
		} else {
			delete newExtra.auto_pause_5h_disabled
		}
		if (autoPause7dDisabled.value) {
			newExtra.auto_pause_7d_disabled = true
		} else {
			delete newExtra.auto_pause_7d_disabled
		}

		delete newExtra.codex_image_generation_bridge_enabled
      if (codexImageGenerationBridgeMode.value === 'inherit') {
        delete newExtra.codex_image_generation_bridge
      } else {
        newExtra.codex_image_generation_bridge = codexImageGenerationBridgeMode.value === 'enabled'
      }

      if (props.account.type === 'oauth') {
        if (codexCLIOnlyEnabled.value) {
          newExtra.codex_cli_only = true
        } else if (hadCodexCLIOnlyEnabled) {
          // 关闭时显式写 false，避免 extra 为空被后端忽略导致旧值无法清除
          newExtra.codex_cli_only = false
        } else {
          delete newExtra.codex_cli_only
        }
        // 仅当 codex_cli_only 开启且子开关开启时写入 Claude Code 插件白名单，否则清除避免孤立字段
        if (codexCLIOnlyEnabled.value && codexCLIOnlyAllowClaudeCodeEnabled.value) {
          newExtra.codex_cli_only_allowed_clients = ['claude_code']
        } else {
          delete newExtra.codex_cli_only_allowed_clients
        }
      }

      updatePayload.extra = newExtra
    }

    // For Gemini OAuth/API Key accounts, handle relay mode in extra
    if (props.account.platform === 'gemini' && props.account.type !== 'service_account') {
      const currentExtra = (updatePayload.extra as Record<string, unknown>) ||
        (props.account.extra as Record<string, unknown>) || {}
      const newExtra: Record<string, unknown> = { ...currentExtra }
      writeRelayModeToExtra(newExtra, geminiRelayMode.value)
      updatePayload.extra = newExtra
    }

    // For Custom accounts, handle credentials update (protocol, base_url, api_key) and relay mode in extra.
    if (props.account.platform === 'custom') {
      const selectedCustomProtocol = editCustomProtocol.value.trim()
      if (!selectedCustomProtocol) {
        appStore.showError(t('admin.accounts.custom.pleaseSelectProtocol'))
        return
      }
      const currentCredentials = (props.account.credentials as Record<string, unknown>) || {}
      const newCredentials: Record<string, unknown> = { ...currentCredentials }
      delete newCredentials.protocol

      // Update base URL
      if (editCustomBaseUrl.value.trim()) {
        newCredentials.base_url = editCustomBaseUrl.value.trim()
      }

      // Update API key (only if provided, otherwise keep existing)
      if (editCustomApiKey.value.trim()) {
        newCredentials.api_key = editCustomApiKey.value.trim()
      }

      updatePayload.credentials = newCredentials

      const currentExtra = (updatePayload.extra as Record<string, unknown>) ||
        (props.account.extra as Record<string, unknown>) || {}
      const newExtra: Record<string, unknown> = { ...currentExtra }
      newExtra.protocol = selectedCustomProtocol
      writeRelayModeToExtra(newExtra, customRelayMode.value)
      updatePayload.extra = newExtra
    }

    // For apikey/bedrock accounts, handle quota_limit in extra
    if (props.account.type === 'apikey' || props.account.type === 'bedrock') {
      const currentExtra = (updatePayload.extra as Record<string, unknown>) ||
        (props.account.extra as Record<string, unknown>) || {}
      const newExtra: Record<string, unknown> = { ...currentExtra }
      // Total quota
      if (editQuotaLimit.value != null && editQuotaLimit.value > 0) {
        newExtra.quota_limit = editQuotaLimit.value
      } else {
        delete newExtra.quota_limit
      }
      // Daily quota
      if (editQuotaDailyLimit.value != null && editQuotaDailyLimit.value > 0) {
        newExtra.quota_daily_limit = editQuotaDailyLimit.value
      } else {
        delete newExtra.quota_daily_limit
        delete newExtra.quota_daily_used
        delete newExtra.quota_daily_start
      }
      // Weekly quota
      if (editQuotaWeeklyLimit.value != null && editQuotaWeeklyLimit.value > 0) {
        newExtra.quota_weekly_limit = editQuotaWeeklyLimit.value
      } else {
        delete newExtra.quota_weekly_limit
        delete newExtra.quota_weekly_used
        delete newExtra.quota_weekly_start
      }
      // Quota reset mode config
      if (editDailyResetMode.value === 'fixed') {
        newExtra.quota_daily_reset_mode = 'fixed'
        newExtra.quota_daily_reset_hour = editDailyResetHour.value ?? 0
      } else {
        delete newExtra.quota_daily_reset_mode
        delete newExtra.quota_daily_reset_hour
      }
      if (editWeeklyResetMode.value === 'fixed') {
        newExtra.quota_weekly_reset_mode = 'fixed'
        newExtra.quota_weekly_reset_day = editWeeklyResetDay.value ?? 1
        newExtra.quota_weekly_reset_hour = editWeeklyResetHour.value ?? 0
      } else {
        delete newExtra.quota_weekly_reset_mode
        delete newExtra.quota_weekly_reset_day
        delete newExtra.quota_weekly_reset_hour
      }
      if (editDailyResetMode.value === 'fixed' || editWeeklyResetMode.value === 'fixed') {
        newExtra.quota_reset_timezone = editResetTimezone.value || 'UTC'
      } else {
        delete newExtra.quota_reset_timezone
      }
      // Quota notify config
      writeQuotaNotifyToExtra(newExtra, 'update')
      updatePayload.extra = newExtra
    }

    applyModelListExtra(updatePayload)

    const canContinue = await ensureAntigravityMixedChannelConfirmed(async () => {
      await submitUpdateAccount(accountID, updatePayload)
    })
    if (!canContinue) {
      return
    }

    await submitUpdateAccount(accountID, updatePayload)
  } catch (error: any) {
    appStore.showError(error.message || t('admin.accounts.failedToUpdate'))
  }
}

// Handle mixed channel warning confirmation
const handleMixedChannelConfirm = async () => {
  const action = mixedChannelWarningAction.value
  if (!action) {
    clearMixedChannelDialog()
    return
  }
  clearMixedChannelDialog()
  submitting.value = true
  try {
    await action()
  } finally {
    submitting.value = false
  }
}

const handleMixedChannelCancel = () => {
  clearMixedChannelDialog()
}
</script>
