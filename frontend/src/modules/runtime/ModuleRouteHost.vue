<template>
  <component v-if="component" :is="component" />
  <div v-else-if="error" class="m-6 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
    {{ error }}
  </div>
  <div v-else class="p-8 text-center text-sm text-gray-500">{{ t('modules.runtime.loading') }}</div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { loadModuleComponent } from './remoteLoader'
import type { RegisteredModuleRoute } from './registry'

const props = defineProps<{ contribution: RegisteredModuleRoute }>()
const { t } = useI18n()
const component = shallowRef<Component | null>(null)
const error = shallowRef('')
let active = true

onMounted(async () => {
  try {
    const loaded = await loadModuleComponent(props.contribution.remoteEntry, props.contribution.exposedModule)
    if (active) component.value = loaded
  } catch (cause) {
    if (active) error.value = cause instanceof Error ? cause.message : t('modules.runtime.loadFailed')
  }
})

onBeforeUnmount(() => { active = false })
</script>
