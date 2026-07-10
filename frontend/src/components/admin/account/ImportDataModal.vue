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

      <div>
        <label class="input-label">{{ t('admin.accounts.dataImportFile') }}</label>
        <div
          data-testid="account-data-dropzone"
          class="flex items-center justify-between gap-3 rounded-lg border border-dashed px-4 py-3 transition-colors"
          :class="isDragActive ? 'border-primary-400 bg-primary-50 dark:border-primary-500 dark:bg-primary-900/20' : 'border-gray-300 bg-gray-50 dark:border-dark-600 dark:bg-dark-800'"
          @dragenter.prevent="handleDragEnter"
          @dragover.prevent="handleDragOver"
          @dragleave.prevent="handleDragLeave"
          @drop.prevent="handleDrop"
        >
          <div class="min-w-0">
            <div class="truncate text-sm text-gray-700 dark:text-dark-200">
              {{ fileLabel || t('admin.accounts.dataImportSelectFile') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-dark-400">
              {{ isDragActive ? t('admin.accounts.dataImportDropFiles') : t('admin.accounts.dataImportFileTypes') }}
            </div>
          </div>
          <button type="button" class="btn btn-secondary shrink-0" @click="openFilePicker">
            {{ t('common.chooseFile') }}
          </button>
        </div>
        <input
          ref="fileInput"
          type="file"
          class="hidden"
          accept="application/json,text/plain,application/zip,.json,.jsonl,.txt,.zip"
          multiple
          @change="handleFileChange"
        />
      </div>

      <!-- Detected format badge -->
      <div v-if="detectedFormat" class="flex items-center gap-2">
        <span class="inline-flex items-center gap-1.5 rounded-full bg-primary-50 px-2.5 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
          <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          {{ t('admin.accounts.dataImportFormatDetected', { format: detectedFormatLabel }) }}
        </span>
        <span v-if="detectedFormat !== 'unknown'" class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.accounts.dataImportFormatConverted') }}
        </span>
      </div>

      <!-- Manual format override -->
      <div v-if="detectedFormat && detectedFormat !== 'unknown'">
        <label class="input-label">{{ t('admin.accounts.dataImportFormatOverride') }}</label>
        <select v-model="manualFormatOverride" class="input">
          <option value="auto">{{ t('admin.accounts.dataImportFormatAuto') }}</option>
          <option value="native">{{ t('admin.accounts.dataImportFormatNative') }}</option>
          <option value="cpa">CPA</option>
          <option value="sub2api">sub2api</option>
          <option value="codex2api">codex2api</option>
          <option value="codexmanager">Codex Manager</option>
          <option value="codex">Codex Auth</option>
          <option value="session">ChatGPT Session</option>
        </select>
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.dataImportURL') }}</label>
        <textarea
          v-model="sourceURLs"
          class="input"
          rows="3"
          :placeholder="t('admin.accounts.dataImportURLPlaceholder')"
        ></textarea>
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
        v-if="importing || importProgress.total > 0"
        class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-700"
      >
        <div class="flex items-center justify-between text-xs text-gray-600 dark:text-dark-300">
          <span>{{ t('admin.accounts.dataImportProgress') }}</span>
          <span>{{ importProgress.completed }} / {{ importProgress.total }}</span>
        </div>
        <div class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
          <div
            class="h-full rounded-full bg-primary-600 transition-all"
            :style="{ width: `${importProgressPercent}%` }"
          ></div>
        </div>
        <div class="truncate text-xs text-gray-500 dark:text-dark-400">
          {{ importProgress.current || t('admin.accounts.dataImportProgressIdle') }}
        </div>
      </div>

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
import { extractApiErrorMessage } from '@/utils/apiError'
import { detectFormat, convertToPayload, getInputFormatLabel } from '@/utils/authconv'
import type { AdminDataImportPayload, AdminDataImportResult, AdminGroup } from '@/types'
import type { InputFormat } from '@/utils/authconv'

type ImportFormatOverride = 'auto' | 'native' | InputFormat

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
const isDragActive = ref(false)
const files = ref<File[]>([])
const sourceURLs = ref('')
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
const importProgress = reactive({
  completed: 0,
  total: 0,
  current: ''
})

// Format detection state
const detectedFormat = ref<InputFormat | null>(null)
const manualFormatOverride = ref<ImportFormatOverride>('auto')

const fileInput = ref<HTMLInputElement | null>(null)
const fileLabel = computed(() => {
  if (files.value.length === 0) return ''
  if (files.value.length === 1) return files.value[0].name
  return t('admin.accounts.dataImportSelectedFiles', { count: files.value.length })
})

const detectedFormatLabel = computed(() => {
  if (!detectedFormat.value || detectedFormat.value === 'unknown') return ''
  return getInputFormatLabel(detectedFormat.value)
})

const errorItems = computed(() => result.value?.errors || [])
const importProgressPercent = computed(() => {
  if (importProgress.total <= 0) return 0
  return Math.min(100, Math.round((importProgress.completed / importProgress.total) * 100))
})

async function loadGroups() {
  try {
    groups.value = await adminAPI.groups.getAll()
  } catch {
    groups.value = []
  }
}

function resetImportProgress() {
  importProgress.completed = 0
  importProgress.total = 0
  importProgress.current = ''
}

function resetFormatState() {
  detectedFormat.value = null
  manualFormatOverride.value = 'auto'
}

watch(
  () => props.show,
  (open) => {
    if (open) {
      files.value = []
      sourceURLs.value = ''
      result.value = null
      isDragActive.value = false
      resetImportProgress()
      resetFormatState()
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

const selectImportFiles = async (selectedFiles: File[]) => {
  extracting.value = true
  resetFormatState()
  try {
    files.value = await expandSelectedImportFiles(selectedFiles)
    if (selectedFiles.length > 0 && files.value.length === 0) {
      appStore.showError(t('admin.accounts.dataImportZipNoImportableFiles'))
    }
    // Detect format from first file
    if (files.value.length > 0) {
      const firstText = await readFileAsText(files.value[0])
      try {
        const parsed = parseJSONOrJSONL(firstText)
        detectedFormat.value = detectFormat(parsed)
      } catch {
        detectedFormat.value = null
      }
    }
  } catch (error: any) {
    files.value = []
    appStore.showError(error?.message || t('admin.accounts.dataImportZipFailed'))
  } finally {
    extracting.value = false
  }
}

const handleFileChange = async (event: Event) => {
  const target = event.target as HTMLInputElement
  await selectImportFiles(Array.from(target.files || []))
}

const handleDragEnter = (event: DragEvent) => {
  if (event.dataTransfer?.types.includes('Files')) {
    isDragActive.value = true
  }
}

const handleDragOver = (event: DragEvent) => {
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'copy'
  }
}

const handleDragLeave = (event: DragEvent) => {
  const currentTarget = event.currentTarget as HTMLElement | null
  const relatedTarget = event.relatedTarget as Node | null
  if (!currentTarget || !relatedTarget || !currentTarget.contains(relatedTarget)) {
    isDragActive.value = false
  }
}

const handleDrop = async (event: DragEvent) => {
  isDragActive.value = false
  if (importing.value || extracting.value) return
  await selectImportFiles(Array.from(event.dataTransfer?.files || []))
  if (fileInput.value) {
    fileInput.value.value = ''
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

const isImportableDataFileName = (name: string) => /\.(json|jsonl|txt)$/i.test(name)

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

const parseJSONLines = (text: string): unknown[] => {
  const lines = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)

  if (lines.length === 0) {
    throw new SyntaxError('Empty JSONL input')
  }

  return lines.map((line) => JSON.parse(line))
}

const parseJSONOrJSONL = (text: string): unknown => {
  const normalizedText = text.replace(/^\uFEFF/, '')
  try {
    return JSON.parse(normalizedText)
  } catch (jsonError) {
    try {
      return parseJSONLines(normalizedText)
    } catch {
      throw jsonError
    }
  }
}

const mergeResult = (target: AdminDataImportResult, source: AdminDataImportResult) => {
  target.proxy_created += source.proxy_created
  target.proxy_reused += source.proxy_reused
  target.proxy_failed += source.proxy_failed
  target.account_created += source.account_created
  target.account_failed += source.account_failed
  target.errors = [...(target.errors || []), ...(source.errors || [])]
}

const parseAndConvertPayload = (text: string): AdminDataImportPayload => {
  // Try to parse as JSON/JSONL first
  let parsed: unknown
  try {
    parsed = parseJSONOrJSONL(text)
  } catch (error) {
    if (!compatibilityMode.value) {
      throw error
    }
    return text
  }

  // If manual override is set to native, pass through directly
  if (manualFormatOverride.value === 'native') {
    return parsed as AdminDataImportPayload
  }

  // Check if it's already a valid AdminDataPayload (has proxies + accounts arrays)
  if (
    parsed &&
    typeof parsed === 'object' &&
    !Array.isArray(parsed) &&
    Array.isArray((parsed as any).accounts)
  ) {
    const hasAdminFields = (parsed as any).accounts.some(
      (acc: any) => acc && typeof acc === 'object' && 'platform' in acc && 'type' in acc && 'credentials' in acc
    )
    if (hasAdminFields) {
      return parsed as AdminDataImportPayload
    }
  }

  // Try to detect and convert via authconv.
  const selectedInputFormat = manualFormatOverride.value
  const inputFormat = selectedInputFormat === 'auto'
    ? undefined
    : selectedInputFormat

  const converted = convertToPayload(parsed, {
    concurrency: overrideDefaults.value ? defaults.concurrency : 10,
    priority: overrideDefaults.value ? defaults.priority : 1,
    rate_multiplier: overrideDefaults.value ? defaults.rate_multiplier : 1,
    auto_pause_on_expired: overrideDefaults.value ? defaults.auto_pause_on_expired : true,
    inputFormat
  })

  if (converted) {
    return converted as AdminDataImportPayload
  }

  // Fallback: pass through as-is
  return parsed as AdminDataImportPayload
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

const parseSourceURLs = () => {
  return sourceURLs.value
    .split(/\r?\n/)
    .map((url) => url.trim())
    .filter(Boolean)
}

const handleImport = async () => {
  const remoteURLs = parseSourceURLs()
  if (files.value.length === 0 && remoteURLs.length === 0) {
    appStore.showError(t('admin.accounts.dataImportSelectFile'))
    return
  }

  importing.value = true
  try {
    const merged = emptyResult()
    const importOptions = commonImportOptions()
    importProgress.completed = 0
    importProgress.total = remoteURLs.length + files.value.length
    importProgress.current = ''
    for (const remoteURL of remoteURLs) {
      importProgress.current = remoteURL
      const res = await adminAPI.accounts.importData({
        source_url: remoteURL,
        ...importOptions
      })
      mergeResult(merged, res)
      importProgress.completed++
    }
    for (const sourceFile of files.value) {
      importProgress.current = sourceFile.name
      const text = await readFileAsText(sourceFile)
      const dataPayload = parseAndConvertPayload(text)

      const res = await adminAPI.accounts.importData({
        data: dataPayload,
        ...importOptions
      })
      mergeResult(merged, res)
      importProgress.completed++
    }
    importProgress.current = ''
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
      appStore.showError(extractApiErrorMessage(error, t('admin.accounts.dataImportFailed')))
    }
  } finally {
    importing.value = false
  }
}
</script>
