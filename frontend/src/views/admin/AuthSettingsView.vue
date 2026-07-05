<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <div class="flex items-center justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.authSettings.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.authSettings.description') }}</p>
        </div>
        <button class="btn btn-secondary" @click="$router.push('/admin/settings')">
          <Icon name="arrowLeft" size="sm" />
          {{ t('admin.authSettings.backToSettings') }}
        </button>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>

      <template v-else>
        <!-- OIDC Login -->
        <div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div class="flex items-center justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.authSettings.oidc.title') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.authSettings.oidc.description') }}</p>
              </div>
              <Toggle v-model="form.oidc_connect_enabled" @update:model-value="saveSettings" />
            </div>
          </div>
          <div v-if="form.oidc_connect_enabled" class="space-y-4 p-6">
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.authSettings.oidc.providerName') }}</label>
                <input v-model="form.oidc_connect_provider_name" type="text" class="input" :placeholder="t('admin.authSettings.oidc.providerNamePlaceholder')" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.authSettings.oidc.clientId') }}</label>
                <input v-model="form.oidc_connect_client_id" type="text" class="input" :placeholder="t('admin.authSettings.oidc.clientIdPlaceholder')" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.authSettings.oidc.issuerUrl') }}</label>
                <input v-model="form.oidc_connect_issuer_url" type="url" class="input" :placeholder="t('admin.authSettings.oidc.issuerUrlPlaceholder')" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.authSettings.oidc.redirectUrl') }}</label>
                <input v-model="form.oidc_connect_redirect_url" type="url" class="input" :placeholder="t('admin.authSettings.oidc.redirectUrlPlaceholder')" />
              </div>
            </div>
          </div>
        </div>

        <!-- GitHub Login -->
        <div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div class="flex items-center justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.authSettings.github.title') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.authSettings.github.description') }}</p>
              </div>
              <Toggle v-model="form.github_oauth_enabled" @update:model-value="saveSettings" />
            </div>
          </div>
          <div v-if="form.github_oauth_enabled" class="space-y-4 p-6">
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.authSettings.github.clientId') }}</label>
                <input v-model="form.github_oauth_client_id" type="text" class="input" :placeholder="t('admin.authSettings.github.clientIdPlaceholder')" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.authSettings.github.redirectUrl') }}</label>
                <input v-model="form.github_oauth_redirect_url" type="url" class="input" :placeholder="t('admin.authSettings.github.redirectUrlPlaceholder')" />
              </div>
            </div>
          </div>
        </div>

        <!-- Google Login -->
        <div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div class="flex items-center justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.authSettings.google.title') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.authSettings.google.description') }}</p>
              </div>
              <Toggle v-model="form.google_oauth_enabled" @update:model-value="saveSettings" />
            </div>
          </div>
          <div v-if="form.google_oauth_enabled" class="space-y-4 p-6">
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.authSettings.google.clientId') }}</label>
                <input v-model="form.google_oauth_client_id" type="text" class="input" :placeholder="t('admin.authSettings.google.clientIdPlaceholder')" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.authSettings.google.redirectUrl') }}</label>
                <input v-model="form.google_oauth_redirect_url" type="url" class="input" :placeholder="t('admin.authSettings.google.redirectUrlPlaceholder')" />
              </div>
            </div>
          </div>
        </div>

        <!-- WeChat Login -->
        <div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div class="flex items-center justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.authSettings.wechat.title') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.authSettings.wechat.description') }}</p>
              </div>
              <Toggle v-model="form.wechat_connect_enabled" @update:model-value="saveSettings" />
            </div>
          </div>
          <div v-if="form.wechat_connect_enabled" class="space-y-4 p-6">
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.authSettings.wechat.appId') }}</label>
                <input v-model="form.wechat_connect_app_id" type="text" class="input" :placeholder="t('admin.authSettings.wechat.appIdPlaceholder')" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.authSettings.wechat.redirectUrl') }}</label>
                <input v-model="form.wechat_connect_redirect_url" type="url" class="input" :placeholder="t('admin.authSettings.wechat.redirectUrlPlaceholder')" />
              </div>
            </div>
          </div>
        </div>

        <!-- LinuxDO Login -->
        <div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div class="flex items-center justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.authSettings.linuxdo.title') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.authSettings.linuxdo.description') }}</p>
              </div>
              <Toggle v-model="form.linuxdo_connect_enabled" @update:model-value="saveSettings" />
            </div>
          </div>
          <div v-if="form.linuxdo_connect_enabled" class="space-y-4 p-6">
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.authSettings.linuxdo.clientId') }}</label>
                <input v-model="form.linuxdo_connect_client_id" type="text" class="input" :placeholder="t('admin.authSettings.linuxdo.clientIdPlaceholder')" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.authSettings.linuxdo.redirectUrl') }}</label>
                <input v-model="form.linuxdo_connect_redirect_url" type="url" class="input" :placeholder="t('admin.authSettings.linuxdo.redirectUrlPlaceholder')" />
              </div>
            </div>
          </div>
        </div>

        <!-- DingTalk Login -->
        <div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div class="flex items-center justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.authSettings.dingtalk.title') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.authSettings.dingtalk.description') }}</p>
              </div>
              <Toggle v-model="form.dingtalk_connect_enabled" @update:model-value="saveSettings" />
            </div>
          </div>
          <div v-if="form.dingtalk_connect_enabled" class="space-y-4 p-6">
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.authSettings.dingtalk.clientId') }}</label>
                <input v-model="form.dingtalk_connect_client_id" type="text" class="input" :placeholder="t('admin.authSettings.dingtalk.clientIdPlaceholder')" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.authSettings.dingtalk.redirectUrl') }}</label>
                <input v-model="form.dingtalk_connect_redirect_url" type="url" class="input" :placeholder="t('admin.authSettings.dingtalk.redirectUrlPlaceholder')" />
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import { settingsAPI } from '@/api/admin/settings'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const saving = ref(false)

const form = reactive({
  // OIDC
  oidc_connect_enabled: false,
  oidc_connect_provider_name: '',
  oidc_connect_client_id: '',
  oidc_connect_issuer_url: '',
  oidc_connect_redirect_url: '',
  // GitHub
  github_oauth_enabled: false,
  github_oauth_client_id: '',
  github_oauth_redirect_url: '',
  // Google
  google_oauth_enabled: false,
  google_oauth_client_id: '',
  google_oauth_redirect_url: '',
  // WeChat
  wechat_connect_enabled: false,
  wechat_connect_app_id: '',
  wechat_connect_redirect_url: '',
  // LinuxDO
  linuxdo_connect_enabled: false,
  linuxdo_connect_client_id: '',
  linuxdo_connect_redirect_url: '',
  // DingTalk
  dingtalk_connect_enabled: false,
  dingtalk_connect_client_id: '',
  dingtalk_connect_redirect_url: '',
})

async function loadSettings() {
  loading.value = true
  try {
    const settings = await settingsAPI.getSettings()
    // OIDC
    form.oidc_connect_enabled = settings.oidc_connect_enabled || false
    form.oidc_connect_provider_name = settings.oidc_connect_provider_name || ''
    form.oidc_connect_client_id = settings.oidc_connect_client_id || ''
    form.oidc_connect_issuer_url = settings.oidc_connect_issuer_url || ''
    form.oidc_connect_redirect_url = settings.oidc_connect_redirect_url || ''
    // GitHub
    form.github_oauth_enabled = settings.github_oauth_enabled || false
    form.github_oauth_client_id = settings.github_oauth_client_id || ''
    form.github_oauth_redirect_url = settings.github_oauth_redirect_url || ''
    // Google
    form.google_oauth_enabled = settings.google_oauth_enabled || false
    form.google_oauth_client_id = settings.google_oauth_client_id || ''
    form.google_oauth_redirect_url = settings.google_oauth_redirect_url || ''
    // WeChat
    form.wechat_connect_enabled = settings.wechat_connect_enabled || false
    form.wechat_connect_app_id = settings.wechat_connect_app_id || ''
    form.wechat_connect_redirect_url = settings.wechat_connect_redirect_url || ''
    // LinuxDO
    form.linuxdo_connect_enabled = settings.linuxdo_connect_enabled || false
    form.linuxdo_connect_client_id = settings.linuxdo_connect_client_id || ''
    form.linuxdo_connect_redirect_url = settings.linuxdo_connect_redirect_url || ''
    // DingTalk
    form.dingtalk_connect_enabled = settings.dingtalk_connect_enabled || false
    form.dingtalk_connect_client_id = settings.dingtalk_connect_client_id || ''
    form.dingtalk_connect_redirect_url = settings.dingtalk_connect_redirect_url || ''
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  if (saving.value) return
  saving.value = true
  try {
    await settingsAPI.updateSettings({
      oidc_connect_enabled: form.oidc_connect_enabled,
      oidc_connect_provider_name: form.oidc_connect_provider_name,
      oidc_connect_client_id: form.oidc_connect_client_id,
      oidc_connect_issuer_url: form.oidc_connect_issuer_url,
      oidc_connect_redirect_url: form.oidc_connect_redirect_url,
      github_oauth_enabled: form.github_oauth_enabled,
      github_oauth_client_id: form.github_oauth_client_id,
      github_oauth_redirect_url: form.github_oauth_redirect_url,
      google_oauth_enabled: form.google_oauth_enabled,
      google_oauth_client_id: form.google_oauth_client_id,
      google_oauth_redirect_url: form.google_oauth_redirect_url,
      wechat_connect_enabled: form.wechat_connect_enabled,
      wechat_connect_app_id: form.wechat_connect_app_id,
      wechat_connect_redirect_url: form.wechat_connect_redirect_url,
      linuxdo_connect_enabled: form.linuxdo_connect_enabled,
      linuxdo_connect_client_id: form.linuxdo_connect_client_id,
      linuxdo_connect_redirect_url: form.linuxdo_connect_redirect_url,
      dingtalk_connect_enabled: form.dingtalk_connect_enabled,
      dingtalk_connect_client_id: form.dingtalk_connect_client_id,
      dingtalk_connect_redirect_url: form.dingtalk_connect_redirect_url,
    } as Record<string, unknown>)
    await appStore.fetchPublicSettings(true)
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    saving.value = false
  }
}

onMounted(loadSettings)
</script>
