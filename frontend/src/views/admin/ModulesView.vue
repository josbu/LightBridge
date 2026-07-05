<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-between gap-4">
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('modules.title') }}</h1>
        <button class="btn btn-secondary" :disabled="loading" @click="loadAll">
          <Icon name="refresh" size="sm" :stroke-width="2" :class="{ 'animate-spin': loading }" />
          {{ t('modules.refresh') }}
        </button>
      </div>

      <div
        v-if="error"
        class="flex items-start gap-3 rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-800/50 dark:bg-red-900/20"
      >
        <Icon name="xCircle" size="md" :stroke-width="2" class="mt-0.5 flex-shrink-0 text-red-600 dark:text-red-400" />
        <p class="min-w-0 whitespace-pre-line break-words text-sm text-red-700 dark:text-red-200">{{ error }}</p>
      </div>

      <section class="card overflow-hidden">
        <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div class="min-w-0">
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('modules.builtinFeatures') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('modules.builtinFeaturesDescription') }}</p>
            </div>
            <span class="w-fit rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              {{ t('modules.builtinCount', { count: builtinFeatures.length }) }}
            </span>
          </div>
        </div>
        <div class="grid grid-cols-1 gap-4 p-5 sm:grid-cols-2 lg:grid-cols-3">
          <div
            v-for="feature in builtinFeatures"
            :key="feature.key"
            class="flex min-h-[132px] flex-col rounded-xl border border-gray-200 p-4 transition-colors dark:border-dark-700"
            :class="{ 'opacity-70': isBuiltinBusy(feature) }"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="flex min-w-0 items-center gap-3">
                <span
                  class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg"
                  :class="feature.iconBg"
                >
                  <Icon :name="(feature.icon as any)" size="sm" :stroke-width="2" />
                </span>
                <div class="min-w-0">
                  <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ feature.title }}</h3>
                  <p class="mt-0.5 line-clamp-2 text-xs text-gray-500 dark:text-dark-400">{{ feature.description }}</p>
                </div>
              </div>
              <Toggle
                :model-value="feature.enabled"
                :disabled="Boolean(builtinFeatureBusy)"
                @update:model-value="toggleBuiltinFeature(feature, $event)"
              />
            </div>
            <div class="mt-auto flex items-center justify-end pt-3">
              <router-link
                v-if="feature.configPath && feature.enabled"
                :to="feature.configPath"
                class="inline-flex items-center gap-1 text-xs font-medium text-primary-600 hover:underline dark:text-primary-400"
              >
                {{ t('modules.configure') }}
                <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
                </svg>
              </router-link>
            </div>
          </div>
        </div>
      </section>

      <section class="space-y-3">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div class="min-w-0">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('modules.installedModules') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('modules.installedDescription') }}</p>
          </div>
          <span class="w-fit rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-dark-300">
            {{ t('modules.moduleCount', { count: installedModules.length }) }}
          </span>
        </div>

        <div v-if="installedModules.length === 0" class="card p-6 text-center">
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('modules.noInstalled') }}</p>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('modules.noInstalledHint') }}</p>
        </div>

        <div v-else class="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <div
            v-for="module in installedModules"
            :key="module.id"
            class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ module.name || module.id }}</h3>
                  <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(module.status)">
                    {{ statusLabel(module.status) }}
                  </span>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ typeLabel(module.type) }} · {{ t('modules.versionValue', { version: module.version }) }}
                </p>
                <p v-if="module.enabledAt" class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ t('modules.enabledAt', { time: formatDateTime(module.enabledAt) }) }}
                </p>
              </div>
              <Icon :name="module.type === 'outbound' ? 'globe' : 'server'" size="md" :stroke-width="2" class="flex-shrink-0 text-gray-400" />
            </div>

            <p
              v-if="module.lastError"
              class="mt-3 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-900/20 dark:text-red-200"
            >
              {{ module.lastError }}
            </p>

            <div class="mt-4 flex flex-wrap justify-end gap-2">
              <button
                v-if="module.status !== 'enabled' && module.status !== 'uninstalled'"
                class="btn btn-ghost btn-sm"
                :disabled="isModuleBusy(module.id)"
                @click="approvePermissions(module.id)"
              >
                {{ t('modules.approvePermissions') }}
              </button>
              <button
                v-if="module.status !== 'enabled' && module.status !== 'uninstalled'"
                class="btn btn-primary btn-sm"
                :disabled="isModuleBusy(module.id)"
                @click="enableInstalledModule(module.id)"
              >
                {{ t('modules.enable') }}
              </button>
              <button
                v-if="module.status === 'enabled'"
                class="btn btn-secondary btn-sm"
                :disabled="isModuleBusy(module.id)"
                @click="disableInstalledModule(module.id)"
              >
                {{ t('modules.disable') }}
              </button>
              <button
                v-if="module.status !== 'uninstalled'"
                class="btn btn-secondary btn-sm"
                :disabled="isModuleBusy(module.id)"
                @click="uninstallInstalledModule(module.id)"
              >
                {{ t('modules.uninstall') }}
              </button>
              <button
                class="btn btn-danger btn-sm"
                :disabled="isModuleBusy(module.id)"
                @click="purgeInstalledModule(module.id)"
              >
                {{ t('modules.purge') }}
              </button>
            </div>
          </div>
        </div>
      </section>

      <section class="space-y-3">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div class="min-w-0">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('modules.marketplace') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('modules.marketplaceDescription') }}</p>
          </div>
          <span class="w-fit rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-dark-300">
            {{ t('modules.packageCount', { count: marketplaceModules.length }) }}
          </span>
        </div>

        <div v-if="marketplaceModules.length === 0" class="card p-6 text-center">
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('modules.noMarketplace') }}</p>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('modules.noMarketplaceHint') }}</p>
        </div>

        <div v-else class="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <div
            v-for="module in marketplaceModules"
            :key="`${module.id}@${module.version}`"
            class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ marketplaceName(module) }}</h3>
                  <span class="rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-dark-300">
                    {{ typeLabel(module.type) }}
                  </span>
                  <span v-if="module.sha256 || module.signature" class="rounded-full bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">
                    {{ t('modules.signedPackage') }}
                  </span>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ t('modules.versionValue', { version: module.version }) }}
                </p>
              </div>
              <span
                v-if="marketplaceInstalledStatus(module)"
                class="rounded-full px-2 py-0.5 text-xs font-medium"
                :class="statusClass(marketplaceInstalledStatus(module) || '')"
              >
                {{ statusLabel(marketplaceInstalledStatus(module) || '') }}
              </span>
            </div>

            <p class="mt-3 line-clamp-3 min-h-[48px] text-sm text-gray-600 dark:text-dark-300">
              {{ marketplaceDescription(module) }}
            </p>

            <div class="mt-4 flex flex-wrap justify-end gap-2">
              <button
                class="btn btn-primary btn-sm"
                :disabled="isModuleBusy(module.id) || marketplaceCurrentInstalled(module)"
                @click="installModule(module)"
              >
                {{ marketplaceCurrentInstalled(module) ? t('modules.installed') : t('modules.install') }}
              </button>
              <button
                v-if="marketplaceInstalledStatus(module) && marketplaceInstalledStatus(module) !== 'enabled' && marketplaceInstalledStatus(module) !== 'uninstalled'"
                class="btn btn-secondary btn-sm"
                :disabled="isModuleBusy(module.id)"
                @click="enableInstalledModule(module.id)"
              >
                {{ t('modules.enable') }}
              </button>
            </div>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import { settingsAPI } from '@/api/admin/settings'
import {
  approveModulePermissions,
  disableModule,
  enableModule,
  installMarketplaceModule,
  listInstalledModules,
  listMarketplaceModules,
  purgeModule,
  uninstallModule,
  type InstalledModule,
  type MarketplaceModule
} from '@/api/admin/modules'
import { extractApiErrorMessage } from '@/utils/apiError'
import { FeatureFlags, isFeatureFlagEnabled, type FeatureFlagDefinition } from '@/utils/featureFlags'
import type { PublicSettings } from '@/types'

const { t, locale } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const error = ref('')
const builtinFeatureBusy = ref('')
const pendingBuiltinStates = ref<Partial<Record<keyof PublicSettings, boolean>>>({})
const installedModules = ref<InstalledModule[]>([])
const marketplaceModules = ref<MarketplaceModule[]>([])
const moduleBusy = ref('')

interface BuiltinFeature {
  key: string
  title: string
  description: string
  icon: string
  iconBg: string
  configPath: string
  enabled: boolean
  settingKey: keyof PublicSettings
  flag?: FeatureFlagDefinition
}

function resolveBuiltinEnabled(settingKey: keyof PublicSettings, flag?: FeatureFlagDefinition): boolean {
  const pending = pendingBuiltinStates.value[settingKey]
  if (typeof pending === 'boolean') return pending
  if (flag) return isFeatureFlagEnabled(flag)
  return appStore.cachedPublicSettings?.[settingKey] === true
}

const builtinFeatures = computed<BuiltinFeature[]>(() => [
  {
    key: 'channel-monitor',
    title: t('modules.builtin.channelMonitor'),
    description: t('modules.builtin.channelMonitorDesc'),
    icon: 'chart',
    iconBg: 'bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-300',
    configPath: '/admin/channels/monitor',
    enabled: resolveBuiltinEnabled('channel_monitor_enabled', FeatureFlags.channelMonitor),
    settingKey: 'channel_monitor_enabled',
    flag: FeatureFlags.channelMonitor
  },
  {
    key: 'available-channels',
    title: t('modules.builtin.availableChannels'),
    description: t('modules.builtin.availableChannelsDesc'),
    icon: 'dollar',
    iconBg: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300',
    configPath: '/admin/channels/pricing',
    enabled: resolveBuiltinEnabled('available_channels_enabled', FeatureFlags.availableChannels),
    settingKey: 'available_channels_enabled',
    flag: FeatureFlags.availableChannels
  },
  {
    key: 'risk-control',
    title: t('modules.builtin.riskControl'),
    description: t('modules.builtin.riskControlDesc'),
    icon: 'shield',
    iconBg: 'bg-amber-50 text-amber-600 dark:bg-amber-900/30 dark:text-amber-300',
    configPath: '/admin/risk-control',
    enabled: resolveBuiltinEnabled('risk_control_enabled', FeatureFlags.riskControl),
    settingKey: 'risk_control_enabled',
    flag: FeatureFlags.riskControl
  },
  {
    key: 'privacy-filter',
    title: t('modules.builtin.privacyFilter'),
    description: t('modules.builtin.privacyFilterDesc'),
    icon: 'shield',
    iconBg: 'bg-violet-50 text-violet-600 dark:bg-violet-900/30 dark:text-violet-300',
    configPath: '/admin/privacy-filter',
    enabled: resolveBuiltinEnabled('privacy_filter_enabled', FeatureFlags.privacyFilter),
    settingKey: 'privacy_filter_enabled',
    flag: FeatureFlags.privacyFilter
  },
  {
    key: 'affiliate',
    title: t('modules.builtin.affiliate'),
    description: t('modules.builtin.affiliateDesc'),
    icon: 'gift',
    iconBg: 'bg-rose-50 text-rose-600 dark:bg-rose-900/30 dark:text-rose-300',
    configPath: '/admin/affiliates/invites',
    enabled: resolveBuiltinEnabled('affiliate_enabled', FeatureFlags.affiliate),
    settingKey: 'affiliate_enabled',
    flag: FeatureFlags.affiliate
  },
  {
    key: 'email-verification',
    title: t('modules.builtin.emailVerification'),
    description: t('modules.builtin.emailVerificationDesc'),
    icon: 'mail',
    iconBg: 'bg-cyan-50 text-cyan-600 dark:bg-cyan-900/30 dark:text-cyan-300',
    configPath: '/admin/settings/email',
    enabled: resolveBuiltinEnabled('email_verify_enabled'),
    settingKey: 'email_verify_enabled'
  },
  {
    key: 'login-agreement',
    title: t('modules.builtin.loginAgreement'),
    description: t('modules.builtin.loginAgreementDesc'),
    icon: 'document',
    iconBg: 'bg-indigo-50 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300',
    configPath: '/admin/settings/agreement',
    enabled: resolveBuiltinEnabled('login_agreement_enabled', FeatureFlags.loginAgreement),
    settingKey: 'login_agreement_enabled',
    flag: FeatureFlags.loginAgreement
  },
  {
    key: 'redeem',
    title: t('modules.builtin.redeem'),
    description: t('modules.builtin.redeemDesc'),
    icon: 'ticket',
    iconBg: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300',
    configPath: '/admin/redeem',
    enabled: resolveBuiltinEnabled('redeem_enabled', FeatureFlags.redeem),
    settingKey: 'redeem_enabled',
    flag: FeatureFlags.redeem
  }
])

function isBuiltinBusy(feature: BuiltinFeature): boolean {
  return builtinFeatureBusy.value === feature.key
}

async function toggleBuiltinFeature(feature: BuiltinFeature, value: boolean) {
  if (builtinFeatureBusy.value) return

  const previous = appStore.cachedPublicSettings?.[feature.settingKey]
  builtinFeatureBusy.value = feature.key
  pendingBuiltinStates.value = { ...pendingBuiltinStates.value, [feature.settingKey]: value }
  appStore.patchPublicSettings({ [feature.settingKey]: value } as Partial<PublicSettings>)
  error.value = ''

  try {
    await settingsAPI.updateSettings({ [feature.settingKey]: value } as Record<string, unknown>)
    await appStore.fetchPublicSettings(true)
  } catch (err) {
    error.value = extractApiErrorMessage(err, t('common.error'))
    if (typeof previous === 'boolean') {
      appStore.patchPublicSettings({ [feature.settingKey]: previous } as Partial<PublicSettings>)
    }
    await appStore.fetchPublicSettings(true)
  } finally {
    const nextPending = { ...pendingBuiltinStates.value }
    delete nextPending[feature.settingKey]
    pendingBuiltinStates.value = nextPending
    builtinFeatureBusy.value = ''
  }
}

function messageOf(err: unknown): string {
  return extractApiErrorMessage(err, t('modules.operationFailed'))
}

async function loadModuleData() {
  const failures: string[] = []
  const [installedResult, marketplaceResult] = await Promise.allSettled([
    listInstalledModules(),
    listMarketplaceModules()
  ])

  if (installedResult.status === 'fulfilled') {
    installedModules.value = installedResult.value
  } else {
    installedModules.value = []
    failures.push(messageOf(installedResult.reason))
  }

  if (marketplaceResult.status === 'fulfilled') {
    marketplaceModules.value = marketplaceResult.value
  } else {
    marketplaceModules.value = []
    failures.push(messageOf(marketplaceResult.reason))
  }

  if (failures.length > 0) {
    error.value = failures.join('\n')
  }
}

async function loadAll() {
  loading.value = true
  error.value = ''
  try {
    await Promise.all([
      appStore.fetchPublicSettings(true),
      loadModuleData()
    ])
  } catch (err) {
    error.value = messageOf(err)
  } finally {
    loading.value = false
  }
}

async function withModuleAction(id: string, action: () => Promise<unknown>) {
  if (moduleBusy.value) return
  moduleBusy.value = id
  error.value = ''
  try {
    await action()
    await loadModuleData()
  } catch (err) {
    error.value = messageOf(err)
  } finally {
    moduleBusy.value = ''
  }
}

function isModuleBusy(id: string): boolean {
  return moduleBusy.value === id
}

function installModule(module: MarketplaceModule) {
  return withModuleAction(module.id, () => installMarketplaceModule(module.id, module.version))
}

function enableInstalledModule(id: string) {
  return withModuleAction(id, () => enableModule(id))
}

function disableInstalledModule(id: string) {
  return withModuleAction(id, () => disableModule(id))
}

function uninstallInstalledModule(id: string) {
  return withModuleAction(id, () => uninstallModule(id))
}

function purgeInstalledModule(id: string) {
  return withModuleAction(id, () => purgeModule(id))
}

function approvePermissions(id: string) {
  return withModuleAction(id, () => approveModulePermissions(id))
}

function statusLabel(status: string): string {
  const key = `modules.status.${status}`
  const translated = t(key)
  return translated === key ? status : translated
}

function typeLabel(type: string): string {
  const key = `modules.type.${type}`
  const translated = t(key)
  return translated === key ? type : translated
}

function statusClass(status: string): string {
  switch (status) {
    case 'enabled':
      return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'failed':
      return 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'disabled':
    case 'uninstalled':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
    default:
      return 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  }
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function localizedText(value: Record<string, string> | undefined, fallback: string): string {
  if (!value) return fallback
  const current = locale.value
  const short = current.split('-')[0]
  return value[current] || value[short] || value['zh-CN'] || value.zh || value.en || fallback
}

function marketplaceName(module: MarketplaceModule): string {
  return localizedText(module.name_i18n, module.name || module.id)
}

function marketplaceDescription(module: MarketplaceModule): string {
  return localizedText(module.description_i18n, module.description || module.summary || module.id)
}

function marketplaceInstalledStatus(module: MarketplaceModule): string | undefined {
  return module.installedStatus || installedModules.value.find((item) => item.id === module.id)?.status
}

function marketplaceInstalledVersion(module: MarketplaceModule): string | undefined {
  return module.installedVersion || installedModules.value.find((item) => item.id === module.id)?.version
}

function marketplaceCurrentInstalled(module: MarketplaceModule): boolean {
  const status = marketplaceInstalledStatus(module)
  return status !== 'uninstalled' && marketplaceInstalledVersion(module) === module.version
}

onMounted(loadAll)
</script>
