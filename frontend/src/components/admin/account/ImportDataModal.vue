<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.dataImportTitle')"
    width="normal"
    close-on-click-outside
    @close="handleClose"
  >
    <form id="import-data-form" class="space-y-4" @submit.prevent="handleImport">
      <div class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.accounts.dataImportHint') }}
      </div>
      <div
        class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-600 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400"
      >
        {{ t('admin.accounts.dataImportWarning') }}
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.dataImportFile') }}</label>
        <div
          class="flex items-center justify-between gap-3 rounded-lg border border-dashed border-gray-300 bg-gray-50 px-4 py-3 dark:border-dark-600 dark:bg-dark-800"
        >
          <div class="min-w-0">
            <div class="truncate text-sm text-gray-700 dark:text-dark-200">
              {{ fileLabel || t('admin.accounts.dataImportSelectFile') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-dark-400">JSON/TXT/ZIP (.json, .txt, .zip)</div>
          </div>
          <button type="button" class="btn btn-secondary shrink-0" @click="openFilePicker">
            {{ t('common.chooseFile') }}
          </button>
        </div>
        <input
          ref="fileInput"
          type="file"
          class="hidden"
          accept="application/json,text/plain,application/zip,.json,.txt,.zip"
          multiple
          @change="handleFileChange"
        />
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.dataImportURL') }}</label>
        <input
          v-model.trim="sourceURL"
          type="url"
          class="input"
          :placeholder="t('admin.accounts.dataImportURLPlaceholder')"
        />
        <p class="input-hint">{{ t('admin.accounts.dataImportURLHint') }}</p>
      </div>

      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.accounts.dataImportGroups') }}</label>
          <select v-model="selectedGroupIds" multiple class="input min-h-28">
            <option v-for="group in groups" :key="group.id" :value="group.id">
              {{ group.name }}
            </option>
          </select>
          <p class="input-hint">{{ t('admin.accounts.dataImportGroupsHint') }}</p>
        </div>

        <div class="space-y-3">
          <label class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-dark-200">
            <input v-model="overrideDefaults" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('admin.accounts.dataImportBatchSettings') }}
          </label>
          <div class="grid grid-cols-2 gap-2" :class="{ 'opacity-50': !overrideDefaults }">
            <label class="block">
              <span class="input-label">{{ t('admin.accounts.concurrency') }}</span>
              <input v-model.number="defaults.concurrency" :disabled="!overrideDefaults" type="number" min="0" class="input" />
            </label>
            <label class="block">
              <span class="input-label">{{ t('admin.accounts.priority') }}</span>
              <input v-model.number="defaults.priority" :disabled="!overrideDefaults" type="number" min="0" class="input" />
            </label>
            <label class="block">
              <span class="input-label">{{ t('admin.accounts.dataImportRateMultiplier') }}</span>
              <input v-model.number="defaults.rate_multiplier" :disabled="!overrideDefaults" type="number" min="0" step="0.001" class="input" />
            </label>
            <label class="mt-6 flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="defaults.auto_pause_on_expired" :disabled="!overrideDefaults" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              {{ t('admin.accounts.dataImportAutoPause') }}
            </label>
          </div>
        </div>
      </div>

      <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
        <input
          v-model="compatibilityMode"
          type="checkbox"
          class="mt-1 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
        />
        <span class="min-w-0">
          <span class="block text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.accounts.dataImportCompatibilityMode') }}
          </span>
          <span class="block text-xs leading-5 text-gray-500 dark:text-dark-400">
            {{ t('admin.accounts.dataImportCompatibilityModeHint') }}
          </span>
        </span>
      </label>

      <div
        v-if="result"
        class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
      >
        <div class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.accounts.dataImportResult') }}
        </div>
        <div class="text-sm text-gray-700 dark:text-dark-300">
          {{ t('admin.accounts.dataImportResultSummary', result) }}
        </div>

        <div v-if="errorItems.length" class="mt-2">
          <div class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t('admin.accounts.dataImportErrors') }}
          </div>
          <div
            class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800"
          >
            <div v-for="(item, idx) in errorItems" :key="idx" class="whitespace-pre-wrap">
              {{ item.kind }} {{ item.name || item.proxy_key || '-' }} — {{ item.message }}
            </div>
          </div>
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" type="button" :disabled="importing || extracting" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary"
          type="submit"
          form="import-data-form"
          :disabled="importing || extracting"
        >
          {{ importing || extracting ? t('admin.accounts.dataImporting') : t('admin.accounts.dataImportButton') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { AdminDataImportPayload, AdminDataImportResult, AdminGroup } from '@/types'

interface Props {
  show: boolean
}

interface Emits {
  (e: 'close'): void
  (e: 'imported'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

const importing = ref(false)
const extracting = ref(false)
const files = ref<File[]>([])
const sourceURL = ref('')
const result = ref<AdminDataImportResult | null>(null)
const compatibilityMode = ref(false)
const groups = ref<AdminGroup[]>([])
const selectedGroupIds = ref<number[]>([])
const overrideDefaults = ref(false)
const defaults = reactive({
  concurrency: 10,
  priority: 1,
  rate_multiplier: 1,
  auto_pause_on_expired: true
})

const fileInput = ref<HTMLInputElement | null>(null)
const fileLabel = computed(() => {
  if (files.value.length === 0) return ''
  if (files.value.length === 1) return files.value[0].name
  return t('admin.accounts.dataImportSelectedFiles', { count: files.value.length })
})

const errorItems = computed(() => result.value?.errors || [])

async function loadGroups() {
  try {
    groups.value = await adminAPI.groups.getByPlatform('openai')
  } catch {
    groups.value = []
  }
}

watch(
  () => props.show,
  (open) => {
    if (open) {
      files.value = []
      sourceURL.value = ''
      result.value = null
      compatibilityMode.value = false
      selectedGroupIds.value = []
      overrideDefaults.value = false
      if (fileInput.value) {
        fileInput.value.value = ''
      }
      loadGroups()
    }
  },
  { immediate: true }
)

const openFilePicker = () => {
  fileInput.value?.click()
}

const handleFileChange = async (event: Event) => {
  const target = event.target as HTMLInputElement
  extracting.value = true
  try {
    files.value = await expandSelectedImportFiles(Array.from(target.files || []))
    if (target.files && target.files.length > 0 && files.value.length === 0) {
      appStore.showError(t('admin.accounts.dataImportZipNoImportableFiles'))
    }
  } catch (error: any) {
    files.value = []
    appStore.showError(error?.message || t('admin.accounts.dataImportZipFailed'))
  } finally {
    extracting.value = false
  }
}

const handleClose = () => {
  if (importing.value || extracting.value) return
  emit('close')
}

const readFileAsText = async (sourceFile: File): Promise<string> => {
  if (typeof sourceFile.text === 'function') {
    return sourceFile.text()
  }

  if (typeof sourceFile.arrayBuffer === 'function') {
    const buffer = await sourceFile.arrayBuffer()
    return new TextDecoder().decode(buffer)
  }

  return await new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error || new Error('Failed to read file'))
    reader.readAsText(sourceFile)
  })
}

const isImportableDataFileName = (name: string) => /\.(json|txt)$/i.test(name)

const isZipFile = (file: File) => {
  const name = file.name.toLowerCase()
  return name.endsWith('.zip') || file.type === 'application/zip' || file.type === 'application/x-zip-compressed'
}

const expandSelectedImportFiles = async (selectedFiles: File[]): Promise<File[]> => {
  const expanded: File[] = []
  for (const selectedFile of selectedFiles) {
    if (isZipFile(selectedFile)) {
      expanded.push(...await extractImportableFilesFromZip(selectedFile))
      continue
    }
    if (isImportableDataFileName(selectedFile.name)) {
      expanded.push(selectedFile)
    }
  }
  return expanded
}

const readUint16 = (view: DataView, offset: number) => view.getUint16(offset, true)
const readUint32 = (view: DataView, offset: number) => view.getUint32(offset, true)

const decodeZipName = (bytes: Uint8Array) => new TextDecoder().decode(bytes)

const findZipEndOfCentralDirectory = (view: DataView) => {
  const minOffset = Math.max(0, view.byteLength - 0xffff - 22)
  for (let offset = view.byteLength - 22; offset >= minOffset; offset--) {
    if (readUint32(view, offset) === 0x06054b50) {
      return offset
    }
  }
  return -1
}

const inflateRaw = async (bytes: Uint8Array): Promise<Uint8Array> => {
  const DecompressionStreamCtor = (globalThis as any).DecompressionStream
  if (!DecompressionStreamCtor) {
    throw new Error(t('admin.accounts.dataImportZipUnsupportedCompression'))
  }
  const stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStreamCtor('deflate-raw'))
  return new Uint8Array(await new Response(stream).arrayBuffer())
}

const extractImportableFilesFromZip = async (zipFile: File): Promise<File[]> => {
  const buffer = await zipFile.arrayBuffer()
  const bytes = new Uint8Array(buffer)
  const view = new DataView(buffer)
  const eocdOffset = findZipEndOfCentralDirectory(view)
  if (eocdOffset < 0) {
    throw new Error(t('admin.accounts.dataImportZipFailed'))
  }

  const entryCount = readUint16(view, eocdOffset + 10)
  let offset = readUint32(view, eocdOffset + 16)
  const extracted: File[] = []

  for (let index = 0; index < entryCount; index++) {
    if (offset + 46 > view.byteLength || readUint32(view, offset) !== 0x02014b50) {
      throw new Error(t('admin.accounts.dataImportZipFailed'))
    }

    const flags = readUint16(view, offset + 8)
    const method = readUint16(view, offset + 10)
    const compressedSize = readUint32(view, offset + 20)
    const nameLength = readUint16(view, offset + 28)
    const extraLength = readUint16(view, offset + 30)
    const commentLength = readUint16(view, offset + 32)
    const localHeaderOffset = readUint32(view, offset + 42)
    const nameStart = offset + 46
    const name = decodeZipName(bytes.slice(nameStart, nameStart + nameLength))
    offset = nameStart + nameLength + extraLength + commentLength

    if (!isImportableDataFileName(name) || name.endsWith('/')) {
      continue
    }
    if ((flags & 0x1) !== 0) {
      throw new Error(t('admin.accounts.dataImportZipEncrypted'))
    }
    if (localHeaderOffset + 30 > view.byteLength || readUint32(view, localHeaderOffset) !== 0x04034b50) {
      throw new Error(t('admin.accounts.dataImportZipFailed'))
    }

    const localNameLength = readUint16(view, localHeaderOffset + 26)
    const localExtraLength = readUint16(view, localHeaderOffset + 28)
    const dataStart = localHeaderOffset + 30 + localNameLength + localExtraLength
    const compressedData = bytes.slice(dataStart, dataStart + compressedSize)
    let content: Uint8Array
    if (method === 0) {
      content = compressedData
    } else if (method === 8) {
      content = await inflateRaw(compressedData)
    } else {
      throw new Error(t('admin.accounts.dataImportZipUnsupportedCompression'))
    }

    const pathParts = name.split('/').filter(Boolean)
    const fileName = pathParts[pathParts.length - 1] || name
    extracted.push(new File([content], fileName, {
      type: fileName.toLowerCase().endsWith('.json') ? 'application/json' : 'text/plain'
    }))
  }

  return extracted
}

const emptyResult = (): AdminDataImportResult => ({
  proxy_created: 0,
  proxy_reused: 0,
  proxy_failed: 0,
  account_created: 0,
  account_failed: 0,
  errors: []
})

const mergeResult = (target: AdminDataImportResult, source: AdminDataImportResult) => {
  target.proxy_created += source.proxy_created
  target.proxy_reused += source.proxy_reused
  target.proxy_failed += source.proxy_failed
  target.account_created += source.account_created
  target.account_failed += source.account_failed
  target.errors = [...(target.errors || []), ...(source.errors || [])]
}

const parseImportPayload = (text: string): AdminDataImportPayload => {
  try {
    return JSON.parse(text)
  } catch (error) {
    if (!compatibilityMode.value) {
      throw error
    }
    return text
  }
}

const accountDefaultsPayload = () => {
  if (!overrideDefaults.value) return undefined
  return {
    concurrency: Math.max(0, Number(defaults.concurrency) || 0),
    priority: Math.max(0, Number(defaults.priority) || 0),
    rate_multiplier: Math.max(0, Number(defaults.rate_multiplier) || 0),
    auto_pause_on_expired: defaults.auto_pause_on_expired
  }
}

const selectedImportGroupIds = () => selectedGroupIds.value.map((id) => Number(id)).filter((id) => Number.isInteger(id) && id > 0)

const commonImportOptions = () => {
  const groupIds = selectedImportGroupIds()
  return {
    skip_default_group_bind: groupIds.length === 0,
    compatibility_mode: compatibilityMode.value,
    group_ids: groupIds,
    account_defaults: accountDefaultsPayload()
  }
}

const handleImport = async () => {
  const remoteURL = sourceURL.value.trim()
  if (files.value.length === 0 && !remoteURL) {
    appStore.showError(t('admin.accounts.dataImportSelectFile'))
    return
  }

  importing.value = true
  try {
    const merged = emptyResult()
    const importOptions = commonImportOptions()
    if (remoteURL) {
      const res = await adminAPI.accounts.importData({
        source_url: remoteURL,
        ...importOptions
      })
      mergeResult(merged, res)
    }
    for (const sourceFile of files.value) {
      const text = await readFileAsText(sourceFile)
      const dataPayload = parseImportPayload(text)

      const res = await adminAPI.accounts.importData({
        data: dataPayload,
        ...importOptions
      })
      mergeResult(merged, res)
    }
    result.value = merged

    const msgParams: Record<string, unknown> = {
      account_created: merged.account_created,
      account_failed: merged.account_failed,
      proxy_created: merged.proxy_created,
      proxy_reused: merged.proxy_reused,
      proxy_failed: merged.proxy_failed,
    }
    if (merged.account_failed > 0 || merged.proxy_failed > 0) {
      appStore.showError(t('admin.accounts.dataImportCompletedWithErrors', msgParams))
    } else {
      appStore.showSuccess(t('admin.accounts.dataImportSuccess', msgParams))
      emit('imported')
    }
  } catch (error: any) {
    if (error instanceof SyntaxError) {
      appStore.showError(t('admin.accounts.dataImportParseFailed'))
    } else {
      appStore.showError(error?.message || t('admin.accounts.dataImportFailed'))
    }
  } finally {
    importing.value = false
  }
}
</script>
