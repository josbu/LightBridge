<template>
  <div class="space-y-5">
    <!-- LightBridge Connect 标题 -->
    <div class="rounded-lg border-2 border-purple-200 bg-purple-50 p-4 dark:border-purple-800 dark:bg-purple-900/20">
      <div class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-purple-500 text-white">
          <Icon name="link" size="md" />
        </div>
        <div>
          <h3 class="text-lg font-semibold text-purple-900 dark:text-purple-100">
            {{ t('admin.accounts.lightBridgeConnect.title') }}
          </h3>
          <p class="text-sm text-purple-700 dark:text-purple-300">
            {{ t('admin.accounts.lightBridgeConnect.description') }}
          </p>
        </div>
      </div>
    </div>

    <!-- 系统访问令牌 -->
    <div>
      <label class="input-label">
        {{ t('admin.accounts.lightBridgeConnect.systemToken') }}
        <span class="text-red-500">*</span>
      </label>
      <div class="flex gap-2">
        <input
          v-model="localConfig.system_token"
          type="password"
          required
          class="input flex-1"
          :placeholder="t('admin.accounts.lightBridgeConnect.systemTokenPlaceholder')"
        />
        <button
          type="button"
          @click="verifyToken"
          :disabled="verifying || !localConfig.system_token || !localConfig.instance_url"
          class="btn-secondary"
        >
          <Icon v-if="verifying" name="refresh" class="animate-spin" size="sm" />
          <Icon v-else name="checkCircle" size="sm" />
          {{ verifying ? t('admin.accounts.lightBridgeConnect.verifying') : t('admin.accounts.lightBridgeConnect.verify') }}
        </button>
      </div>
      <p class="input-hint">
        {{ t('admin.accounts.lightBridgeConnect.systemTokenHint') }}
      </p>

      <!-- 验证结果 -->
      <div v-if="verificationResult" class="mt-2">
        <div
          v-if="verificationResult.valid"
          class="rounded-md bg-green-50 p-3 dark:bg-green-900/20"
        >
          <div class="flex items-start gap-2">
            <Icon name="checkCircle" class="mt-0.5 text-green-600 dark:text-green-400" size="sm" />
            <div class="flex-1 text-sm">
              <p class="font-medium text-green-800 dark:text-green-200">
                {{ t('admin.accounts.lightBridgeConnect.verifySuccess') }}
              </p>
              <ul class="mt-1 space-y-1 text-green-700 dark:text-green-300">
                <li>{{ t('admin.accounts.lightBridgeConnect.username') }}: {{ verificationResult.username }}</li>
                <li v-if="verificationResult.display_name">
                  {{ t('admin.accounts.lightBridgeConnect.displayName') }}: {{ verificationResult.display_name }}
                </li>
                <li v-if="verificationResult.email">
                  {{ t('admin.accounts.lightBridgeConnect.email') }}: {{ verificationResult.email }}
                </li>
                <li>
                  {{ t('admin.accounts.lightBridgeConnect.balance') }}:
                  <span class="font-semibold">{{ formatBalance(verificationResult.quota || 0) }}</span>
                </li>
              </ul>
            </div>
          </div>
        </div>
        <div v-else class="rounded-md bg-red-50 p-3 dark:bg-red-900/20">
          <div class="flex items-start gap-2">
            <Icon name="xCircle" class="mt-0.5 text-red-600 dark:text-red-400" size="sm" />
            <div class="flex-1 text-sm">
              <p class="font-medium text-red-800 dark:text-red-200">
                {{ t('admin.accounts.lightBridgeConnect.verifyFailed') }}
              </p>
              <p class="mt-1 text-red-700 dark:text-red-300">{{ verificationResult.error_msg }}</p>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 余额警报配置 -->
    <div class="space-y-4 rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700">
      <h4 class="font-medium text-gray-900 dark:text-white">
        {{ t('admin.accounts.lightBridgeConnect.alertConfig') }}
      </h4>

      <!-- 启用警报 -->
      <div class="flex items-center justify-between">
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.accounts.lightBridgeConnect.enableAlert') }}
        </label>
        <label class="relative inline-flex cursor-pointer items-center">
          <input
            v-model="localConfig.alert!.enabled"
            type="checkbox"
            class="peer sr-only"
          />
          <div class="peer h-6 w-11 rounded-full bg-gray-300 after:absolute after:left-[2px] after:top-[2px] after:h-5 after:w-5 after:rounded-full after:bg-white after:transition-all after:content-[''] peer-checked:bg-primary-500 peer-checked:after:translate-x-full dark:bg-gray-600"></div>
        </label>
      </div>

      <div v-if="localConfig.alert?.enabled" class="space-y-4">
        <!-- 警报阈值 -->
        <div>
          <label class="input-label">
            {{ t('admin.accounts.lightBridgeConnect.alertThreshold') }}
          </label>
          <div class="flex items-center gap-2">
            <input
              v-model.number="thresholdYuan"
              type="number"
              min="0"
              step="1"
              class="input"
              :placeholder="t('admin.accounts.lightBridgeConnect.alertThresholdPlaceholder')"
            />
            <span class="text-sm text-gray-600 dark:text-gray-400">{{ t('common.currency.yuan') }}</span>
          </div>
          <p class="input-hint">
            {{ t('admin.accounts.lightBridgeConnect.alertThresholdHint') }}
          </p>
        </div>

        <!-- 警报渠道 -->
        <div>
          <label class="input-label">
            {{ t('admin.accounts.lightBridgeConnect.alertChannels') }}
          </label>
          <div class="space-y-2">
            <label class="flex items-center gap-2">
              <input
                v-model="localConfig.alert!.channels"
                type="checkbox"
                value="dashboard"
                class="checkbox"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">
                {{ t('admin.accounts.lightBridgeConnect.channels.dashboard') }}
              </span>
            </label>
            <label class="flex items-center gap-2">
              <input
                v-model="localConfig.alert!.channels"
                type="checkbox"
                value="email"
                class="checkbox"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">
                {{ t('admin.accounts.lightBridgeConnect.channels.email') }}
              </span>
            </label>
            <label class="flex items-center gap-2">
              <input
                v-model="localConfig.alert!.channels"
                type="checkbox"
                value="webhook"
                class="checkbox"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">
                {{ t('admin.accounts.lightBridgeConnect.channels.webhook') }}
              </span>
            </label>
          </div>
        </div>

        <!-- Webhook URL (仅当选择了 webhook 渠道时显示) -->
        <div v-if="localConfig.alert?.channels.includes('webhook')">
          <label class="input-label">
            {{ t('admin.accounts.lightBridgeConnect.webhookUrl') }}
          </label>
          <input
            v-model="localConfig.webhook_url"
            type="url"
            class="input"
            :placeholder="t('admin.accounts.lightBridgeConnect.webhookUrlPlaceholder')"
          />
          <p class="input-hint">
            {{ t('admin.accounts.lightBridgeConnect.webhookUrlHint') }}
          </p>
        </div>

        <!-- 自动禁用 -->
        <div class="flex items-center justify-between">
          <div>
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.lightBridgeConnect.autoDisable') }}
            </label>
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.lightBridgeConnect.autoDisableHint') }}
            </p>
          </div>
          <label class="relative inline-flex cursor-pointer items-center">
            <input
              v-model="localConfig.alert!.auto_disable_on_low"
              type="checkbox"
              class="peer sr-only"
            />
            <div class="peer h-6 w-11 rounded-full bg-gray-300 after:absolute after:left-[2px] after:top-[2px] after:h-5 after:w-5 after:rounded-full after:bg-white after:transition-all after:content-[''] peer-checked:bg-primary-500 peer-checked:after:translate-x-full dark:bg-gray-600"></div>
          </label>
        </div>

        <!-- 同步间隔 -->
        <div>
          <label class="input-label">
            {{ t('admin.accounts.lightBridgeConnect.syncInterval') }}
          </label>
          <div class="flex items-center gap-2">
            <input
              v-model.number="syncIntervalMinutes"
              type="number"
              min="1"
              max="60"
              class="input"
            />
            <span class="text-sm text-gray-600 dark:text-gray-400">{{ t('common.time.minutes') }}</span>
          </div>
          <p class="input-hint">
            {{ t('admin.accounts.lightBridgeConnect.syncIntervalHint') }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { lightBridgeConnectAPI } from '@/api/lightbridge-connect'
import Icon from '@/components/icons/Icon.vue'
import type {
  LightBridgeConnectConfig,
  VerifyLightBridgeConnectTokenResponse
} from '@/types'

const { t } = useI18n()

interface Props {
  instanceUrl: string
  modelValue: Partial<LightBridgeConnectConfig>
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:modelValue': [value: Partial<LightBridgeConnectConfig>]
  'verified': [result: VerifyLightBridgeConnectTokenResponse]
}>()

// Local config state
const localConfig = ref<Partial<LightBridgeConnectConfig>>({
  type: 'new-api',
  instance_url: props.instanceUrl,
  system_token: '',
  alert: {
    enabled: true,
    threshold: 10000, // 100 元
    channels: ['dashboard'],
    auto_disable_on_low: true
  },
  sync_interval: 300, // 5 分钟
  ...props.modelValue
})

// 确保 alert 始终有值
if (!localConfig.value.alert) {
  localConfig.value.alert = {
    enabled: true,
    threshold: 10000,
    channels: ['dashboard'],
    auto_disable_on_low: true
  }
}

// Verification state
const verifying = ref(false)
const verificationResult = ref<VerifyLightBridgeConnectTokenResponse | null>(null)

// 元 <-> 分 转换
const thresholdYuan = computed({
  get: () => (localConfig.value.alert?.threshold || 0) / 100,
  set: (val) => {
    if (!localConfig.value.alert) {
      localConfig.value.alert = {
        enabled: true,
        threshold: Math.round(val * 100),
        channels: ['dashboard'],
        auto_disable_on_low: true
      }
    } else {
      localConfig.value.alert.threshold = Math.round(val * 100)
    }
  }
})

// 分钟 <-> 秒 转换
const syncIntervalMinutes = computed({
  get: () => (localConfig.value.sync_interval || 300) / 60,
  set: (val) => {
    localConfig.value.sync_interval = val * 60
  }
})

// 格式化余额显示
const formatBalance = (cents: number): string => {
  const yuan = cents / 100
  return `¥${yuan.toFixed(2)}`
}

// 验证令牌
const verifyToken = async () => {
  if (!localConfig.value.system_token || !localConfig.value.instance_url) {
    return
  }

  verifying.value = true
  verificationResult.value = null

  try {
    const result = await lightBridgeConnectAPI.verifyToken(0, {
      type: 'new-api',
      instance_url: localConfig.value.instance_url,
      system_token: localConfig.value.system_token
    })

    verificationResult.value = result

    if (result.valid) {
      // 更新 config 中的用户信息
      localConfig.value.user_id = result.user_id
      localConfig.value.username = result.username

      // 初始化 quota 信息
      if (result.quota !== undefined) {
        localConfig.value.quota = {
          balance: result.quota,
          used: result.used_quota || 0,
          currency: 'CNY',
          last_sync_at: new Date().toISOString()
        }
      }

      emit('verified', result)
    }
  } catch (error: any) {
    verificationResult.value = {
      valid: false,
      error_msg: error.message || t('admin.accounts.lightBridgeConnect.verifyError')
    }
  } finally {
    verifying.value = false
  }
}

// Watch for changes and emit
watch(
  () => localConfig.value,
  (newValue) => {
    emit('update:modelValue', newValue)
  },
  { deep: true }
)

// Watch for instance URL changes from parent
watch(
  () => props.instanceUrl,
  (newUrl) => {
    localConfig.value.instance_url = newUrl
  }
)
</script>
