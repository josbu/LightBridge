<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-wrap items-center justify-end gap-2">
        <div class="min-w-[240px] flex-1 sm:max-w-md">
          <div class="relative">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input
              v-model="searchQuery"
              type="text"
              class="input w-full pl-9 text-sm"
              :placeholder="t('admin.ops.errorAnalysis.searchPlaceholder')"
            />
          </div>
        </div>
        <div class="w-36">
          <Select :model-value="timeRange" :options="timeRangeOptions" @update:model-value="timeRange = String($event || '24h')" />
        </div>
        <div class="w-32">
          <Select :model-value="statusCodeFilter" :options="statusOptions" @update:model-value="statusCodeFilter = String($event || '')" />
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loadingList" @click="fetchRequestErrors({ keepSelection: false })">
          <Icon name="refresh" size="sm" :class="loadingList ? 'animate-spin' : ''" />
          <span>{{ t('common.refresh') }}</span>
        </button>
      </div>

      <div class="grid min-h-[720px] grid-cols-1 gap-5 xl:grid-cols-[440px_minmax(0,1fr)]">
        <section class="flex min-h-0 flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
          <div class="border-b border-gray-200 p-4 dark:border-dark-700">
            <div>
              <h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.ops.errorAnalysis.requestList') }}</h2>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.ops.errorAnalysis.total', { total }) }}
              </p>
            </div>
          </div>

          <div class="min-h-0 flex-1 overflow-auto">
            <div v-if="loadingList" class="flex h-full min-h-[320px] items-center justify-center">
              <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
            </div>

            <div v-else-if="requestErrors.length === 0" class="flex h-full min-h-[320px] flex-col items-center justify-center px-8 text-center">
              <Icon name="inbox" size="xl" class="text-gray-300 dark:text-dark-500" />
              <div class="mt-3 text-sm font-semibold text-gray-700 dark:text-gray-200">{{ t('admin.ops.errorAnalysis.emptyTitle') }}</div>
              <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.errorAnalysis.emptyHint') }}</div>
            </div>

            <template v-else>
              <button
                v-for="item in requestErrors"
                :key="item.id"
                type="button"
                :class="[
                  'block w-full border-b border-gray-100 px-4 py-3 text-left transition-colors dark:border-dark-800',
                  selectedErrorId === item.id
                    ? 'bg-primary-50 dark:bg-primary-900/20'
                    : 'hover:bg-gray-50 dark:hover:bg-dark-800/70'
                ]"
                @click="selectError(item.id)"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="flex min-w-0 items-center gap-2">
                      <span :class="['inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[10px] font-black ring-1 ring-inset', statusClass(item.status_code)]">
                        {{ item.status_code }}
                      </span>
                      <span class="truncate text-xs font-bold text-gray-900 dark:text-white">
                        {{ item.phase || '-' }} / {{ item.error_owner || '-' }}
                      </span>
                    </div>
                    <div class="mt-1 truncate font-mono text-[11px] text-gray-500 dark:text-gray-400">
                      {{ item.request_id || item.client_request_id || '-' }}
                    </div>
                  </div>
                  <div class="shrink-0 text-right text-[11px] text-gray-400 dark:text-dark-400">
                    {{ formatDateTime(item.created_at) }}
                  </div>
                </div>

                <div class="mt-2 line-clamp-2 text-xs text-gray-600 dark:text-gray-300">
                  {{ shortErrorMessage(item) || '-' }}
                </div>

                <div class="mt-2 flex flex-wrap gap-1.5">
                  <span class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-semibold text-gray-600 dark:bg-dark-700 dark:text-dark-200">
                    {{ item.platform || '-' }}
                  </span>
                  <span class="max-w-[160px] truncate rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-mono font-semibold text-gray-600 dark:bg-dark-700 dark:text-dark-200">
                    {{ displayModel(item) || '-' }}
                  </span>
                  <span v-if="item.group_name || item.group_id" class="max-w-[130px] truncate rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-semibold text-gray-600 dark:bg-dark-700 dark:text-dark-200">
                    {{ item.group_name || item.group_id }}
                  </span>
                </div>
              </button>
            </template>
          </div>

          <div
            v-if="total > 0"
            class="border-t border-gray-200 bg-white px-3 py-3 dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="flex flex-col gap-2">
              <div class="whitespace-nowrap text-xs text-gray-600 dark:text-gray-300">
                {{ t('pagination.showing') }}
                <span class="font-semibold">{{ fromItem }}</span>
                {{ t('pagination.to') }}
                <span class="font-semibold">{{ toItem }}</span>
                {{ t('pagination.of') }}
                <span class="font-semibold">{{ total }}</span>
                {{ t('pagination.results') }}
              </div>
              <div class="flex items-center justify-between gap-2">
                <button
                  type="button"
                  class="inline-flex h-8 w-8 items-center justify-center rounded border border-gray-300 bg-white text-gray-500 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600"
                  :disabled="page === 1"
                  :aria-label="t('pagination.previous')"
                  @click="handlePageChange(page - 1)"
                >
                  <Icon name="chevronLeft" size="sm" />
                </button>
                <div class="flex min-w-0 items-center justify-center gap-1 overflow-hidden">
                  <button
                    v-for="(pageNum, pageIndex) in compactVisiblePages"
                    :key="`${pageNum}-${pageIndex}`"
                    type="button"
                    :disabled="typeof pageNum !== 'number'"
                    :class="[
                      'inline-flex h-8 min-w-8 items-center justify-center rounded border px-2 text-xs font-semibold',
                      pageNum === page
                        ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                        : 'border-gray-300 bg-white text-gray-600 hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600',
                      typeof pageNum !== 'number' && 'cursor-default border-transparent bg-transparent px-1 text-gray-400 hover:bg-transparent dark:bg-transparent'
                    ]"
                    @click="typeof pageNum === 'number' && handlePageChange(pageNum)"
                  >
                    {{ pageNum }}
                  </button>
                </div>
                <button
                  type="button"
                  class="inline-flex h-8 w-8 items-center justify-center rounded border border-gray-300 bg-white text-gray-500 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600"
                  :disabled="page === totalPages"
                  :aria-label="t('pagination.next')"
                  @click="handlePageChange(page + 1)"
                >
                  <Icon name="chevronRight" size="sm" />
                </button>
              </div>
            </div>
          </div>
        </section>

        <section class="min-h-0 overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
          <div v-if="loadingDetail" class="flex h-full min-h-[520px] items-center justify-center">
            <div class="flex flex-col items-center gap-3">
              <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
              <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ops.errorAnalysis.loadingAnalysis') }}</span>
            </div>
          </div>

          <div v-else-if="!selectedDetail" class="flex h-full min-h-[520px] flex-col items-center justify-center px-8 text-center">
            <Icon name="lightbulb" size="xl" class="text-gray-300 dark:text-dark-500" />
            <div class="mt-3 text-sm font-semibold text-gray-700 dark:text-gray-200">{{ t('admin.ops.errorAnalysis.noSelectionTitle') }}</div>
            <div class="mt-1 max-w-md text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.errorAnalysis.noSelectionHint') }}</div>
          </div>

          <div v-else class="h-full overflow-auto">
            <div class="border-b border-gray-200 p-5 dark:border-dark-700">
              <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <span :class="['inline-flex items-center rounded px-2 py-1 text-xs font-black ring-1 ring-inset', statusClass(selectedDetail.status_code)]">
                      {{ selectedDetail.status_code }}
                    </span>
                    <span class="rounded bg-gray-100 px-2 py-1 text-xs font-bold uppercase text-gray-600 dark:bg-dark-800 dark:text-gray-300">
                      {{ selectedDetail.phase || '-' }}
                    </span>
                    <span class="rounded bg-gray-100 px-2 py-1 text-xs font-bold uppercase text-gray-600 dark:bg-dark-800 dark:text-gray-300">
                      {{ selectedDetail.error_owner || '-' }}
                    </span>
                  </div>
                  <h2 class="mt-3 text-xl font-bold text-gray-900 dark:text-white">
                    {{ t(`admin.ops.errorAnalysis.rootCause.${analysis.rootCause}`) }}
                  </h2>
                  <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
                    {{ t(`admin.ops.errorAnalysis.rootCauseDesc.${analysis.rootCause}`) }}
                  </p>
                </div>

                <div class="grid min-w-[260px] grid-cols-2 gap-2">
                  <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
                    <div class="text-[10px] font-bold uppercase text-gray-400">{{ t('admin.ops.errorAnalysis.rootModule') }}</div>
                    <div class="mt-1 break-all font-mono text-xs font-bold text-gray-900 dark:text-white">{{ analysis.rootModule }}</div>
                  </div>
                  <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
                    <div class="text-[10px] font-bold uppercase text-gray-400">{{ t('admin.ops.errorAnalysis.confidence') }}</div>
                    <div class="mt-1 text-xs font-bold text-gray-900 dark:text-white">
                      {{ t(`admin.ops.errorAnalysis.confidenceLevel.${analysis.confidence}`) }}
                    </div>
                  </div>
                </div>
              </div>

              <div class="mt-4 grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-4">
                <div v-for="ev in analysis.evidence" :key="`${ev.key}-${ev.value}`" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
                  <div class="text-[10px] font-bold uppercase text-gray-400">{{ evidenceLabel(ev.key) }}</div>
                  <div :class="['mt-1 break-all text-xs font-semibold', evidenceToneClass(ev.tone)]">{{ ev.value }}</div>
                </div>
              </div>
            </div>

            <div class="grid grid-cols-1 gap-5 p-5 2xl:grid-cols-[minmax(0,1fr)_360px]">
              <div class="space-y-5">
                <div>
                  <div class="mb-3 flex items-center justify-between gap-3">
                    <h3 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.ops.errorAnalysis.stepFlow') }}</h3>
                    <span class="text-xs text-gray-500 dark:text-gray-400">
                      {{ t('admin.ops.errorAnalysis.failedAt', { module: analysis.rootModule }) }}
                    </span>
                  </div>

                  <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-7">
                    <div
                      v-for="(step, idx) in analysis.steps"
                      :key="step.key"
                      :class="[
                        'relative rounded-lg border p-3',
                        stepCardClass(step.state)
                      ]"
                    >
                      <div class="flex items-center justify-between gap-2">
                        <div :class="['flex h-7 w-7 items-center justify-center rounded-full', stepIconClass(step.state)]">
                          <Icon :name="stepIconName(step.state)" size="sm" :stroke-width="2" />
                        </div>
                        <div class="text-[10px] font-black uppercase text-gray-400">#{{ idx + 1 }}</div>
                      </div>
                      <div class="mt-3 text-sm font-bold text-gray-900 dark:text-white">{{ t(`admin.ops.errorAnalysis.steps.${step.key}`) }}</div>
                      <div class="mt-1 break-all font-mono text-[10px] text-gray-500 dark:text-gray-400">{{ step.module }}</div>
                      <div :class="['mt-3 inline-flex rounded px-1.5 py-0.5 text-[10px] font-black', stateBadgeClass(step.state)]">
                        {{ t(`admin.ops.errorAnalysis.stepState.${step.state}`) }}
                      </div>
                    </div>
                  </div>
                </div>

                <div class="space-y-3">
                  <h3 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.ops.errorAnalysis.stepDetails') }}</h3>
                  <div
                    v-for="step in analysis.steps"
                    :key="`detail-${step.key}`"
                    :class="[
                      'rounded-lg border p-4',
                      stepCardClass(step.state)
                    ]"
                  >
                    <div class="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                      <div>
                        <div class="flex items-center gap-2">
                          <div :class="['flex h-7 w-7 items-center justify-center rounded-full', stepIconClass(step.state)]">
                            <Icon :name="stepIconName(step.state)" size="sm" :stroke-width="2" />
                          </div>
                          <div class="text-sm font-bold text-gray-900 dark:text-white">{{ t(`admin.ops.errorAnalysis.steps.${step.key}`) }}</div>
                        </div>
                        <div class="mt-1 break-all pl-9 font-mono text-xs text-gray-500 dark:text-gray-400">{{ step.module }}</div>
                      </div>
                      <span :class="['self-start rounded px-2 py-1 text-[10px] font-black', stateBadgeClass(step.state)]">
                        {{ t(`admin.ops.errorAnalysis.stepState.${step.state}`) }}
                      </span>
                    </div>

                    <div class="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
                      <div
                        v-for="ev in step.evidence"
                        :key="`${step.key}-${ev.key}-${ev.value}`"
                        class="rounded border border-gray-200 bg-white px-3 py-2 dark:border-dark-700 dark:bg-dark-900"
                      >
                        <div class="text-[10px] font-bold uppercase text-gray-400">{{ evidenceLabel(ev.key) }}</div>
                        <div :class="['mt-1 break-all text-xs font-semibold', evidenceToneClass(ev.tone)]">{{ ev.value }}</div>
                      </div>
                      <div v-if="step.evidence.length === 0" class="rounded border border-dashed border-gray-200 px-3 py-2 text-xs text-gray-400 dark:border-dark-700">
                        {{ t('admin.ops.errorAnalysis.noEvidence') }}
                      </div>
                    </div>

                    <div v-if="step.key === 'account_scheduler'" class="mt-4 rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
                      <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                        <div>
                          <div class="text-xs font-bold text-gray-900 dark:text-white">
                            {{ t('admin.ops.errorAnalysis.schedulerAccounts.title') }}
                          </div>
                          <div class="mt-0.5 text-[11px] text-gray-500 dark:text-gray-400">
                            {{ selectedDetail?.group_name || selectedDetail?.group_id || '-' }} ·
                            {{ t('admin.ops.errorAnalysis.schedulerAccounts.availableCount', { available: availableSchedulerAccountCount, total: schedulerAccountDiagnostics.length }) }}
                          </div>
                        </div>
                        <div v-if="loadingSchedulerAccounts" class="flex items-center gap-2 text-[11px] font-semibold text-gray-500 dark:text-gray-400">
                          <Icon name="refresh" size="xs" class="animate-spin" />
                          <span>{{ t('admin.ops.errorAnalysis.schedulerAccounts.loading') }}</span>
                        </div>
                      </div>

                      <div v-if="!loadingSchedulerAccounts && schedulerAccountDiagnostics.length === 0" class="mt-3 rounded border border-dashed border-gray-200 px-3 py-2 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
                        {{ selectedDetail?.group_id ? t('admin.ops.errorAnalysis.schedulerAccounts.empty') : t('admin.ops.errorAnalysis.schedulerAccounts.noGroup') }}
                      </div>

                      <div v-else class="mt-3 space-y-2">
                        <div
                          v-if="!loadingSchedulerAccounts && availableSchedulerAccountCount === 0 && schedulerAccountDiagnostics.length > 0"
                          class="rounded border border-red-200 bg-red-50 px-3 py-2 text-xs font-semibold text-red-700 dark:border-red-900/50 dark:bg-red-900/10 dark:text-red-300"
                        >
                          {{ t('admin.ops.errorAnalysis.schedulerAccounts.noneAvailable') }}
                        </div>

                        <div
                          v-for="diagnostic in schedulerAccountDiagnostics"
                          :key="diagnostic.account.id"
                          class="rounded border border-gray-200 px-3 py-2 dark:border-dark-700"
                        >
                          <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                            <div class="min-w-0">
                              <div class="flex min-w-0 items-center gap-2">
                                <span :class="['inline-flex shrink-0 rounded px-1.5 py-0.5 text-[10px] font-black ring-1 ring-inset', diagnostic.available ? 'bg-emerald-50 text-emerald-700 ring-emerald-600/20 dark:bg-emerald-900/30 dark:text-emerald-300 dark:ring-emerald-500/30' : 'bg-red-50 text-red-700 ring-red-600/20 dark:bg-red-900/30 dark:text-red-300 dark:ring-red-500/30']">
                                  {{ diagnostic.available ? t('admin.ops.errorAnalysis.schedulerAccounts.available') : t('admin.ops.errorAnalysis.schedulerAccounts.unavailable') }}
                                </span>
                                <span class="truncate text-xs font-bold text-gray-900 dark:text-white">
                                  {{ accountDisplayLabel(diagnostic.account) }}
                                </span>
                              </div>
                              <div class="mt-1 flex flex-wrap gap-1.5 text-[10px] text-gray-500 dark:text-gray-400">
                                <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-800">#{{ diagnostic.account.id }}</span>
                                <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-800">{{ diagnostic.account.platform }}</span>
                                <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-800">{{ diagnostic.account.status }}</span>
                              </div>
                            </div>
                            <div class="text-[11px] text-gray-500 dark:text-gray-400">
                              {{ formatAccountCapacity(diagnostic.account) }}
                            </div>
                          </div>

                          <div v-if="diagnostic.reasons.length > 0" class="mt-2 space-y-1">
                            <div
                              v-for="reason in diagnostic.reasons"
                              :key="`${diagnostic.account.id}-${reason.key}-${reason.detail || ''}`"
                              class="rounded bg-red-50 px-2 py-1 text-[11px] text-red-700 dark:bg-red-900/10 dark:text-red-300"
                            >
                              {{ accountReasonLabel(reason) }}
                            </div>
                          </div>
                          <div v-else class="mt-2 rounded bg-emerald-50 px-2 py-1 text-[11px] text-emerald-700 dark:bg-emerald-900/10 dark:text-emerald-300">
                            {{ t('admin.ops.errorAnalysis.schedulerAccounts.noBlockingReason') }}
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>

                <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800">
                  <div class="flex items-center justify-between gap-2">
                    <h3 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.ops.errorAnalysis.rawResponse') }}</h3>
                    <button type="button" class="text-xs font-bold text-primary-600 hover:text-primary-700 dark:text-primary-400" @click="showRaw = !showRaw">
                      {{ showRaw ? t('admin.ops.errorAnalysis.hideRaw') : t('admin.ops.errorAnalysis.showRaw') }}
                    </button>
                  </div>
                  <pre v-if="showRaw" class="mt-3 max-h-[360px] overflow-auto rounded-lg border border-gray-200 bg-white p-3 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-100"><code>{{ prettyJSON(primaryResponseBody) }}</code></pre>
                </div>
              </div>

              <aside class="space-y-4">
                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                  <h3 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.ops.errorAnalysis.suggestions') }}</h3>
                  <div class="mt-3 space-y-2">
                    <div
                      v-for="key in analysis.suggestionKeys"
                      :key="key"
                      class="flex gap-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300"
                    >
                      <Icon name="checkCircle" size="sm" class="mt-0.5 shrink-0 text-primary-600 dark:text-primary-400" :stroke-width="2" />
                      <span>{{ t(`admin.ops.errorAnalysis.suggestion.${key}`) }}</span>
                    </div>
                  </div>
                </div>

                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                  <h3 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.ops.errorAnalysis.upstreamAttempts') }}</h3>
                  <div v-if="correlatedUpstreamErrors.length === 0" class="mt-3 rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                    {{ t('admin.ops.errorAnalysis.noUpstreamAttempts') }}
                  </div>
                  <div v-else class="mt-3 space-y-2">
                    <div
                      v-for="(item, idx) in correlatedUpstreamErrors"
                      :key="item.id"
                      class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
                    >
                      <div class="flex items-center justify-between gap-2">
                        <div class="text-xs font-black text-gray-900 dark:text-white">#{{ idx + 1 }}</div>
                        <span :class="['rounded px-1.5 py-0.5 text-[10px] font-black ring-1 ring-inset', statusClass(item.status_code)]">
                          {{ item.status_code || '-' }}
                        </span>
                      </div>
                      <div class="mt-2 break-all text-xs font-semibold text-gray-700 dark:text-gray-200">
                        {{ item.account_name || item.account_id || '-' }}
                      </div>
                      <div class="mt-1 line-clamp-3 text-xs text-gray-500 dark:text-gray-400">
                        {{ shortErrorMessage(item) || '-' }}
                      </div>
                    </div>
                  </div>
                </div>
              </aside>
            </div>
          </div>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import { useAppStore } from '@/stores'
import { adminAPI } from '@/api/admin'
import { opsAPI, type OpsErrorDetail, type OpsErrorLog, type OpsErrorListQueryParams } from '@/api/admin/ops'
import type { Account } from '@/types'
import { formatDateTime } from './utils/opsFormatters'
import {
  accountDisplayLabel,
  buildErrorAnalysis,
  diagnoseSchedulerAccounts,
  shortErrorMessage,
  type ErrorAnalysisAccountDiagnostic,
  type ErrorAnalysisAccountReason,
  type ErrorAnalysisStepState
} from './utils/errorAnalysis'
import { resolvePrimaryResponseBody } from './utils/errorDetailResponse'

const { t } = useI18n()
const appStore = useAppStore()

const loadingList = ref(false)
const loadingDetail = ref(false)
const requestErrors = ref<OpsErrorLog[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const timeRange = ref('24h')
const statusCodeFilter = ref('')
const searchQuery = ref('')

const selectedErrorId = ref<number | null>(null)
const selectedDetail = ref<OpsErrorDetail | null>(null)
const correlatedUpstreamErrors = ref<OpsErrorDetail[]>([])
const showRaw = ref(false)
const loadingSchedulerAccounts = ref(false)
const schedulerAccounts = ref<Account[]>([])

let listFetchSeq = 0
let detailFetchSeq = 0
let schedulerAccountFetchSeq = 0

const timeRangeOptions = computed(() => [
  { value: '5m', label: t('admin.ops.timeRange.5m') },
  { value: '30m', label: t('admin.ops.timeRange.30m') },
  { value: '1h', label: t('admin.ops.timeRange.1h') },
  { value: '6h', label: t('admin.ops.timeRange.6h') },
  { value: '24h', label: t('admin.ops.timeRange.24h') },
  { value: '7d', label: t('admin.ops.timeRange.7d') },
  { value: '30d', label: t('admin.ops.timeRange.30d') }
])

const statusOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: '403', label: '403' },
  { value: '429', label: '429' },
  { value: '500', label: '500' },
  { value: '502', label: '502' },
  { value: '503', label: '503' },
  { value: '504', label: '504' }
])

const analysis = computed(() => buildErrorAnalysis(selectedDetail.value, correlatedUpstreamErrors.value))
const primaryResponseBody = computed(() => resolvePrimaryResponseBody(selectedDetail.value, 'request'))
const schedulerAccountDiagnostics = computed<ErrorAnalysisAccountDiagnostic[]>(() => diagnoseSchedulerAccounts(schedulerAccounts.value, selectedDetail.value))
const availableSchedulerAccountCount = computed(() => schedulerAccountDiagnostics.value.filter((item) => item.available).length)

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const fromItem = computed(() => total.value === 0 ? 0 : (page.value - 1) * pageSize.value + 1)
const toItem = computed(() => Math.min(page.value * pageSize.value, total.value))
const compactVisiblePages = computed<(number | string)[]>(() => {
  const totalPageCount = totalPages.value
  if (totalPageCount <= 5) return Array.from({ length: totalPageCount }, (_, idx) => idx + 1)

  if (page.value <= 3) return [1, 2, 3, '...', totalPageCount]
  if (page.value >= totalPageCount - 2) return [1, '...', totalPageCount - 2, totalPageCount - 1, totalPageCount]
  return [1, '...', page.value, '...', totalPageCount]
})

async function fetchRequestErrors(options: { keepSelection?: boolean } = {}) {
  const fetchSeq = ++listFetchSeq
  loadingList.value = true
  try {
    const params: OpsErrorListQueryParams = {
      page: page.value,
      page_size: pageSize.value,
      time_range: timeRange.value,
      view: 'all'
    }
    if (statusCodeFilter.value) params.status_codes = statusCodeFilter.value
    if (searchQuery.value.trim()) params.q = searchQuery.value.trim()

    const res = await opsAPI.listRequestErrors(params)
    if (fetchSeq !== listFetchSeq) return

    requestErrors.value = res.items || []
    total.value = res.total || 0

    const selectedStillVisible = requestErrors.value.some((item) => item.id === selectedErrorId.value)
    if (!options.keepSelection || !selectedStillVisible) {
      const nextID = requestErrors.value[0]?.id ?? null
      if (nextID) {
        await selectError(nextID)
      } else {
        selectedErrorId.value = null
        selectedDetail.value = null
        correlatedUpstreamErrors.value = []
        schedulerAccounts.value = []
      }
    }
  } catch (err: any) {
    if (fetchSeq !== listFetchSeq) return

    console.error('[ErrorAnalysisView] Failed to load request errors', err)
    appStore.showError(err?.message || t('admin.ops.errorAnalysis.failedToLoadList'))
    requestErrors.value = []
    total.value = 0
  } finally {
    if (fetchSeq === listFetchSeq) loadingList.value = false
  }
}

async function selectError(id: number) {
  if (!id) return
  const fetchSeq = ++detailFetchSeq
  schedulerAccountFetchSeq++
  selectedErrorId.value = id
  showRaw.value = false
  schedulerAccounts.value = []
  loadingSchedulerAccounts.value = false
  loadingDetail.value = true
  try {
    const [detail, upstream] = await Promise.all([
      opsAPI.getRequestErrorDetail(id),
      opsAPI.listRequestErrorUpstreamErrors(id, { page: 1, page_size: 100, view: 'all' }, { include_detail: true })
    ])
    if (fetchSeq !== detailFetchSeq || selectedErrorId.value !== id) return

    selectedDetail.value = detail
    correlatedUpstreamErrors.value = upstream.items || []
    fetchSchedulerAccounts(detail)
  } catch (err: any) {
    if (fetchSeq !== detailFetchSeq || selectedErrorId.value !== id) return

    console.error('[ErrorAnalysisView] Failed to load analysis detail', err)
    appStore.showError(err?.message || t('admin.ops.errorAnalysis.failedToLoadDetail'))
    selectedDetail.value = null
    correlatedUpstreamErrors.value = []
    schedulerAccounts.value = []
  } finally {
    if (fetchSeq === detailFetchSeq && selectedErrorId.value === id) loadingDetail.value = false
  }
}

async function fetchSchedulerAccounts(detail: OpsErrorDetail | null) {
  const fetchSeq = ++schedulerAccountFetchSeq
  schedulerAccounts.value = []
  if (!detail?.group_id) return

  loadingSchedulerAccounts.value = true
  try {
    const pageSize = 100
    let nextPage = 1
    let totalAccounts = 0
    const items: Account[] = []

    do {
      const res = await adminAPI.accounts.list(nextPage, pageSize, {
        group: String(detail.group_id),
        sort_by: 'priority',
        sort_order: 'desc'
      })
      if (fetchSeq !== schedulerAccountFetchSeq) return

      items.push(...(res.items || []))
      totalAccounts = res.total || items.length
      nextPage += 1
    } while (items.length < totalAccounts)

    if (fetchSeq !== schedulerAccountFetchSeq) return
    schedulerAccounts.value = items
  } catch (err: any) {
    if (fetchSeq !== schedulerAccountFetchSeq) return
    console.error('[ErrorAnalysisView] Failed to load scheduler accounts', err)
    appStore.showError(err?.message || t('admin.ops.errorAnalysis.failedToLoadSchedulerAccounts'))
  } finally {
    if (fetchSeq === schedulerAccountFetchSeq) loadingSchedulerAccounts.value = false
  }
}

function handlePageChange(next: number) {
  page.value = next
  fetchRequestErrors({ keepSelection: false })
}

const debouncedSearch = useDebounceFn(() => {
  page.value = 1
  fetchRequestErrors({ keepSelection: false })
}, 350)

watch(searchQuery, () => debouncedSearch())

watch([timeRange, statusCodeFilter], () => {
  page.value = 1
  fetchRequestErrors({ keepSelection: false })
})

onMounted(() => {
  fetchRequestErrors({ keepSelection: false })
})

function displayModel(item: OpsErrorLog | OpsErrorDetail): string {
  const requested = String(item.requested_model || '').trim()
  const upstream = String(item.upstream_model || '').trim()
  if (requested && upstream && requested !== upstream) return `${requested} -> ${upstream}`
  return upstream || requested || String(item.model || '').trim()
}

function statusClass(code: number | null | undefined): string {
  const status = code || 0
  if (status >= 500) return 'bg-red-50 text-red-700 ring-red-600/20 dark:bg-red-900/30 dark:text-red-300 dark:ring-red-500/30'
  if (status === 429) return 'bg-purple-50 text-purple-700 ring-purple-600/20 dark:bg-purple-900/30 dark:text-purple-300 dark:ring-purple-500/30'
  if (status >= 400) return 'bg-amber-50 text-amber-700 ring-amber-600/20 dark:bg-amber-900/30 dark:text-amber-300 dark:ring-amber-500/30'
  return 'bg-gray-50 text-gray-700 ring-gray-600/20 dark:bg-gray-900/30 dark:text-gray-300 dark:ring-gray-500/30'
}

function stepCardClass(state: ErrorAnalysisStepState): string {
  switch (state) {
    case 'passed':
      return 'border-emerald-200 bg-emerald-50/60 dark:border-emerald-900/50 dark:bg-emerald-900/10'
    case 'failed':
      return 'border-red-200 bg-red-50/70 dark:border-red-900/50 dark:bg-red-900/10'
    case 'warning':
      return 'border-amber-200 bg-amber-50/70 dark:border-amber-900/50 dark:bg-amber-900/10'
    case 'skipped':
      return 'border-gray-200 bg-gray-50/80 opacity-80 dark:border-dark-700 dark:bg-dark-800/60'
    default:
      return 'border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900'
  }
}

function stepIconClass(state: ErrorAnalysisStepState): string {
  switch (state) {
    case 'passed':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'failed':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'warning':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'skipped':
      return 'bg-gray-100 text-gray-400 dark:bg-dark-700 dark:text-dark-300'
    default:
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  }
}

function stepIconName(state: ErrorAnalysisStepState): 'checkCircle' | 'xCircle' | 'exclamationTriangle' | 'clock' | 'chevronRight' {
  switch (state) {
    case 'passed':
      return 'checkCircle'
    case 'failed':
      return 'xCircle'
    case 'warning':
      return 'exclamationTriangle'
    case 'skipped':
      return 'chevronRight'
    default:
      return 'clock'
  }
}

function stateBadgeClass(state: ErrorAnalysisStepState): string {
  switch (state) {
    case 'passed':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'failed':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'warning':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'skipped':
      return 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'
    default:
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  }
}

function evidenceToneClass(tone?: string): string {
  switch (tone) {
    case 'good':
      return 'text-emerald-700 dark:text-emerald-300'
    case 'warning':
      return 'text-amber-700 dark:text-amber-300'
    case 'danger':
      return 'text-red-700 dark:text-red-300'
    default:
      return 'text-gray-800 dark:text-gray-100'
  }
}

function evidenceLabel(key: string): string {
  const translated = t(`admin.ops.errorAnalysis.evidence.${key}`)
  if (translated !== `admin.ops.errorAnalysis.evidence.${key}`) return translated
  return key
}

function accountReasonLabel(reason: ErrorAnalysisAccountReason): string {
  const translated = t(`admin.ops.errorAnalysis.schedulerAccounts.reason.${reason.key}`)
  const base = translated !== `admin.ops.errorAnalysis.schedulerAccounts.reason.${reason.key}` ? translated : reason.key
  return reason.detail ? `${base}: ${reason.detail}` : base
}

function formatAccountCapacity(account: Account): string {
  const parts: string[] = []
  if (account.concurrency > 0) parts.push(`CC ${account.current_concurrency ?? 0}/${account.concurrency}`)
  if (account.base_rpm && account.base_rpm > 0) parts.push(`RPM ${account.current_rpm ?? 0}/${account.base_rpm}`)
  return parts.join(' · ') || '-'
}

function prettyJSON(raw?: string): string {
  if (!raw) return 'N/A'
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}
</script>
