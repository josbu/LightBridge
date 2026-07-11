<template src="./templates/CreateAccountModal.template.html"></template>

<script setup lang="ts">
import { ref, reactive, computed, defineAsyncComponent, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useDeploymentMode } from '@/composables/useDeploymentMode'
import {
  claudeModels,
  getPresetMappingsByPlatform,
  getModelsByPlatform,
  commonErrorCodes,
  buildModelMappingObject,
  fetchAntigravityDefaultMappings,
  isValidWildcardPattern
} from '@/composables/useModelWhitelist'
import { useAuthStore } from '@/stores/auth'
import { adminAPI } from '@/api/admin'
import {
  aistudioProxyImportCookies,
  aistudioProxyRuntimeStatus,
  aistudioProxyRuntimeInstall,
  aistudioProxyStartLogin,
  aistudioProxyLoginStatus,
  type AistudioProxyRuntimeStatus,
} from '@/api/admin/accounts'
import { useQuotaNotifyState } from '@/composables/useQuotaNotifyState'
import {
  useAccountOAuth,
  type AddMethod,
  type AuthInputMethod
} from '@/composables/useAccountOAuth'
import { useOpenAIOAuth } from '@/composables/useOpenAIOAuth'
import { useGeminiOAuth } from '@/composables/useGeminiOAuth'
import { useAntigravityOAuth } from '@/composables/useAntigravityOAuth'
import { useGrokOAuth } from '@/composables/useGrokOAuth'
import {
  PRESETS_BY_PROTOCOL,
  findPresetById
} from '@/config/customProviderPresets'
import type {
  Proxy,
  AdminGroup,
  AccountPlatform,
  AccountType,
  CheckMixedChannelResponse,
  CreateAccountRequest,
  CodexSessionImportMessage,
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
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import QuotaLimitCard from '@/components/account/QuotaLimitCard.vue'
import { applyInterceptWarmup } from '@/components/account/credentialsBuilder'
import { formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/utils/format'
import { createStableObjectKeyResolver } from '@/utils/stableObjectKey'
import { VERTEX_LOCATION_OPTIONS } from '@/constants/account'
import {
  OPENAI_WS_MODE_CTX_POOL,
  OPENAI_WS_MODE_HTTP_BRIDGE,
  OPENAI_WS_MODE_OFF,
  OPENAI_WS_MODE_PASSTHROUGH,
  isOpenAIWSModeEnabled,
  resolveOpenAIWSModeConcurrencyHintKey,
  type OpenAIWSMode
} from '@/utils/openaiWsMode'
import {
  RELAY_MODE_FULL_PASSTHROUGH,
  RELAY_MODE_PASSTHROUGH,
  RELAY_MODE_ROUTER,
  writeRelayModeToExtra,
  type RelayMode
} from '@/utils/relayMode'
import OAuthAuthorizationFlow from './OAuthAuthorizationFlow.vue'
import { isProgressiveFeatureEnabled, ProgressiveFeatures } from '@/utils/progressiveFeatures'

const LightBridgeConnectConfig = defineAsyncComponent(() => import('@/components/account/LightBridgeConnectConfig.vue'))

// Type for exposed OAuthAuthorizationFlow component
// Note: defineExpose automatically unwraps refs, so we use the unwrapped types
interface OAuthFlowExposed {
  authCode: string
  oauthState: string
  projectId: string
  sessionKey: string
  refreshToken: string
  sessionToken: string
  codexSession: string
  inputMethod: AuthInputMethod
  reset: () => void
}

const { t } = useI18n()
const authStore = useAuthStore()

const oauthStepTitle = computed(() => {
  if (isAistudioProxyFlow.value) return t('admin.accounts.gemini.providerAistudioProxy')
  if (form.platform === 'custom') return t('admin.accounts.custom.accountAddTitle')
  if (form.platform === 'openai') return t('admin.accounts.oauth.openai.title')
  if (form.platform === 'gemini') return t('admin.accounts.oauth.gemini.title')
  if (form.platform === 'grok') return t('admin.accounts.oauth.grok.title')
  if (form.platform === 'antigravity') return t('admin.accounts.oauth.antigravity.title')
  return t('admin.accounts.oauth.title')
})

const apiKeyHint = computed(() => {
  if (form.platform === 'openai') return t('admin.accounts.openai.apiKeyHint')
  if (form.platform === 'gemini') return t('admin.accounts.gemini.apiKeyHint')
  return t('admin.accounts.apiKeyHint')
})

interface Props {
  show: boolean
  proxies: Proxy[]
  groups: AdminGroup[]
}

const props = defineProps<Props>()
const emit = defineEmits<{
  close: []
  created: []
}>()

const appStore = useAppStore()

// OAuth composables
const oauth = useAccountOAuth() // For Anthropic OAuth
const openaiOAuth = useOpenAIOAuth() // For OpenAI OAuth
const geminiOAuth = useGeminiOAuth() // For Gemini OAuth
const antigravityOAuth = useAntigravityOAuth() // For Antigravity OAuth
const grokOAuth = useGrokOAuth() // For Grok OAuth

// Computed: current OAuth state for template binding
const currentAuthUrl = computed(() => {
  if (form.platform === 'openai') return openaiOAuth.authUrl.value
  if (form.platform === 'gemini') return geminiOAuth.authUrl.value
  if (form.platform === 'grok') return grokOAuth.authUrl.value
  if (form.platform === 'antigravity') return antigravityOAuth.authUrl.value
  return oauth.authUrl.value
})

const currentSessionId = computed(() => {
  if (form.platform === 'openai') return openaiOAuth.sessionId.value
  if (form.platform === 'gemini') return geminiOAuth.sessionId.value
  if (form.platform === 'grok') return grokOAuth.sessionId.value
  if (form.platform === 'antigravity') return antigravityOAuth.sessionId.value
  return oauth.sessionId.value
})

const currentOAuthLoading = computed(() => {
  if (form.platform === 'openai') return openaiOAuth.loading.value
  if (form.platform === 'gemini') return geminiOAuth.loading.value
  if (form.platform === 'grok') return grokOAuth.loading.value
  if (form.platform === 'antigravity') return antigravityOAuth.loading.value
  return oauth.loading.value
})

const currentOAuthError = computed(() => {
  if (form.platform === 'openai') return openaiOAuth.error.value
  if (form.platform === 'gemini') return geminiOAuth.error.value
  if (form.platform === 'grok') return grokOAuth.error.value
  if (form.platform === 'antigravity') return antigravityOAuth.error.value
  return oauth.error.value
})

// Refs
const oauthFlowRef = ref<OAuthFlowExposed | null>(null)

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
const step = ref(1)
const submitting = ref(false)
const accountCategory = ref<'oauth-based' | 'apikey' | 'bedrock' | 'service_account'>('oauth-based') // UI selection for account category
const addMethod = ref<AddMethod>('oauth') // For oauth-based: 'oauth' or 'setup-token'
const apiKeyBaseUrl = ref('https://api.anthropic.com')
const apiKeyValue = ref('')
const editQuotaLimit = ref<number | null>(null)
const editQuotaDailyLimit = ref<number | null>(null)
const editQuotaWeeklyLimit = ref<number | null>(null)
const editDailyResetMode = ref<'rolling' | 'fixed' | null>(null)
const editDailyResetHour = ref<number | null>(null)
const editWeeklyResetMode = ref<'rolling' | 'fixed' | null>(null)
const editWeeklyResetDay = ref<number | null>(null)
const editWeeklyResetHour = ref<number | null>(null)
const editResetTimezone = ref<string | null>(null)
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
const customErrorCodesEnabled = ref(false)
const selectedErrorCodes = ref<number[]>([])
const customErrorCodeInput = ref<number | null>(null)
const interceptWarmupRequests = ref(false)
const autoPauseOnExpired = ref(true)
const openaiRelayMode = ref<RelayMode>(RELAY_MODE_ROUTER)
const openAICompactMode = ref<OpenAICompactMode>('auto')
const openAIResponsesMode = ref<OpenAIResponsesMode>('auto')
const openAIEndpointCapabilities = ref<OpenAIEndpointCapability[]>(['chat_completions', 'embeddings'])
const openaiOAuthResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
const openaiAPIKeyResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
const codexCLIOnlyEnabled = ref(false)
const codexCLIOnlyAllowClaudeCodeEnabled = ref(false)
const anthropicRelayMode = ref<RelayMode>(RELAY_MODE_ROUTER)
const geminiRelayMode = ref<RelayMode>(RELAY_MODE_ROUTER)
const customRelayMode = ref<RelayMode>(RELAY_MODE_ROUTER)
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
  writeToExtra: writeQuotaNotifyToExtra,
} = useQuotaNotifyState()

// Load global feature states once
adminAPI.settings.getWebSearchEmulationConfig().then(cfg => {
  webSearchGlobalEnabled.value = cfg?.enabled === true && (cfg?.providers?.length ?? 0) > 0
}).catch(() => { webSearchGlobalEnabled.value = false })

loadQuotaNotifyGlobal()
const mixedScheduling = ref(false) // For antigravity accounts: enable mixed scheduling
const allowOverages = ref(false) // For antigravity accounts: enable AI Credits overages
const antigravityAccountType = ref<'oauth' | 'upstream'>('oauth') // For antigravity: oauth or upstream
const upstreamBaseUrl = ref('') // For upstream type: base URL
const upstreamApiKey = ref('') // For upstream type: API key
const antigravityModelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')
const antigravityWhitelistModels = ref<string[]>([])
const antigravityModelMappings = ref<ModelMapping[]>([])
const antigravityPresetMappings = computed(() => getPresetMappingsByPlatform('antigravity'))
const bedrockPresets = computed(() => getPresetMappingsByPlatform('bedrock'))

// Bedrock credentials
const bedrockAuthMode = ref<'sigv4' | 'apikey'>('sigv4')
const bedrockAccessKeyId = ref('')
const bedrockSecretAccessKey = ref('')
const bedrockSessionToken = ref('')
const bedrockRegion = ref('us-east-1')
const bedrockForceGlobal = ref(false)
const bedrockApiKeyValue = ref('')
const vertexServiceAccountFileInput = ref<HTMLInputElement | null>(null)
const vertexServiceAccountJson = ref('')
const vertexProjectId = ref('')
const vertexClientEmail = ref('')
const vertexLocation = ref('global')
const vertexServiceAccountDragActive = ref(false)
const tempUnschedEnabled = ref(false)
const tempUnschedRules = ref<TempUnschedRuleForm[]>([])
const getModelMappingKey = createStableObjectKeyResolver<ModelMapping>('create-model-mapping')
const getOpenAICompactModelMappingKey = createStableObjectKeyResolver<ModelMapping>('create-openai-compact-model-mapping')
const getAntigravityModelMappingKey = createStableObjectKeyResolver<ModelMapping>('create-antigravity-model-mapping')
const getTempUnschedRuleKey = createStableObjectKeyResolver<TempUnschedRuleForm>('create-temp-unsched-rule')
const geminiOAuthType = ref<'code_assist' | 'google_one' | 'ai_studio'>('google_one')
const geminiAIStudioOAuthEnabled = ref(false)
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

function buildAntigravityExtra(): Record<string, unknown> | undefined {
  const extra: Record<string, unknown> = {}
  if (mixedScheduling.value) extra.mixed_scheduling = true
  if (allowOverages.value) extra.allow_overages = true
  return Object.keys(extra).length > 0 ? extra : undefined
}

const buildOpenAICompactModelMapping = () =>
  buildModelMappingObject('mapping', [], openAICompactModelMappings.value)

const buildAdvancedModelMapping = () =>
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

const buildModelListExtra = (base?: Record<string, unknown>): Record<string, unknown> | undefined => {
  const extra: Record<string, unknown> = { ...(base || {}) }
  extra.supported_models = normalizedModelList()
  extra.restrict_to_model_list = restrictToModelList.value
  return Object.keys(extra).length > 0 ? extra : undefined
}

const showMixedChannelWarning = ref(false)
const mixedChannelWarningDetails = ref<{ groupName: string; currentPlatform: string; otherPlatform: string } | null>(
  null
)
const mixedChannelWarningRawMessage = ref('')
const mixedChannelWarningAction = ref<(() => Promise<void>) | null>(null)
const antigravityMixedChannelConfirmed = ref(false)
const showAdvancedOAuth = ref(false)
const showGeminiHelpDialog = ref(false)
const showAdvancedMenu = ref(false)

// 个人模式：把名称/备注/平台/添加方式/分组 以外的非必需选项（配额控制、代理/有效期/
// 并发等高级菜单）折叠起来，默认隐藏。用户点击「显示更多选项」可临时展开。
const { isPersonalMode } = useDeploymentMode()
const showPersonalOptional = ref(false)
// 这些「可选」区块在个人模式下默认折叠：仅当非个人模式、或用户已展开时才渲染。
const showOptionalSections = computed(() => !isPersonalMode.value || showPersonalOptional.value)

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

// Gemini tier selection (used as fallback when auto-detection is unavailable/fails)
const geminiTierGoogleOne = ref<'google_one_free' | 'google_ai_pro' | 'google_ai_ultra'>('google_one_free')
const geminiTierGcp = ref<'gcp_standard' | 'gcp_enterprise'>('gcp_standard')
const geminiTierAIStudio = ref<'aistudio_free' | 'aistudio_paid'>('aistudio_free')

// Gemini 鉴权模式：official=官方 AI Studio（x-goog-api-key）；proxy=AIStudio 反代（Bearer）。
// 仅在 Gemini 平台 + APIKey 类型下生效。选择「AIStudio 反代」接入卡片时自动切到 proxy。
const geminiAuthMode = ref<'official' | 'proxy'>('official')

// 反代模式下用户粘贴的 Google cookie 串（创建账号后用于绑定 Google 会话）。
const geminiProxyCookie = ref('')
const geminiProxyBinding = ref(false)
// 反代绑定方式：cookie（M1）或 login（M3 有头浏览器引导）。
const geminiProxyBindMode = ref<'cookie' | 'login'>('cookie')
// 反代运行时检测/安装状态（M2）。
const proxyRuntimeStatus = ref<AistudioProxyRuntimeStatus | null>(null)
const proxyRuntimeChecking = ref(false)
const proxyRuntimeInstalling = ref(false)
const proxyRuntimeLogs = ref<string[]>([])

// 选择「AIStudio 反代」接入卡片：切到 Gemini 平台 + APIKey 类型 + Bearer 鉴权模式。
function selectAistudioProxy() {
  form.platform = 'gemini'
  geminiAuthMode.value = 'proxy'
  accountCategory.value = 'apikey'
}

async function checkProxyRuntime() {
  proxyRuntimeChecking.value = true
  try {
    proxyRuntimeStatus.value = await aistudioProxyRuntimeStatus()
  } catch (e: any) {
    appStore.showError(e.response?.data?.detail || e.message || 'runtime check failed')
  } finally {
    proxyRuntimeChecking.value = false
  }
}

async function installProxyRuntime() {
  proxyRuntimeInstalling.value = true
  proxyRuntimeLogs.value = []
  try {
    await aistudioProxyRuntimeInstall((line) => {
      proxyRuntimeLogs.value.push(line)
    })
    appStore.showSuccess(t('admin.accounts.gemini.proxyRuntimeInstallDone'))
    await checkProxyRuntime()
  } catch (e: any) {
    proxyRuntimeLogs.value.push(e.message || 'install failed')
    appStore.showError(e.message || t('admin.accounts.gemini.proxyRuntimeInstallFailed'))
  } finally {
    proxyRuntimeInstalling.value = false
  }
}

const geminiSelectedTier = computed(() => {
  if (form.platform !== 'gemini') return ''
  if (accountCategory.value === 'apikey') return geminiTierAIStudio.value
  switch (geminiOAuthType.value) {
    case 'google_one':
      return geminiTierGoogleOne.value
    case 'code_assist':
      return geminiTierGcp.value
    default:
      return geminiTierAIStudio.value
  }
})

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

const openaiResponsesWebSocketV2Mode = computed({
  get: () => {
    if (form.platform === 'openai' && accountCategory.value === 'apikey') {
      return openaiAPIKeyResponsesWebSocketV2Mode.value
    }
    return openaiOAuthResponsesWebSocketV2Mode.value
  },
  set: (mode: OpenAIWSMode) => {
    if (form.platform === 'openai' && accountCategory.value === 'apikey') {
      openaiAPIKeyResponsesWebSocketV2Mode.value = mode
      return
    }
    openaiOAuthResponsesWebSocketV2Mode.value = mode
  }
})

const openAIWSModeConcurrencyHintKey = computed(() =>
  resolveOpenAIWSModeConcurrencyHintKey(openaiResponsesWebSocketV2Mode.value)
)

const isOpenAIModelRestrictionDisabled = computed(() =>
  form.platform === 'openai' && openaiPassthroughEnabled.value
)

const mixedChannelWarningMessageText = computed(() => {
  if (mixedChannelWarningDetails.value) {
    return t('admin.accounts.mixedChannelWarning', mixedChannelWarningDetails.value)
  }
  return mixedChannelWarningRawMessage.value
})

const geminiQuotaDocs = {
  codeAssist: 'https://developers.google.com/gemini-code-assist/resources/quotas',
  aiStudio: 'https://ai.google.dev/pricing',
  vertex: 'https://cloud.google.com/vertex-ai/generative-ai/docs/quotas'
}

const geminiHelpLinks = {
  apiKey: 'https://aistudio.google.com/app/apikey',
  aiStudioPricing: 'https://ai.google.dev/pricing',
  gcpProject: 'https://console.cloud.google.com/welcome/new',
  geminiWebActivation: 'https://gemini.google.com/gems/create?hl=en-US&pli=1',
  countryCheck: 'https://policies.google.com/terms',
  countryChange: 'https://policies.google.com/country-association-form'
}

// Computed: current preset mappings based on platform
const presetMappings = computed(() => getPresetMappingsByPlatform(form.platform))
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

// Custom Provider Presets
const selectedPreset = ref('')
const presetsByProtocol = PRESETS_BY_PROTOCOL

// Protocol mapping: preset protocol -> LightBridge form protocol
const protocolMapping: Record<string, string> = {
  'openai-chat': 'openai_chat_completions',
  'openai-responses': 'openai_responses',
  'openai-embeddings': 'openai_embeddings',
  'anthropic': 'anthropic_messages',
  'gemini': 'gemini'
}

const applyPreset = () => {
  if (!selectedPreset.value) return

  const preset = findPresetById(selectedPreset.value)
  if (!preset) return

  // Auto-fill Base URL and Protocol
  form.customBaseUrl = preset.baseUrl
  form.customProtocol = protocolMapping[preset.protocol] || ''

  // Auto-fill account name if empty
  if (!form.name.trim()) {
    form.name = preset.name
  }

  // Fill notes with provider info if empty
  if (!form.notes.trim() && preset.description) {
    form.notes = preset.description
  }
}

const form = reactive({
  name: '',
  notes: '',
  platform: 'anthropic' as AccountPlatform,
  type: 'oauth' as AccountType, // Will be 'oauth', 'setup-token', or 'apikey'
  credentials: {} as Record<string, unknown>,
  proxy_id: null as number | null,
  concurrency: 10,
  load_factor: null as number | null,
  priority: 1,
  rate_multiplier: 1,
  group_ids: [] as number[],
  expires_at: null as number | null,
  // Custom provider fields
  customProtocol: '',
  customBaseUrl: '',
  customApiKey: '',
  // LightBridge Connect
  lightBridgeConnect: null as any
})

// Helper to check if current type needs OAuth flow
const isOAuthFlow = computed(() => {
  // Antigravity upstream 类型不需要 OAuth 流程
  if (form.platform === 'antigravity' && antigravityAccountType.value === 'upstream') {
    return false
  }
  // Bedrock 类型不需要 OAuth 流程
  if (form.platform === 'anthropic' && accountCategory.value === 'bedrock') {
    return false
  }
  return accountCategory.value === 'oauth-based'
})

// AIStudio 反代流程：Gemini 平台 + API Key + proxy 模式。
// 这是一个独立的两步流程（基础信息 → AI Studio 反代授权），需要保留顶部步骤指示器。
const isAistudioProxyFlow = computed(() =>
  form.platform === 'gemini' && geminiAuthMode.value === 'proxy'
)

// 是否展示顶部的「授权方式 / 第二步」步骤指示器
const showStepIndicator = computed(() => isOAuthFlow.value || isAistudioProxyFlow.value)

// Check if should show LightBridge Connect configuration
const shouldShowLightBridgeConnect = computed(() => {
  if (!isProgressiveFeatureEnabled(ProgressiveFeatures.lightbridgeConnect)) return false
  if (form.platform !== 'custom') return false

  // Check if selected preset supports LightBridge Connect
  if (selectedPreset.value) {
    const preset = findPresetById(selectedPreset.value)
    return preset?.supportsLightBridgeConnect === true
  }

  // Check if Base URL contains known New API patterns
  const url = form.customBaseUrl.toLowerCase()
  return url.includes('new-api') || url.includes('newapi') || url.includes(':3000')
})

// Handle LightBridge Connect verification success
const handleLightBridgeConnectVerified = (result: any) => {
  // Auto-fill account name if empty and username is available
  if (!form.name.trim() && result.username) {
    form.name = `New API - ${result.username}`
  }
}

const isManualInputMethod = computed(() => {
  return oauthFlowRef.value?.inputMethod === 'manual'
})

const expiresAtInput = computed({
  get: () => formatDateTimeLocal(form.expires_at),
  set: (value: string) => {
    form.expires_at = parseDateTimeLocal(value)
  }
})

const canExchangeCode = computed(() => {
  const authCode = oauthFlowRef.value?.authCode || ''
  if (form.platform === 'openai') {
    return authCode.trim() && openaiOAuth.sessionId.value && !openaiOAuth.loading.value
  }
  if (form.platform === 'gemini') {
    return authCode.trim() && geminiOAuth.sessionId.value && !geminiOAuth.loading.value
  }
  if (form.platform === 'grok') {
    return authCode.trim() && grokOAuth.sessionId.value && !grokOAuth.loading.value
  }
  if (form.platform === 'antigravity') {
    return authCode.trim() && antigravityOAuth.sessionId.value && !antigravityOAuth.loading.value
  }
  return authCode.trim() && oauth.sessionId.value && !oauth.loading.value
})

// Watchers
watch(
  () => props.show,
  (newVal) => {
    if (newVal) {
      // Load TLS fingerprint profiles
      adminAPI.tlsFingerprintProfiles.list()
        .then(profiles => { tlsFingerprintProfiles.value = profiles.map(p => ({ id: p.id, name: p.name })) })
        .catch(() => { tlsFingerprintProfiles.value = [] })
      // Modal opened - fill related models
      allowedModels.value = [...getModelsByPlatform(form.platform)]
      // Antigravity: 默认使用映射模式并填充默认映射
      if (form.platform === 'antigravity') {
        antigravityModelRestrictionMode.value = 'mapping'
        fetchAntigravityDefaultMappings().then(mappings => {
          antigravityModelMappings.value = [...mappings]
        })
        antigravityWhitelistModels.value = []
      } else {
        antigravityWhitelistModels.value = []
        antigravityModelMappings.value = []
        antigravityModelRestrictionMode.value = 'mapping'
      }
    } else {
      resetForm()
    }
  }
)

// Sync form.type based on accountCategory, addMethod, and platform-specific type
watch(
  [accountCategory, addMethod, antigravityAccountType, () => form.platform],
  ([category, method, agType]) => {
    // Antigravity upstream 类型（实际创建为 apikey）
    if (form.platform === 'antigravity' && agType === 'upstream') {
      form.type = 'apikey'
      return
    }
    if (form.platform === 'grok') {
      form.type = 'oauth'
      if (category !== 'oauth-based') {
        accountCategory.value = 'oauth-based'
      }
      form.concurrency = 1
      return
    }
    // Bedrock 类型
    if (form.platform === 'anthropic' && category === 'bedrock') {
      form.type = 'bedrock' as AccountType
      return
    }
    if ((form.platform === 'gemini' || form.platform === 'anthropic') && category === 'service_account') {
      form.type = 'service_account' as AccountType
    } else if (category === 'oauth-based') {
      form.type = method as AccountType // 'oauth' or 'setup-token'
    } else {
      form.type = 'apikey'
    }
    // 反代模式仅对 Gemini + APIKey 有效；离开 apikey 类别时复位为官方模式。
    if (form.platform !== 'gemini' || category !== 'apikey') {
      if (geminiAuthMode.value === 'proxy') geminiAuthMode.value = 'official'
    }
  },
  { immediate: true }
)

// Reset platform-specific settings when platform changes
watch(
  () => form.platform,
  (newPlatform) => {
    // Reset base URL based on platform
    apiKeyBaseUrl.value =
      (newPlatform === 'openai')
        ? 'https://api.openai.com'
        : newPlatform === 'gemini'
          ? 'https://generativelanguage.googleapis.com'
          : newPlatform === 'grok'
            ? 'https://api.x.ai'
            : 'https://api.anthropic.com'
    // Clear model-related settings
    allowedModels.value = []
    modelMappings.value = []
    restrictToModelList.value = false
    // Antigravity: 默认使用映射模式并填充默认映射
    if (newPlatform === 'antigravity') {
      antigravityModelRestrictionMode.value = 'mapping'
      fetchAntigravityDefaultMappings().then(mappings => {
        antigravityModelMappings.value = [...mappings]
      })
      antigravityWhitelistModels.value = []
      accountCategory.value = 'oauth-based'
      antigravityAccountType.value = 'oauth'
    } else {
      allowOverages.value = false
      antigravityWhitelistModels.value = []
      antigravityModelMappings.value = []
      antigravityModelRestrictionMode.value = 'mapping'
    }
    if (newPlatform === 'grok') {
      accountCategory.value = 'oauth-based'
      form.type = 'oauth'
      form.concurrency = 1
    }
    if (newPlatform !== 'gemini' && newPlatform !== 'anthropic' && accountCategory.value === 'service_account') {
      accountCategory.value = 'oauth-based'
    }
    if (newPlatform !== 'anthropic' && accountCategory.value === 'bedrock') {
      accountCategory.value = 'oauth-based'
    }
    // Reset Bedrock fields when switching platforms
    bedrockAccessKeyId.value = ''
    bedrockSecretAccessKey.value = ''
    bedrockSessionToken.value = ''
    bedrockRegion.value = 'us-east-1'
    bedrockForceGlobal.value = false
    bedrockAuthMode.value = 'sigv4'
    bedrockApiKeyValue.value = ''
    vertexServiceAccountJson.value = ''
    vertexProjectId.value = ''
    vertexClientEmail.value = ''
    vertexLocation.value = 'global'
    // Reset Anthropic/Antigravity-specific settings when switching to other platforms
    if (newPlatform !== 'anthropic' && newPlatform !== 'antigravity') {
      interceptWarmupRequests.value = false
    }
    if (newPlatform !== 'openai') {
      openaiRelayMode.value = RELAY_MODE_ROUTER
      openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
      openaiOAuthResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
      openaiAPIKeyResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
      codexCLIOnlyEnabled.value = false
      codexCLIOnlyAllowClaudeCodeEnabled.value = false
    }
    if (newPlatform !== 'anthropic') {
      anthropicRelayMode.value = RELAY_MODE_ROUTER
      webSearchEmulationMode.value = 'default'
    }
    if (newPlatform !== 'gemini') {
      geminiRelayMode.value = RELAY_MODE_ROUTER
    }
    if (newPlatform !== 'custom') {
      customRelayMode.value = RELAY_MODE_ROUTER
    }
    // Reset OAuth states
    oauth.resetState()
    openaiOAuth.resetState()

    geminiOAuth.resetState()
    antigravityOAuth.resetState()
    grokOAuth.resetState()
  }
)

// AIStudio 反代模式切换：清空 base_url（待用户填入反代地址）；切回官方时恢复默认地址。
watch(
  geminiAuthMode,
  (mode) => {
    if (form.platform !== 'gemini') return
    if (mode === 'proxy') {
      apiKeyBaseUrl.value = ''
      apiKeyValue.value = ''
    } else {
      apiKeyBaseUrl.value = 'https://generativelanguage.googleapis.com'
    }
  }
)

// Gemini AI Studio OAuth availability (requires operator-configured OAuth client)
watch(
  [accountCategory, () => form.platform],
  ([category, platform]) => {
    if (platform === 'openai' && category !== 'oauth-based') {
      codexCLIOnlyEnabled.value = false
      codexCLIOnlyAllowClaudeCodeEnabled.value = false
    }
    if (platform !== 'anthropic' || category !== 'apikey') {
      anthropicRelayMode.value = RELAY_MODE_ROUTER
      webSearchEmulationMode.value = 'default'
    }
  }
)

watch(
  [() => props.show, () => form.platform, accountCategory],
  async ([show, platform, category]) => {
    if (!show || platform !== 'gemini' || category !== 'oauth-based') {
      geminiAIStudioOAuthEnabled.value = false
      return
    }
    const caps = await geminiOAuth.getCapabilities()
    geminiAIStudioOAuthEnabled.value = !!caps?.ai_studio_oauth_enabled
    if (!geminiAIStudioOAuthEnabled.value && geminiOAuthType.value === 'ai_studio') {
      geminiOAuthType.value = 'code_assist'
    }
  },
  { immediate: true }
)

const handleSelectGeminiOAuthType = (oauthType: 'code_assist' | 'google_one' | 'ai_studio') => {
  if (oauthType === 'ai_studio' && !geminiAIStudioOAuthEnabled.value) {
    appStore.showError(t('admin.accounts.oauth.gemini.aiStudioNotConfigured'))
    return
  }
  geminiOAuthType.value = oauthType
}

// Auto-fill related models when switching to whitelist mode or changing platform
watch(
  [modelRestrictionMode, () => form.platform],
  ([newMode]) => {
    if (newMode === 'whitelist') {
      allowedModels.value = [...getModelsByPlatform(form.platform)]
    }
  }
)

watch(
  [antigravityModelRestrictionMode, () => form.platform],
  ([, platform]) => {
    if (platform !== 'antigravity') return
    // Antigravity 默认不做限制：白名单留空表示允许所有（包含未来新增模型）。
    // 如果需要快速填充常用模型，可在组件内点“填充相关模型”。
  }
)

// Model mapping helpers
const addModelMapping = () => {
  modelMappings.value.push({ from: '', to: '' })
}

const addOpenAICompactModelMapping = () => {
  openAICompactModelMappings.value.push({ from: '', to: '' })
}

const removeOpenAICompactModelMapping = (index: number) => {
  openAICompactModelMappings.value.splice(index, 1)
}

const removeModelMapping = (index: number) => {
  modelMappings.value.splice(index, 1)
}

const addPresetMapping = (from: string, to: string) => {
  if (modelMappings.value.some((m) => m.from === from)) {
    appStore.showInfo(t('admin.accounts.mappingExists', { model: from }))
    return
  }
  modelMappings.value.push({ from, to })
}

const addAntigravityModelMapping = () => {
  antigravityModelMappings.value.push({ from: '', to: '' })
}

const removeAntigravityModelMapping = (index: number) => {
  antigravityModelMappings.value.splice(index, 1)
}

const addAntigravityPresetMapping = (from: string, to: string) => {
  if (antigravityModelMappings.value.some((m) => m.from === from)) {
    appStore.showInfo(t('admin.accounts.mappingExists', { model: from }))
    return
  }
  antigravityModelMappings.value.push({ from, to })
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

const splitTempUnschedKeywords = (value: string) => {
  return value
    .split(/[,;]/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

const needsMixedChannelCheck = (platform: AccountPlatform) => platform === 'antigravity' || platform === 'anthropic'

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
    opts.message || opts.response?.message || t('admin.accounts.failedToCreate')
  mixedChannelWarningAction.value = opts.onConfirm
  showMixedChannelWarning.value = true
}

const withAntigravityConfirmFlag = (payload: CreateAccountRequest): CreateAccountRequest => {
  if (needsMixedChannelCheck(payload.platform) && antigravityMixedChannelConfirmed.value) {
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
  if (!needsMixedChannelCheck(form.platform)) {
    return true
  }
  if (antigravityMixedChannelConfirmed.value) {
    return true
  }

  try {
    const result = await adminAPI.accounts.checkMixedChannelRisk({
      platform: form.platform,
      group_ids: form.group_ids
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
    appStore.showError(error.response?.data?.message || error.response?.data?.detail || t('admin.accounts.failedToCreate'))
    return false
  }
}

const submitCreateAccount = async (payload: CreateAccountRequest) => {
  submitting.value = true
  try {
    const created = await adminAPI.accounts.create(withAntigravityConfirmFlag(payload))
    appStore.showSuccess(t('admin.accounts.accountCreated'))

    // AIStudio 反代（LB 托管）模式：账号创建后立即绑定 Google 会话。
    const isGeminiProxy = form.platform === 'gemini' && geminiAuthMode.value === 'proxy'
    if (isGeminiProxy && created?.id) {
      geminiProxyBinding.value = true
      try {
        if (geminiProxyBindMode.value === 'cookie' && geminiProxyCookie.value.trim()) {
          await aistudioProxyImportCookies(created.id, { cookies: geminiProxyCookie.value.trim() })
          appStore.showSuccess(t('admin.accounts.gemini.proxyCookieBound'))
        } else if (geminiProxyBindMode.value === 'login') {
          // 引导登录：启动有头浏览器会话并轮询到完成。
          const start = await aistudioProxyStartLogin(created.id, form.name)
          const sessionId = start.session_id
          if (sessionId) {
            appStore.showSuccess(t('admin.accounts.gemini.proxyLoginStarted'))
            for (let i = 0; i < 120; i++) {
              await new Promise((r) => setTimeout(r, 3000))
              const st = await aistudioProxyLoginStatus(created.id, sessionId)
              if (st.status === 'completed') {
                appStore.showSuccess(t('admin.accounts.gemini.proxyLoginDone'))
                break
              }
              if (st.status === 'failed') {
                appStore.showError(t('admin.accounts.gemini.proxyLoginFailed') + (st.error || ''))
                break
              }
            }
          }
        }
      } catch (e: any) {
        appStore.showError(t('admin.accounts.gemini.proxyCookieBindFailed') + (e.response?.data?.detail || e.message || ''))
      } finally {
        geminiProxyBinding.value = false
      }
    }

    emit('created')
    handleClose()
  } catch (error: any) {
    if (error.response?.status === 409 && error.response?.data?.error === 'mixed_channel_warning' && needsMixedChannelCheck(form.platform)) {
      openMixedChannelDialog({
        message: error.response?.data?.message,
        onConfirm: async () => {
          antigravityMixedChannelConfirmed.value = true
          await submitCreateAccount(payload)
        }
      })
      return
    }
    appStore.showError(error.response?.data?.message || error.response?.data?.detail || t('admin.accounts.failedToCreate'))
  } finally {
    submitting.value = false
  }
}

// Methods
const resetForm = () => {
  step.value = 1
  form.name = ''
  form.notes = ''
  form.platform = 'anthropic'
  form.type = 'oauth'
  form.credentials = {}
  form.proxy_id = null
  form.concurrency = 10
  form.load_factor = null
  form.priority = 1
  form.rate_multiplier = 1
  form.group_ids = []
  form.expires_at = null
  accountCategory.value = 'oauth-based'
  addMethod.value = 'oauth'
  apiKeyBaseUrl.value = 'https://api.anthropic.com'
  apiKeyValue.value = ''
  editQuotaLimit.value = null
  editQuotaDailyLimit.value = null
  editQuotaWeeklyLimit.value = null
  editDailyResetMode.value = null
  editDailyResetHour.value = null
  editWeeklyResetMode.value = null
  editWeeklyResetDay.value = null
  editWeeklyResetHour.value = null
  editResetTimezone.value = null
  modelMappings.value = []
  openAICompactModelMappings.value = []
  modelRestrictionMode.value = 'whitelist'
  allowedModels.value = [...claudeModels] // Default fill related models
  restrictToModelList.value = false

  antigravityModelRestrictionMode.value = 'mapping'
  antigravityWhitelistModels.value = []
  fetchAntigravityDefaultMappings().then(mappings => {
    antigravityModelMappings.value = [...mappings]
  })
  poolModeEnabled.value = false
  poolModeRetryCount.value = DEFAULT_POOL_MODE_RETRY_COUNT
  poolModeRetryStatusCodesInput.value = ''
  customErrorCodesEnabled.value = false
  selectedErrorCodes.value = []
  customErrorCodeInput.value = null
  interceptWarmupRequests.value = false
  autoPauseOnExpired.value = true
  openaiRelayMode.value = RELAY_MODE_ROUTER
  openAICompactMode.value = 'auto'
  openAIResponsesMode.value = 'auto'
  openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
  openaiOAuthResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
  openaiAPIKeyResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
  codexCLIOnlyEnabled.value = false
  codexCLIOnlyAllowClaudeCodeEnabled.value = false
  anthropicRelayMode.value = RELAY_MODE_ROUTER
  geminiRelayMode.value = RELAY_MODE_ROUTER
  customRelayMode.value = RELAY_MODE_ROUTER
  webSearchEmulationMode.value = 'default'
  // Reset quota control state
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
  allowOverages.value = false
  antigravityAccountType.value = 'oauth'
  upstreamBaseUrl.value = ''
  upstreamApiKey.value = ''
  vertexServiceAccountJson.value = ''
  vertexProjectId.value = ''
  vertexClientEmail.value = ''
  vertexLocation.value = 'global'
  tempUnschedEnabled.value = false
  tempUnschedRules.value = []
  geminiOAuthType.value = 'code_assist'
  geminiTierGoogleOne.value = 'google_one_free'
  geminiTierGcp.value = 'gcp_standard'
  geminiTierAIStudio.value = 'aistudio_free'
  geminiAuthMode.value = 'official'
  geminiProxyCookie.value = ''
  geminiProxyBinding.value = false
  geminiProxyBindMode.value = 'cookie'
  proxyRuntimeStatus.value = null
  proxyRuntimeChecking.value = false
  proxyRuntimeInstalling.value = false
  proxyRuntimeLogs.value = []
  oauth.resetState()
  openaiOAuth.resetState()
  geminiOAuth.resetState()
  antigravityOAuth.resetState()
  grokOAuth.resetState()
  oauthFlowRef.value?.reset()
  antigravityMixedChannelConfirmed.value = false
  clearMixedChannelDialog()
}

const handleClose = () => {
  antigravityMixedChannelConfirmed.value = false
  clearMixedChannelDialog()
  emit('close')
}

const buildOpenAIExtra = (base?: Record<string, unknown>): Record<string, unknown> | undefined => {
  if (form.platform !== 'openai') {
    return base
  }

  const extra: Record<string, unknown> = { ...(base || {}) }
  if (accountCategory.value === 'oauth-based') {
    extra.openai_oauth_responses_websockets_v2_mode = openaiOAuthResponsesWebSocketV2Mode.value
    extra.openai_oauth_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(openaiOAuthResponsesWebSocketV2Mode.value)
  } else if (accountCategory.value === 'apikey') {
    extra.openai_apikey_responses_websockets_v2_mode = openaiAPIKeyResponsesWebSocketV2Mode.value
    extra.openai_apikey_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(openaiAPIKeyResponsesWebSocketV2Mode.value)
  }
  // 清理兼容旧键，统一改用分类型开关。
  delete extra.responses_websockets_v2_enabled
  delete extra.openai_ws_enabled
  writeRelayModeToExtra(extra, openaiRelayMode.value)

  if (accountCategory.value === 'oauth-based' && codexCLIOnlyEnabled.value) {
    extra.codex_cli_only = true
  } else {
    delete extra.codex_cli_only
  }
  if (
    accountCategory.value === 'oauth-based' &&
    codexCLIOnlyEnabled.value &&
    codexCLIOnlyAllowClaudeCodeEnabled.value
  ) {
    extra.codex_cli_only_allowed_clients = ['claude_code']
  } else {
    delete extra.codex_cli_only_allowed_clients
  }
  if (openAICompactMode.value !== 'auto') {
    extra.openai_compact_mode = openAICompactMode.value
  } else {
    delete extra.openai_compact_mode
  }

  if (
    accountCategory.value === 'apikey' &&
    openAITextGenerationCapabilityEnabled.value &&
    openAIResponsesMode.value !== 'auto'
  ) {
    extra.openai_responses_mode = openAIResponsesMode.value
  } else {
    delete extra.openai_responses_mode
  }

  return Object.keys(extra).length > 0 ? extra : undefined
}

const buildAnthropicExtra = (base?: Record<string, unknown>): Record<string, unknown> | undefined => {
  if (form.platform !== 'anthropic' || accountCategory.value !== 'apikey') {
    return base
  }

  const extra: Record<string, unknown> = { ...(base || {}) }
  writeRelayModeToExtra(extra, anthropicRelayMode.value)
  if (webSearchEmulationMode.value === 'default') {
    delete extra.web_search_emulation
  } else {
    extra.web_search_emulation = webSearchEmulationMode.value
  }

  return Object.keys(extra).length > 0 ? extra : undefined
}

const buildGeminiExtra = (base?: Record<string, unknown>): Record<string, unknown> | undefined => {
  if (form.platform !== 'gemini' || accountCategory.value === 'service_account') {
    return base
  }

  const extra: Record<string, unknown> = { ...(base || {}) }
  writeRelayModeToExtra(extra, geminiRelayMode.value)
  return Object.keys(extra).length > 0 ? extra : undefined
}

// Helper function to create account with mixed channel warning handling
const doCreateAccount = async (payload: CreateAccountRequest) => {
  const canContinue = await ensureAntigravityMixedChannelConfirmed(async () => {
    await submitCreateAccount(payload)
  })
  if (!canContinue) {
    return
  }
  await submitCreateAccount(payload)
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

const applyVertexServiceAccountJson = (value: string) => {
  const raw = value.trim()
  if (!raw) {
    vertexProjectId.value = ''
    vertexClientEmail.value = ''
    return false
  }
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>
    const projectId = typeof parsed.project_id === 'string' ? parsed.project_id.trim() : ''
    const clientEmail = typeof parsed.client_email === 'string' ? parsed.client_email.trim() : ''
    const privateKey = typeof parsed.private_key === 'string' ? parsed.private_key.trim() : ''
    if (!projectId || !clientEmail || !privateKey) {
      appStore.showError(t('admin.accounts.vertexSaJsonMissingFields'))
      return false
    }
    vertexProjectId.value = projectId
    vertexClientEmail.value = clientEmail
    vertexServiceAccountJson.value = JSON.stringify(parsed)
    return true
  } catch {
    appStore.showError(t('admin.accounts.vertexSaJsonInvalid'))
    return false
  }
}

const parseVertexServiceAccountJson = () => applyVertexServiceAccountJson(vertexServiceAccountJson.value)

const handleVertexServiceAccountFile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  try {
    applyVertexServiceAccountJson(await file.text())
  } finally {
    input.value = ''
  }
}

const handleVertexServiceAccountDrop = async (event: DragEvent) => {
  vertexServiceAccountDragActive.value = false
  const file = event.dataTransfer?.files?.[0]
  if (!file) return
  applyVertexServiceAccountJson(await file.text())
}

const handleSubmit = async () => {
  // For Custom provider, create directly
  if (form.platform === 'custom') {
    if (!form.name.trim()) {
      appStore.showError(t('admin.accounts.pleaseEnterAccountName'))
      return
    }
    if (!form.customProtocol) {
      appStore.showError(t('admin.accounts.custom.pleaseSelectProtocol'))
      return
    }
    if (!form.customBaseUrl.trim()) {
      appStore.showError(t('admin.accounts.custom.pleaseEnterBaseUrl'))
      return
    }
    if (!form.customApiKey.trim()) {
      appStore.showError(t('admin.accounts.custom.pleaseEnterApiKey'))
      return
    }

    const credentials: Record<string, unknown> = {
      base_url: form.customBaseUrl.trim(),
      api_key: form.customApiKey.trim()
    }

    const extra: Record<string, unknown> = {
      protocol: form.customProtocol
    }

    writeRelayModeToExtra(extra, customRelayMode.value)

    await createAccountAndFinish('custom', 'apikey', credentials, extra)
    return
  }

  // For OAuth-based type, handle OAuth flow (goes to step 2)
  if (isOAuthFlow.value) {
    if (!form.name.trim()) {
      appStore.showError(t('admin.accounts.pleaseEnterAccountName'))
      return
    }
    const canContinue = await ensureAntigravityMixedChannelConfirmed(async () => {
      step.value = 2
    })
    if (!canContinue) {
      return
    }
    step.value = 2
    return
  }

  // For Bedrock type, create directly
  if (form.platform === 'anthropic' && accountCategory.value === 'bedrock') {
    if (!form.name.trim()) {
      appStore.showError(t('admin.accounts.pleaseEnterAccountName'))
      return
    }

    const credentials: Record<string, unknown> = {
      auth_mode: bedrockAuthMode.value,
      aws_region: bedrockRegion.value.trim() || 'us-east-1',
    }

    if (bedrockAuthMode.value === 'sigv4') {
      if (!bedrockAccessKeyId.value.trim()) {
        appStore.showError(t('admin.accounts.bedrockAccessKeyIdRequired'))
        return
      }
      if (!bedrockSecretAccessKey.value.trim()) {
        appStore.showError(t('admin.accounts.bedrockSecretAccessKeyRequired'))
        return
      }
      credentials.aws_access_key_id = bedrockAccessKeyId.value.trim()
      credentials.aws_secret_access_key = bedrockSecretAccessKey.value.trim()
      if (bedrockSessionToken.value.trim()) {
        credentials.aws_session_token = bedrockSessionToken.value.trim()
      }
    } else {
      if (!bedrockApiKeyValue.value.trim()) {
        appStore.showError(t('admin.accounts.bedrockApiKeyRequired'))
        return
      }
      credentials.api_key = bedrockApiKeyValue.value.trim()
    }

    if (bedrockForceGlobal.value) {
      credentials.aws_force_global = 'true'
    }

    // Model mapping
    const modelMapping = buildAdvancedModelMapping()
    if (modelMapping) {
      credentials.model_mapping = modelMapping
    }

    // Pool mode
    if (poolModeEnabled.value) {
      credentials.pool_mode = true
      credentials.pool_mode_retry_count = normalizePoolModeRetryCount(poolModeRetryCount.value)
      const parsedRetryStatusCodes = parsePoolModeRetryStatusCodes(poolModeRetryStatusCodesInput.value)
      if (parsedRetryStatusCodes.length > 0) {
        credentials.pool_mode_retry_status_codes = parsedRetryStatusCodes
      }
    }

    applyInterceptWarmup(credentials, interceptWarmupRequests.value, 'create')

    await createAccountAndFinish('anthropic', 'bedrock' as AccountType, credentials)
    return
  }

  // For Antigravity upstream type, create directly
  if (form.platform === 'antigravity' && antigravityAccountType.value === 'upstream') {
    if (!form.name.trim()) {
      appStore.showError(t('admin.accounts.pleaseEnterAccountName'))
      return
    }
    if (!upstreamBaseUrl.value.trim()) {
      appStore.showError(t('admin.accounts.upstream.pleaseEnterBaseUrl'))
      return
    }
    if (!upstreamApiKey.value.trim()) {
      appStore.showError(t('admin.accounts.upstream.pleaseEnterApiKey'))
      return
    }

    // Build upstream credentials (and optional model restriction)
    const credentials: Record<string, unknown> = {
      base_url: upstreamBaseUrl.value.trim(),
      api_key: upstreamApiKey.value.trim()
    }

    // Antigravity 只使用映射模式
    const antigravityModelMapping = buildModelMappingObject(
      'mapping',
      [],
      antigravityModelMappings.value
    )
    if (antigravityModelMapping) {
      credentials.model_mapping = antigravityModelMapping
    }

    applyInterceptWarmup(credentials, interceptWarmupRequests.value, 'create')

    const extra = buildAntigravityExtra()
    await createAccountAndFinish(form.platform, 'apikey', credentials, extra)
    return
  }

  if ((form.platform === 'gemini' || form.platform === 'anthropic') && accountCategory.value === 'service_account') {
    if (!form.name.trim()) {
      appStore.showError(t('admin.accounts.pleaseEnterAccountName'))
      return
    }
    if (!parseVertexServiceAccountJson()) {
      return
    }
    if (!vertexLocation.value.trim()) {
      appStore.showError(t('admin.accounts.vertexLocationRequired'))
      return
    }
    const credentials: Record<string, unknown> = {
      service_account_json: vertexServiceAccountJson.value.trim(),
      project_id: vertexProjectId.value.trim(),
      client_email: vertexClientEmail.value.trim(),
      location: vertexLocation.value.trim(),
      tier_id: 'vertex'
    }
    await createAccountAndFinish(form.platform, 'service_account' as AccountType, credentials)
    return
  }

  // For apikey type, create directly. 反代（LB 托管）模式不需要用户填 API Key/token。
  const isGeminiProxy = form.platform === 'gemini' && geminiAuthMode.value === 'proxy'
  if (!isGeminiProxy && !apiKeyValue.value.trim()) {
    appStore.showError(t('admin.accounts.pleaseEnterApiKey'))
    return
  }

  // Determine default base URL based on platform
  const defaultBaseUrl =
    form.platform === 'openai'
      ? 'https://api.openai.com'
      : form.platform === 'gemini'
        ? 'https://generativelanguage.googleapis.com'
        : 'https://api.anthropic.com'

  // Build credentials with optional model mapping
  const credentials: Record<string, unknown> = {}
  if (isGeminiProxy) {
    // 反代（LB 托管）：base_url/api_key 由后端 Manager 启动子进程时生成并写入，
    // 这里只埋一个路由标记 auth_header=bearer 和占位 api_key。
    credentials.auth_header = 'bearer'
    credentials.api_key = 'managed-by-lightbridge'
  } else {
    credentials.base_url = apiKeyBaseUrl.value.trim() || defaultBaseUrl
    credentials.api_key = apiKeyValue.value.trim()
  }
  if (form.platform === 'gemini') {
    if (isGeminiProxy) {
      // already set auth_header above; 反代无 tier
    } else {
      credentials.tier_id = geminiTierAIStudio.value
    }
  }

  // Add model mapping if configured（OpenAI 开启自动透传时不应用）
  if (!isOpenAIModelRestrictionDisabled.value) {
    const modelMapping = buildAdvancedModelMapping()
    if (modelMapping) {
      credentials.model_mapping = modelMapping
    }
  }
  if (form.platform === 'openai') {
    applyOpenAIEndpointCapabilities(credentials)
    const compactModelMapping = buildOpenAICompactModelMapping()
    if (compactModelMapping) {
      credentials.compact_model_mapping = compactModelMapping
    }
  }

  // Add pool mode if enabled
  if (poolModeEnabled.value) {
    credentials.pool_mode = true
    credentials.pool_mode_retry_count = normalizePoolModeRetryCount(poolModeRetryCount.value)
    const parsedRetryStatusCodes = parsePoolModeRetryStatusCodes(poolModeRetryStatusCodesInput.value)
    if (parsedRetryStatusCodes.length > 0) {
      credentials.pool_mode_retry_status_codes = parsedRetryStatusCodes
    }
  }

  // Add custom error codes if enabled
  if (customErrorCodesEnabled.value) {
    credentials.custom_error_codes_enabled = true
    credentials.custom_error_codes = [...selectedErrorCodes.value]
  }

  applyInterceptWarmup(credentials, interceptWarmupRequests.value, 'create')
  if (!applyTempUnschedConfig(credentials)) {
    return
  }

  form.credentials = credentials
  const extra = buildModelListExtra(buildGeminiExtra(buildAnthropicExtra(buildOpenAIExtra())))

  await doCreateAccount({
    ...form,
    group_ids: form.group_ids,
    extra,
    auto_pause_on_expired: autoPauseOnExpired.value
  })
}

const goBackToBasicInfo = () => {
  step.value = 1
  oauth.resetState()
  openaiOAuth.resetState()
  geminiOAuth.resetState()
  antigravityOAuth.resetState()
  grokOAuth.resetState()
  oauthFlowRef.value?.reset()
}

const handleGenerateUrl = async () => {
  if (form.platform === 'openai') {
    await openaiOAuth.generateAuthUrl(form.proxy_id)
  } else if (form.platform === 'gemini') {
    await geminiOAuth.generateAuthUrl(
      form.proxy_id,
      oauthFlowRef.value?.projectId,
      geminiOAuthType.value,
      geminiSelectedTier.value
    )
  } else if (form.platform === 'antigravity') {
    await antigravityOAuth.generateAuthUrl(form.proxy_id)
  } else if (form.platform === 'grok') {
    await grokOAuth.generateAuthUrl(form.proxy_id)
  } else {
    await oauth.generateAuthUrl(addMethod.value, form.proxy_id)
  }
}

const handleValidateRefreshToken = (rt: string) => {
  if (form.platform === 'openai') {
    handleOpenAIValidateRT(rt)
  } else if (form.platform === 'antigravity') {
    handleAntigravityValidateRT(rt)
  } else if (form.platform === 'grok') {
    handleGrokValidateRT(rt)
  }
}

const handleValidateSessionToken = (_sessionToken: string) => {
  // Session token validation removed
}

const formatDateTimeLocal = formatDateTimeLocalInput
const parseDateTimeLocal = parseDateTimeLocalInput

// Create account and handle success/failure
const createAccountAndFinish = async (
  platform: AccountPlatform,
  type: AccountType,
  credentials: Record<string, unknown>,
  extra?: Record<string, unknown>
) => {
  if (!applyTempUnschedConfig(credentials)) {
    return
  }
  // Inject quota limits for apikey/bedrock accounts
  let finalExtra = extra
  if (type === 'apikey' || type === 'bedrock') {
    const quotaExtra: Record<string, unknown> = { ...(extra || {}) }
    if (editQuotaLimit.value != null && editQuotaLimit.value > 0) {
      quotaExtra.quota_limit = editQuotaLimit.value
    }
    if (editQuotaDailyLimit.value != null && editQuotaDailyLimit.value > 0) {
      quotaExtra.quota_daily_limit = editQuotaDailyLimit.value
    }
    if (editQuotaWeeklyLimit.value != null && editQuotaWeeklyLimit.value > 0) {
      quotaExtra.quota_weekly_limit = editQuotaWeeklyLimit.value
    }
    // Quota reset mode config
    if (editDailyResetMode.value === 'fixed') {
      quotaExtra.quota_daily_reset_mode = 'fixed'
      quotaExtra.quota_daily_reset_hour = editDailyResetHour.value ?? 0
    }
    if (editWeeklyResetMode.value === 'fixed') {
      quotaExtra.quota_weekly_reset_mode = 'fixed'
      quotaExtra.quota_weekly_reset_day = editWeeklyResetDay.value ?? 1
      quotaExtra.quota_weekly_reset_hour = editWeeklyResetHour.value ?? 0
    }
    if (editDailyResetMode.value === 'fixed' || editWeeklyResetMode.value === 'fixed') {
      quotaExtra.quota_reset_timezone = editResetTimezone.value || 'UTC'
    }
    // Quota notify config
    writeQuotaNotifyToExtra(quotaExtra, 'create')
    if (Object.keys(quotaExtra).length > 0) {
      finalExtra = quotaExtra
    }
  }
  if (platform === 'openai') {
    if (type === 'apikey') {
      applyOpenAIEndpointCapabilities(credentials)
    }
    const compactModelMapping = buildOpenAICompactModelMapping()
    if (compactModelMapping) {
      credentials.compact_model_mapping = compactModelMapping
    } else {
      delete credentials.compact_model_mapping
    }
  }
  finalExtra = buildModelListExtra(finalExtra)
  const concurrency = platform === 'grok' ? 1 : form.concurrency
  await doCreateAccount({
    name: form.name,
    notes: form.notes,
    platform,
    type,
    credentials,
    extra: finalExtra,
    proxy_id: form.proxy_id,
    concurrency,
    load_factor: form.load_factor ?? undefined,
    priority: form.priority,
    rate_multiplier: form.rate_multiplier,
    group_ids: form.group_ids,
    expires_at: form.expires_at,
    auto_pause_on_expired: autoPauseOnExpired.value
  })
}

// OpenAI OAuth 授权码兑换
const handleOpenAIExchange = async (authCode: string) => {
  const oauthClient = openaiOAuth
  if (!authCode.trim() || !oauthClient.sessionId.value) return

  oauthClient.loading.value = true
  oauthClient.error.value = ''

  try {
    const stateToUse = (oauthFlowRef.value?.oauthState || oauthClient.oauthState.value || '').trim()
    if (!stateToUse) {
      oauthClient.error.value = t('admin.accounts.oauth.authFailed')
      appStore.showError(oauthClient.error.value)
      return
    }

    const tokenInfo = await oauthClient.exchangeAuthCode(
      authCode.trim(),
      oauthClient.sessionId.value,
      stateToUse,
      form.proxy_id
    )
    if (!tokenInfo) return

    const credentials = oauthClient.buildCredentials(tokenInfo)
    const oauthExtra = oauthClient.buildExtraInfo(tokenInfo) as Record<string, unknown> | undefined
    const extra = buildModelListExtra(buildOpenAIExtra(oauthExtra))
    const shouldCreateOpenAI = form.platform === 'openai'

    // Add model mapping for OpenAI OAuth accounts（透传模式下不应用）
    if (shouldCreateOpenAI && !isOpenAIModelRestrictionDisabled.value) {
      const modelMapping = buildAdvancedModelMapping()
      if (modelMapping) {
        credentials.model_mapping = modelMapping
      }
    }
    if (shouldCreateOpenAI) {
      const compactModelMapping = buildOpenAICompactModelMapping()
      if (compactModelMapping) {
        credentials.compact_model_mapping = compactModelMapping
      }
    }

    // 应用临时不可调度配置
    if (!applyTempUnschedConfig(credentials)) {
      return
    }

    if (shouldCreateOpenAI) {
      await adminAPI.accounts.create({
        name: form.name,
        notes: form.notes,
        platform: 'openai',
        type: 'oauth',
        credentials,
        extra,
        proxy_id: form.proxy_id,
        concurrency: form.concurrency,
        load_factor: form.load_factor ?? undefined,
        priority: form.priority,
        rate_multiplier: form.rate_multiplier,
        group_ids: form.group_ids,
        expires_at: form.expires_at,
        auto_pause_on_expired: autoPauseOnExpired.value
      })
      appStore.showSuccess(t('admin.accounts.accountCreated'))
    }

    emit('created')
    handleClose()
  } catch (error: any) {
    oauthClient.error.value = error.response?.data?.detail || t('admin.accounts.oauth.authFailed')
    appStore.showError(oauthClient.error.value)
  } finally {
    oauthClient.loading.value = false
  }
}

// OpenAI 手动 RT 批量验证和创建
// OpenAI Mobile RT client_id
const OPENAI_MOBILE_RT_CLIENT_ID = 'app_LlGpXReQgckcGGUo2JrYvtJK'

const buildOpenAICodexImportCredentialExtras = (): Record<string, unknown> | null => {
  const credentials: Record<string, unknown> = {}
  if (!isOpenAIModelRestrictionDisabled.value) {
    const modelMapping = buildAdvancedModelMapping()
    if (modelMapping) {
      credentials.model_mapping = modelMapping
    }
  }

  const compactModelMapping = buildOpenAICompactModelMapping()
  if (compactModelMapping) {
    credentials.compact_model_mapping = compactModelMapping
  }

  if (!applyTempUnschedConfig(credentials)) {
    return null
  }
  return credentials
}

const formatCodexImportMessages = (messages?: CodexSessionImportMessage[]) => {
  return (messages || [])
    .map((item) => {
      const name = item.name ? ` ${item.name}` : ''
      return `#${item.index}${name}: ${item.message}`
    })
    .join('\n')
}

const handleOpenAIImportCodexSession = async (content: string) => {
  const oauthClient = openaiOAuth
  const trimmed = content.trim()
  if (!trimmed) {
    oauthClient.error.value = t('admin.accounts.oauth.openai.codexSessionEmpty')
    return
  }

  const credentialExtras = buildOpenAICodexImportCredentialExtras()
  if (credentialExtras === null) {
    return
  }

  oauthClient.loading.value = true
  oauthClient.error.value = ''

  try {
    const extra = buildModelListExtra(buildOpenAIExtra())
    const result = await adminAPI.accounts.importCodexSession({
      content: trimmed,
      name: form.name,
      notes: form.notes || null,
      proxy_id: form.proxy_id,
      concurrency: form.concurrency,
      load_factor: form.load_factor ?? undefined,
      priority: form.priority,
      rate_multiplier: form.rate_multiplier,
      group_ids: form.group_ids,
      expires_at: form.expires_at,
      auto_pause_on_expired: autoPauseOnExpired.value,
      credential_extras: Object.keys(credentialExtras).length > 0 ? credentialExtras : undefined,
      extra,
      update_existing: true
    })

    const successCount = result.created + result.updated
    const params = {
      created: result.created,
      updated: result.updated,
      skipped: result.skipped,
      failed: result.failed
    }

    if (successCount > 0 && result.failed === 0) {
      appStore.showSuccess(t('admin.accounts.oauth.openai.codexSessionImportSuccess', params))
      emit('created')
      handleClose()
      return
    }

    const errorText = formatCodexImportMessages(result.errors)
    const warningText = formatCodexImportMessages(result.warnings)
    oauthClient.error.value = [errorText, warningText].filter(Boolean).join('\n')

    if (result.failed === 0) {
      appStore.showWarning(t('admin.accounts.oauth.openai.codexSessionImportSuccess', params))
      return
    }

    if (successCount > 0) {
      appStore.showWarning(t('admin.accounts.oauth.openai.codexSessionImportPartial', params))
      emit('created')
      return
    }

    appStore.showError(t('admin.accounts.oauth.openai.codexSessionImportFailed'))
  } catch (error: any) {
    oauthClient.error.value =
      error.response?.data?.detail ||
      error.response?.data?.message ||
      error.message ||
      t('admin.accounts.oauth.openai.codexSessionImportFailed')
    appStore.showError(oauthClient.error.value)
  } finally {
    oauthClient.loading.value = false
  }
}

// OpenAI RT 批量验证和创建（共享逻辑）
const handleOpenAIBatchRT = async (refreshTokenInput: string, clientId?: string) => {
  const oauthClient = openaiOAuth
  if (!refreshTokenInput.trim()) return

  const refreshTokens = refreshTokenInput
    .split('\n')
    .map((rt) => rt.trim())
    .filter((rt) => rt)

  if (refreshTokens.length === 0) {
    oauthClient.error.value = t('admin.accounts.oauth.openai.pleaseEnterRefreshToken')
    return
  }

  oauthClient.loading.value = true
  oauthClient.error.value = ''

  let successCount = 0
  let failedCount = 0
  const errors: string[] = []
  const shouldCreateOpenAI = form.platform === 'openai'

  try {
    for (let i = 0; i < refreshTokens.length; i++) {
      try {
        const tokenInfo = await oauthClient.validateRefreshToken(
          refreshTokens[i],
          form.proxy_id,
          clientId
        )
        if (!tokenInfo) {
          failedCount++
          errors.push(`#${i + 1}: ${oauthClient.error.value || 'Validation failed'}`)
          oauthClient.error.value = ''
          continue
        }

        const credentials = oauthClient.buildCredentials(tokenInfo)
        if (clientId) {
          credentials.client_id = clientId
        }
        const oauthExtra = oauthClient.buildExtraInfo(tokenInfo) as Record<string, unknown> | undefined
        const extra = buildModelListExtra(buildOpenAIExtra(oauthExtra))

        // Add model mapping for OpenAI OAuth accounts（透传模式下不应用）
        if (shouldCreateOpenAI && !isOpenAIModelRestrictionDisabled.value) {
          const modelMapping = buildAdvancedModelMapping()
          if (modelMapping) {
            credentials.model_mapping = modelMapping
          }
        }
        if (shouldCreateOpenAI) {
          const compactModelMapping = buildOpenAICompactModelMapping()
          if (compactModelMapping) {
            credentials.compact_model_mapping = compactModelMapping
          }
        }

        // Generate account name; fallback to email if name is empty (ent schema requires NotEmpty)
        const baseName = form.name || tokenInfo.email || 'OpenAI OAuth Account'
        const accountName = refreshTokens.length > 1 ? `${baseName} #${i + 1}` : baseName

        if (shouldCreateOpenAI) {
          await adminAPI.accounts.create({
            name: accountName,
            notes: form.notes,
            platform: 'openai',
            type: 'oauth',
            credentials,
            extra,
            proxy_id: form.proxy_id,
            concurrency: form.concurrency,
            load_factor: form.load_factor ?? undefined,
            priority: form.priority,
            rate_multiplier: form.rate_multiplier,
            group_ids: form.group_ids,
            expires_at: form.expires_at,
            auto_pause_on_expired: autoPauseOnExpired.value
          })
        }

        successCount++
      } catch (error: any) {
        failedCount++
        const errMsg = error.response?.data?.detail || error.message || 'Unknown error'
        errors.push(`#${i + 1}: ${errMsg}`)
      }
    }

    // Show results
    if (successCount > 0 && failedCount === 0) {
      appStore.showSuccess(
        refreshTokens.length > 1
          ? t('admin.accounts.oauth.batchSuccess', { count: successCount })
          : t('admin.accounts.accountCreated')
      )
      emit('created')
      handleClose()
    } else if (successCount > 0 && failedCount > 0) {
      appStore.showWarning(
        t('admin.accounts.oauth.batchPartialSuccess', { success: successCount, failed: failedCount })
      )
      oauthClient.error.value = errors.join('\n')
      emit('created')
    } else {
      oauthClient.error.value = errors.join('\n')
      appStore.showError(t('admin.accounts.oauth.batchFailed'))
    }
  } finally {
    oauthClient.loading.value = false
  }
}

// 手动输入 RT（Codex CLI client_id，默认）
const handleOpenAIValidateRT = (rt: string) => handleOpenAIBatchRT(rt)

// 手动输入 Mobile RT
const handleOpenAIValidateMobileRT = (rt: string) => handleOpenAIBatchRT(rt, OPENAI_MOBILE_RT_CLIENT_ID)

// Antigravity 手动 RT 批量验证和创建
const handleAntigravityValidateRT = async (refreshTokenInput: string) => {
  if (!refreshTokenInput.trim()) return

  // Parse multiple refresh tokens (one per line)
  const refreshTokens = refreshTokenInput
    .split('\n')
    .map((rt) => rt.trim())
    .filter((rt) => rt)

  if (refreshTokens.length === 0) {
    antigravityOAuth.error.value = t('admin.accounts.oauth.antigravity.pleaseEnterRefreshToken')
    return
  }

  antigravityOAuth.loading.value = true
  antigravityOAuth.error.value = ''

  let successCount = 0
  let failedCount = 0
  const errors: string[] = []

  try {
    for (let i = 0; i < refreshTokens.length; i++) {
      try {
        const tokenInfo = await antigravityOAuth.validateRefreshToken(
          refreshTokens[i],
          form.proxy_id
        )
        if (!tokenInfo) {
          failedCount++
          errors.push(`#${i + 1}: ${antigravityOAuth.error.value || 'Validation failed'}`)
          antigravityOAuth.error.value = ''
          continue
        }

        const credentials = antigravityOAuth.buildCredentials(tokenInfo)
        
        // Generate account name with index for batch
        const accountName = refreshTokens.length > 1 ? `${form.name} #${i + 1}` : form.name

        // Note: Antigravity doesn't have buildExtraInfo, so we pass empty extra or rely on credentials
        const createPayload = withAntigravityConfirmFlag({
          name: accountName,
          notes: form.notes,
          platform: 'antigravity',
          type: 'oauth',
          credentials,
          extra: {},
          proxy_id: form.proxy_id,
          concurrency: form.concurrency,
          load_factor: form.load_factor ?? undefined,
          priority: form.priority,
          rate_multiplier: form.rate_multiplier,
          group_ids: form.group_ids,
          expires_at: form.expires_at,
          auto_pause_on_expired: autoPauseOnExpired.value
        })
        await adminAPI.accounts.create(createPayload)
        successCount++
      } catch (error: any) {
        failedCount++
        const errMsg = error.response?.data?.detail || error.message || 'Unknown error'
        errors.push(`#${i + 1}: ${errMsg}`)
      }
    }

    // Show results
    if (successCount > 0 && failedCount === 0) {
      appStore.showSuccess(
        refreshTokens.length > 1
          ? t('admin.accounts.oauth.batchSuccess', { count: successCount })
          : t('admin.accounts.accountCreated')
      )
      emit('created')
      handleClose()
    } else if (successCount > 0 && failedCount > 0) {
      appStore.showWarning(
        t('admin.accounts.oauth.batchPartialSuccess', { success: successCount, failed: failedCount })
      )
      antigravityOAuth.error.value = errors.join('\n')
      emit('created')
    } else {
      antigravityOAuth.error.value = errors.join('\n')
      appStore.showError(t('admin.accounts.oauth.batchFailed'))
    }
  } finally {
    antigravityOAuth.loading.value = false
  }
}

// Grok 手动 RT 批量验证和创建
const handleGrokValidateRT = async (refreshTokenInput: string) => {
  if (!refreshTokenInput.trim()) return

  const refreshTokens = refreshTokenInput
    .split('\n')
    .map((rt) => rt.trim())
    .filter((rt) => rt)

  if (refreshTokens.length === 0) {
    grokOAuth.error.value = t('admin.accounts.oauth.grok.pleaseEnterRefreshToken')
    return
  }

  grokOAuth.loading.value = true
  grokOAuth.error.value = ''

  let successCount = 0
  let failedCount = 0
  const errors: string[] = []

  try {
    for (let i = 0; i < refreshTokens.length; i++) {
      try {
        const tokenInfo = await grokOAuth.validateRefreshToken(refreshTokens[i], form.proxy_id)
        if (!tokenInfo) {
          failedCount++
          errors.push(`#${i + 1}: ${grokOAuth.error.value || 'Validation failed'}`)
          grokOAuth.error.value = ''
          continue
        }

        const credentials = grokOAuth.buildCredentials(tokenInfo)
        const modelMapping = buildAdvancedModelMapping()
        if (modelMapping) {
          credentials.model_mapping = modelMapping
        }
        const extra = buildModelListExtra(grokOAuth.buildExtraInfo(tokenInfo))
        const baseName = form.name || tokenInfo.email || tokenInfo.name || 'Grok OAuth Account'
        const accountName = refreshTokens.length > 1 ? `${baseName} #${i + 1}` : baseName

        await adminAPI.accounts.create({
          name: accountName,
          notes: form.notes,
          platform: 'grok',
          type: 'oauth',
          credentials,
          extra,
          proxy_id: form.proxy_id,
          concurrency: 1,
          load_factor: form.load_factor ?? undefined,
          priority: form.priority,
          rate_multiplier: form.rate_multiplier,
          group_ids: form.group_ids,
          expires_at: form.expires_at,
          auto_pause_on_expired: autoPauseOnExpired.value
        })

        successCount++
      } catch (error: any) {
        failedCount++
        const errMsg = error.response?.data?.detail || error.message || 'Unknown error'
        errors.push(`#${i + 1}: ${errMsg}`)
      }
    }

    if (successCount > 0 && failedCount === 0) {
      appStore.showSuccess(
        refreshTokens.length > 1
          ? t('admin.accounts.oauth.batchSuccess', { count: successCount })
          : t('admin.accounts.accountCreated')
      )
      emit('created')
      handleClose()
    } else if (successCount > 0 && failedCount > 0) {
      appStore.showWarning(
        t('admin.accounts.oauth.batchPartialSuccess', { success: successCount, failed: failedCount })
      )
      grokOAuth.error.value = errors.join('\n')
      emit('created')
    } else {
      grokOAuth.error.value = errors.join('\n')
      appStore.showError(t('admin.accounts.oauth.batchFailed'))
    }
  } finally {
    grokOAuth.loading.value = false
  }
}

// Gemini OAuth 授权码兑换
const handleGeminiExchange = async (authCode: string) => {
  if (!authCode.trim() || !geminiOAuth.sessionId.value) return

  geminiOAuth.loading.value = true
  geminiOAuth.error.value = ''

  try {
    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || geminiOAuth.state.value
    if (!stateToUse) {
      geminiOAuth.error.value = t('admin.accounts.oauth.authFailed')
      appStore.showError(geminiOAuth.error.value)
      return
    }

    const tokenInfo = await geminiOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId: geminiOAuth.sessionId.value,
      state: stateToUse,
      proxyId: form.proxy_id,
      oauthType: geminiOAuthType.value,
      tierId: geminiSelectedTier.value
    })
    if (!tokenInfo) return

    const credentials = geminiOAuth.buildCredentials(tokenInfo)
    const extra = buildGeminiExtra(geminiOAuth.buildExtraInfo(tokenInfo))
    await createAccountAndFinish('gemini', 'oauth', credentials, extra)
  } catch (error: any) {
    geminiOAuth.error.value = error.response?.data?.detail || t('admin.accounts.oauth.authFailed')
    appStore.showError(geminiOAuth.error.value)
  } finally {
    geminiOAuth.loading.value = false
  }
}

// Antigravity OAuth 授权码兑换
const handleAntigravityExchange = async (authCode: string) => {
  if (!authCode.trim() || !antigravityOAuth.sessionId.value) return

  antigravityOAuth.loading.value = true
  antigravityOAuth.error.value = ''

  try {
    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || antigravityOAuth.state.value
    if (!stateToUse) {
      antigravityOAuth.error.value = t('admin.accounts.oauth.authFailed')
      appStore.showError(antigravityOAuth.error.value)
      return
    }

    const tokenInfo = await antigravityOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId: antigravityOAuth.sessionId.value,
      state: stateToUse,
      proxyId: form.proxy_id
    })
		if (!tokenInfo) return

		const credentials = antigravityOAuth.buildCredentials(tokenInfo)
		applyInterceptWarmup(credentials, interceptWarmupRequests.value, 'create')
		// Antigravity 只使用映射模式
		const antigravityModelMapping = buildModelMappingObject(
			'mapping',
			[],
			antigravityModelMappings.value
		)
		if (antigravityModelMapping) {
			credentials.model_mapping = antigravityModelMapping
		}
		const extra = buildAntigravityExtra()
		await createAccountAndFinish('antigravity', 'oauth', credentials, extra)
  } catch (error: any) {
    antigravityOAuth.error.value = error.response?.data?.detail || t('admin.accounts.oauth.authFailed')
    appStore.showError(antigravityOAuth.error.value)
  } finally {
    antigravityOAuth.loading.value = false
  }
}

// Grok OAuth 授权码兑换
const handleGrokExchange = async (authCode: string) => {
  if (!authCode.trim() || !grokOAuth.sessionId.value) return

  grokOAuth.loading.value = true
  grokOAuth.error.value = ''

  try {
    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || grokOAuth.state.value

    const tokenInfo = await grokOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId: grokOAuth.sessionId.value,
      state: stateToUse,
      proxyId: form.proxy_id
    })
    if (!tokenInfo) return

    const credentials = grokOAuth.buildCredentials(tokenInfo)
    const modelMapping = buildAdvancedModelMapping()
    if (modelMapping) {
      credentials.model_mapping = modelMapping
    }
    const extra = buildModelListExtra(grokOAuth.buildExtraInfo(tokenInfo))
    await createAccountAndFinish('grok', 'oauth', credentials, extra)
  } catch (error: any) {
    grokOAuth.error.value = error.response?.data?.detail || t('admin.accounts.oauth.authFailed')
    appStore.showError(grokOAuth.error.value)
  } finally {
    grokOAuth.loading.value = false
  }
}

// Anthropic OAuth 授权码兑换
const handleAnthropicExchange = async (authCode: string) => {
  if (!authCode.trim() || !oauth.sessionId.value) return

  oauth.loading.value = true
  oauth.error.value = ''

  try {
    oauth.authCode.value = authCode.trim()
    const tokenInfo = await oauth.exchangeAuthCode(addMethod.value, form.proxy_id)
    if (!tokenInfo) return

    // Build extra with quota control settings
    const baseExtra = oauth.buildExtraInfo(tokenInfo) || {}
    const extra: Record<string, unknown> = { ...baseExtra }

    // Add window cost limit settings
    if (windowCostEnabled.value && windowCostLimit.value != null && windowCostLimit.value > 0) {
      extra.window_cost_limit = windowCostLimit.value
      extra.window_cost_sticky_reserve = windowCostStickyReserve.value ?? 10
    }

    // Add session limit settings
    if (sessionLimitEnabled.value && maxSessions.value != null && maxSessions.value > 0) {
      extra.max_sessions = maxSessions.value
      extra.session_idle_timeout_minutes = sessionIdleTimeout.value ?? 5
    }

    // Add RPM limit settings
    if (rpmLimitEnabled.value) {
      const DEFAULT_BASE_RPM = 15
      extra.base_rpm = (baseRpm.value != null && baseRpm.value > 0)
        ? baseRpm.value
        : DEFAULT_BASE_RPM
      extra.rpm_strategy = rpmStrategy.value
      if (rpmStickyBuffer.value != null && rpmStickyBuffer.value > 0) {
        extra.rpm_sticky_buffer = rpmStickyBuffer.value
      }
    }

    // UMQ mode（独立于 RPM）
    if (userMsgQueueMode.value) {
      extra.user_msg_queue_mode = userMsgQueueMode.value
    }

    // Add TLS fingerprint settings
    if (tlsFingerprintEnabled.value) {
      extra.enable_tls_fingerprint = true
      if (tlsFingerprintProfileId.value) {
        extra.tls_fingerprint_profile_id = tlsFingerprintProfileId.value
      }
    }

    // Add session ID masking settings
    if (sessionIdMaskingEnabled.value) {
      extra.session_id_masking_enabled = true
    }

    // Add cache TTL override settings
    if (cacheTTLOverrideEnabled.value) {
      extra.cache_ttl_override_enabled = true
      extra.cache_ttl_override_target = cacheTTLOverrideTarget.value
    }

    // Add custom base URL settings
    if (customBaseUrlEnabled.value && customBaseUrl.value.trim()) {
      extra.custom_base_url_enabled = true
      extra.custom_base_url = customBaseUrl.value.trim()
    }

    const credentials: Record<string, unknown> = { ...tokenInfo }
    applyInterceptWarmup(credentials, interceptWarmupRequests.value, 'create')
    await createAccountAndFinish(form.platform, addMethod.value as AccountType, credentials, extra)
  } catch (error: any) {
    oauth.error.value = error.response?.data?.detail || t('admin.accounts.oauth.authFailed')
    appStore.showError(oauth.error.value)
  } finally {
    oauth.loading.value = false
  }
}

// 主入口：根据平台路由到对应处理函数
const handleExchangeCode = async () => {
  const authCode = oauthFlowRef.value?.authCode || ''

  switch (form.platform) {
    case 'openai':
      return handleOpenAIExchange(authCode)
    case 'gemini':
      return handleGeminiExchange(authCode)
    case 'grok':
      return handleGrokExchange(authCode)
    case 'antigravity':
      return handleAntigravityExchange(authCode)
    default:
      return handleAnthropicExchange(authCode)
  }
}

const handleCookieAuth = async (sessionKey: string) => {
  oauth.loading.value = true
  oauth.error.value = ''

  try {
    const keys = oauth.parseSessionKeys(sessionKey)

    if (keys.length === 0) {
      oauth.error.value = t('admin.accounts.oauth.pleaseEnterSessionKey')
      return
    }

    const tempUnschedPayload = tempUnschedEnabled.value
      ? buildTempUnschedRules(tempUnschedRules.value)
      : []
    if (tempUnschedEnabled.value && tempUnschedPayload.length === 0) {
      appStore.showError(t('admin.accounts.tempUnschedulable.rulesInvalid'))
      return
    }

    let successCount = 0
    let failedCount = 0
    const errors: string[] = []

    for (let i = 0; i < keys.length; i++) {
      try {
        const tokenInfo = await oauth.cookieAuth(addMethod.value, keys[i], form.proxy_id)
        if (!tokenInfo) {
          throw new Error(oauth.error.value || t('admin.accounts.oauth.authFailed'))
        }

        // Build extra with quota control settings
        const baseExtra = oauth.buildExtraInfo(tokenInfo) || {}
        const extra: Record<string, unknown> = { ...baseExtra }

        // Add window cost limit settings
        if (windowCostEnabled.value && windowCostLimit.value != null && windowCostLimit.value > 0) {
          extra.window_cost_limit = windowCostLimit.value
          extra.window_cost_sticky_reserve = windowCostStickyReserve.value ?? 10
        }

        // Add session limit settings
        if (sessionLimitEnabled.value && maxSessions.value != null && maxSessions.value > 0) {
          extra.max_sessions = maxSessions.value
          extra.session_idle_timeout_minutes = sessionIdleTimeout.value ?? 5
        }

        // Add RPM limit settings
        if (rpmLimitEnabled.value) {
          const DEFAULT_BASE_RPM = 15
          extra.base_rpm = (baseRpm.value != null && baseRpm.value > 0)
            ? baseRpm.value
            : DEFAULT_BASE_RPM
          extra.rpm_strategy = rpmStrategy.value
          if (rpmStickyBuffer.value != null && rpmStickyBuffer.value > 0) {
            extra.rpm_sticky_buffer = rpmStickyBuffer.value
          }
        }

        // UMQ mode（独立于 RPM）
        if (userMsgQueueMode.value) {
          extra.user_msg_queue_mode = userMsgQueueMode.value
        }

        // Add TLS fingerprint settings
        if (tlsFingerprintEnabled.value) {
          extra.enable_tls_fingerprint = true
          if (tlsFingerprintProfileId.value) {
            extra.tls_fingerprint_profile_id = tlsFingerprintProfileId.value
          }
        }

        // Add session ID masking settings
        if (sessionIdMaskingEnabled.value) {
          extra.session_id_masking_enabled = true
        }

        // Add cache TTL override settings
        if (cacheTTLOverrideEnabled.value) {
          extra.cache_ttl_override_enabled = true
          extra.cache_ttl_override_target = cacheTTLOverrideTarget.value
        }

        // Add custom base URL settings
        if (customBaseUrlEnabled.value && customBaseUrl.value.trim()) {
          extra.custom_base_url_enabled = true
          extra.custom_base_url = customBaseUrl.value.trim()
        }

        const accountName = keys.length > 1 ? `${form.name} #${i + 1}` : form.name

        const credentials: Record<string, unknown> = { ...tokenInfo }
        applyInterceptWarmup(credentials, interceptWarmupRequests.value, 'create')
        if (tempUnschedEnabled.value) {
          credentials.temp_unschedulable_enabled = true
          credentials.temp_unschedulable_rules = tempUnschedPayload
        }

        const finalExtra = buildModelListExtra(extra)
        await adminAPI.accounts.create({
          name: accountName,
          notes: form.notes,
          platform: form.platform,
          type: addMethod.value, // Use addMethod as type: 'oauth' or 'setup-token'
          credentials,
          extra: finalExtra,
          proxy_id: form.proxy_id,
          concurrency: form.concurrency,
          load_factor: form.load_factor ?? undefined,
          priority: form.priority,
          rate_multiplier: form.rate_multiplier,
          group_ids: form.group_ids,
          expires_at: form.expires_at,
          auto_pause_on_expired: autoPauseOnExpired.value
        })

        successCount++
      } catch (error: any) {
        failedCount++
        errors.push(
          t('admin.accounts.oauth.keyAuthFailed', {
            index: i + 1,
            error: error.response?.data?.detail || t('admin.accounts.oauth.authFailed')
          })
        )
      }
    }

    if (successCount > 0) {
      appStore.showSuccess(t('admin.accounts.oauth.successCreated', { count: successCount }))
      if (failedCount === 0) {
        emit('created')
        handleClose()
      } else {
        emit('created')
      }
    }

    if (failedCount > 0) {
      oauth.error.value = errors.join('\n')
    }
  } catch (error: any) {
    oauth.error.value = error.response?.data?.detail || t('admin.accounts.oauth.cookieAuthFailed')
  } finally {
    oauth.loading.value = false
  }
}
</script>
