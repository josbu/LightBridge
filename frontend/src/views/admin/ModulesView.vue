<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-between">
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
        <p class="min-w-0 break-words text-sm text-red-700 dark:text-red-200">{{ error }}</p>
      </div>

      <!-- 内置功能卡片 -->
      <section class="card overflow-hidden">
        <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <div class="flex items-center justify-between">
            <span class="rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              {{ t('modules.builtinCount', { count: builtinFeatures.length }) }}
            </span>
          </div>
        </div>
        <div class="grid grid-cols-1 gap-4 p-5 sm:grid-cols-2 lg:grid-cols-3">
          <div
            v-for="feature in builtinFeatures"
            :key="feature.key"
            class="flex flex-col rounded-xl border border-gray-200 p-4 transition-colors dark:border-dark-700"
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
              <Toggle :model-value="feature.enabled" @update:model-value="toggleBuiltinFeature(feature, $event)" />
            </div>
            <div class="mt-3 flex items-center justify-end">
              <router-link
                v-if="feature.configPath"
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
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const error = ref('')
const builtinFeatureBusy = ref('')

interface BuiltinFeature {
  key: string
  title: string
  description: string
  icon: string
  iconBg: string
  configPath: string
  enabled: boolean
  settingKey: string
}

const builtinFeatures = computed<BuiltinFeature[]>(() => {
  const ps = appStore.cachedPublicSettings
  return [
    {
      key: 'channel-monitor',
      title: t('modules.builtin.channelMonitor'),
      description: t('modules.builtin.channelMonitorDesc'),
      icon: 'chart',
      iconBg: 'bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-300',
      configPath: '/admin/channels/monitor',
      enabled: !!ps?.channel_monitor_enabled,
      settingKey: 'channel_monitor_enabled'
    },
    {
      key: 'available-channels',
      title: t('modules.builtin.availableChannels'),
      description: t('modules.builtin.availableChannelsDesc'),
      icon: 'dollar',
      iconBg: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300',
      configPath: '/admin/channels/pricing',
      enabled: !!ps?.available_channels_enabled,
      settingKey: 'available_channels_enabled'
    },
    {
      key: 'risk-control',
      title: t('modules.builtin.riskControl'),
      description: t('modules.builtin.riskControlDesc'),
      icon: 'shield',
      iconBg: 'bg-amber-50 text-amber-600 dark:bg-amber-900/30 dark:text-amber-300',
      configPath: '/admin/risk-control',
      enabled: !!ps?.risk_control_enabled,
      settingKey: 'risk_control_enabled'
    },
    {
      key: 'privacy-filter',
      title: t('modules.builtin.privacyFilter'),
      description: t('modules.builtin.privacyFilterDesc'),
      icon: 'shield',
      iconBg: 'bg-violet-50 text-violet-600 dark:bg-violet-900/30 dark:text-violet-300',
      configPath: '/admin/privacy-filter',
      enabled: !!ps?.privacy_filter_enabled,
      settingKey: 'privacy_filter_enabled'
    },
    {
      key: 'affiliate',
      title: t('modules.builtin.affiliate'),
      description: t('modules.builtin.affiliateDesc'),
      icon: 'gift',
      iconBg: 'bg-rose-50 text-rose-600 dark:bg-rose-900/30 dark:text-rose-300',
      configPath: '/admin/affiliates/invites',
      enabled: !!ps?.affiliate_enabled,
      settingKey: 'affiliate_enabled'
    },
    {
      key: 'email-verification',
      title: t('modules.builtin.emailVerification'),
      description: t('modules.builtin.emailVerificationDesc'),
      icon: 'mail',
      iconBg: 'bg-cyan-50 text-cyan-600 dark:bg-cyan-900/30 dark:text-cyan-300',
      configPath: '/admin/settings/email',
      enabled: !!ps?.email_verify_enabled,
      settingKey: 'email_verify_enabled'
    },
    {
      key: 'login-agreement',
      title: t('modules.builtin.loginAgreement'),
      description: t('modules.builtin.loginAgreementDesc'),
      icon: 'document',
      iconBg: 'bg-indigo-50 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300',
      configPath: '/admin/settings/agreement',
      enabled: !!ps?.login_agreement_enabled,
      settingKey: 'login_agreement_enabled'
    },
    {
      key: 'redeem',
      title: t('modules.builtin.redeem'),
      description: t('modules.builtin.redeemDesc'),
      icon: 'ticket',
      iconBg: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300',
      configPath: '/admin/redeem',
      enabled: !!ps?.redeem_enabled,
      settingKey: 'redeem_enabled'
    }
  ]
})

async function toggleBuiltinFeature(feature: BuiltinFeature, value: boolean) {
  if (builtinFeatureBusy.value) return
  builtinFeatureBusy.value = feature.key
  error.value = ''
  try {
    await settingsAPI.updateSettings({ [feature.settingKey]: value } as Record<string, unknown>)
    await appStore.fetchPublicSettings(true)
  } catch (err) {
    error.value = extractApiErrorMessage(err, t('common.error'))
  } finally {
    builtinFeatureBusy.value = ''
  }
}

function messageOf(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

async function loadAll() {
  loading.value = true
  error.value = ''
  try {
    await appStore.fetchPublicSettings(true)
  } catch (err) {
    error.value = messageOf(err)
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)
</script>
