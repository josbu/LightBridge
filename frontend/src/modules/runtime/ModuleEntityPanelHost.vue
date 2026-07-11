<template>
  <BaseDialog :show="show" :title="dialogTitle" width="wide" @close="emit('close')">
    <component v-if="component && panel" :is="component" :entity="entity" :entity-id="entityId" :context="context" :contribution="panel" @close="emit('close')" @updated="emit('updated')" />
    <div v-else-if="error" class="rounded-lg bg-red-50 p-4 text-sm text-red-700">{{ error }}</div>
    <div v-else class="p-6 text-center text-sm text-gray-500">{{ t('modules.runtime.loadingPanel') }}</div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, shallowRef, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { loadModuleComponent } from './remoteLoader'
import { resolveModuleText, type RegisteredModuleEntityPanel } from './registry'

const props = defineProps<{ show: boolean; panel: RegisteredModuleEntityPanel | null; entity: string; entityId: string | number | null; context?: unknown }>()
const emit = defineEmits<{ close: []; updated: [] }>()
const { locale, t } = useI18n()
const dialogTitle = computed(() => props.panel
  ? resolveModuleText(props.panel.title, props.panel.title_i18n, String(locale.value))
  : t('modules.runtime.panelFallbackTitle'))
const component = shallowRef<Component | null>(null)
const error = shallowRef('')
let generation = 0

watch(() => [props.show, props.panel] as const, async ([show, panel]) => {
  const current = ++generation
  component.value = null
  error.value = ''
  if (!show || !panel) return
  try {
    const loaded = await loadModuleComponent(panel.remoteEntry, panel.exposedModule)
    if (current === generation) component.value = loaded
  } catch (cause) {
    if (current === generation) error.value = cause instanceof Error ? cause.message : t('modules.runtime.loadPanelFailed')
  }
}, { immediate: true })

onBeforeUnmount(() => { ++generation })
</script>
