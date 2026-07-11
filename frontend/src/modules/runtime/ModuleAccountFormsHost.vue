<template>
  <div v-if="forms.length" class="inline-flex">
    <button class="btn btn-secondary" type="button" @click="open = true">{{ t('modules.runtime.accountButton') }}</button>
    <BaseDialog :show="open" :title="t('modules.runtime.accountTitle')" width="wide" @close="close">
      <div v-if="!selected" class="grid gap-3 sm:grid-cols-2">
        <button v-for="form in forms" :key="form.key" type="button" class="rounded-lg border border-gray-200 p-4 text-left hover:border-primary-400 dark:border-dark-600" @click="select(form)">
          <div class="font-medium text-gray-900 dark:text-white">{{ formDisplayName(form) }}</div>
          <div class="mt-1 text-xs text-gray-500">{{ form.providerId }}</div>
        </button>
      </div>
      <div v-else>
        <button type="button" class="mb-4 text-sm text-primary-600 hover:underline" @click="reset">← {{ t('modules.runtime.back') }}</button>
        <component v-if="component" :is="component" :contribution="selected" @created="created" @close="close" />
        <div v-else-if="error" class="rounded-lg bg-red-50 p-4 text-sm text-red-700">{{ error }}</div>
        <div v-else class="p-6 text-center text-sm text-gray-500">{{ t('modules.runtime.loadingForm') }}</div>
      </div>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, shallowRef, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { moduleAccountFormContributions, resolveModuleText, type RegisteredModuleAccountForm } from './registry'
import { loadModuleComponent } from './remoteLoader'

const emit = defineEmits<{ created: [] }>()
const { locale, t } = useI18n()
const forms = computed(() => moduleAccountFormContributions.value)
const open = ref(false)
const selected = shallowRef<RegisteredModuleAccountForm | null>(null)
const component = shallowRef<Component | null>(null)
const error = ref('')
let selectionGeneration = 0

function formDisplayName(form: RegisteredModuleAccountForm): string {
  const fallback = form.providerName || form.moduleName || form.providerId
  return resolveModuleText(fallback, form.providerNameI18n || form.moduleNameI18n, String(locale.value))
}

async function select(form: RegisteredModuleAccountForm) {
  const generation = ++selectionGeneration
  selected.value = form
  component.value = null
  error.value = ''
  try {
    const loaded = await loadModuleComponent(form.remoteEntry, form.exposedModule)
    if (generation === selectionGeneration) component.value = loaded
  } catch (cause) {
    if (generation === selectionGeneration) error.value = cause instanceof Error ? cause.message : t('modules.runtime.loadFormFailed')
  }
}
function reset() { ++selectionGeneration; selected.value = null; component.value = null; error.value = '' }
function close() { open.value = false; reset() }
function created() { emit('created'); close() }
</script>
