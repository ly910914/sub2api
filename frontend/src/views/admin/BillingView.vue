<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <div
          v-for="card in summaryCards"
          :key="card.key"
          class="card flex min-h-28 items-center justify-between gap-4 p-4"
        >
          <div class="min-w-0">
            <p class="text-sm font-medium text-gray-500 dark:text-gray-400">{{ card.label }}</p>
            <p class="mt-2 truncate text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatBillingCurrency(card.value) }}
            </p>
          </div>
          <div
            class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg"
            :class="card.iconClass"
          >
            <Icon :name="card.icon" size="md" />
          </div>
        </div>
      </div>

      <TablePageLayout class="billing-table-layout">
        <template #filters>
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-48 flex-1 sm:max-w-64">
              <input
                v-model="filters.q"
                type="text"
                list="billing-person-filter-suggestions"
                :placeholder="t('admin.billing.searchPerson')"
                class="input"
                @input="handleFilterInput"
              />
              <datalist id="billing-person-filter-suggestions">
                <option v-for="name in nameSuggestions" :key="`filter-${name}`" :value="name" />
              </datalist>
            </div>

            <div class="flex flex-wrap items-center gap-2">
              <input
                v-model="filters.from"
                type="date"
                class="input w-40"
                :aria-label="t('admin.billing.fromDate')"
                @change="handleFilterChange"
              />
              <input
                v-model="filters.to"
                type="date"
                class="input w-40"
                :aria-label="t('admin.billing.toDate')"
                @change="handleFilterChange"
              />
            </div>

            <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="loading || statsLoading"
                :title="t('common.refresh')"
                @click="loadData"
              >
                <Icon
                  name="refresh"
                  size="md"
                  :class="loading || statsLoading ? 'animate-spin' : ''"
                />
              </button>
              <button type="button" class="btn btn-primary" @click="openCreateDialog">
                <Icon name="plus" size="md" class="mr-1" />
                {{ t('admin.billing.createRecord') }}
              </button>
            </div>
          </div>
        </template>

        <template #table>
          <DataTable
            :columns="columns"
            :data="records"
            :loading="loading"
            :server-side-sort="true"
            default-sort-key="occurred_at"
            default-sort-order="desc"
            @sort="handleSort"
          >
            <template #cell-person_name="{ value }">
              <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
            </template>

            <template #cell-source="{ value }">
              <span class="text-sm text-gray-700 dark:text-dark-300">{{ value || '-' }}</span>
            </template>

            <template #cell-cost="{ value }">
              <span class="font-medium text-red-600 dark:text-red-400">{{ formatBillingCurrency(value) }}</span>
            </template>

            <template #cell-profit="{ value }">
              <span class="font-medium text-emerald-600 dark:text-emerald-400">
                {{ formatBillingCurrency(value) }}
              </span>
            </template>

            <template #cell-revenue="{ row }">
              <span class="font-medium text-gray-900 dark:text-white">
                {{ formatBillingCurrency(row.cost + row.profit) }}
              </span>
            </template>

            <template #cell-occurred_at="{ value }">
              <span class="text-sm text-gray-500 dark:text-dark-400">
                {{ formatDateTime(value) }}
              </span>
            </template>

            <template #cell-note="{ value }">
              <span class="block max-w-72 truncate text-sm text-gray-500 dark:text-dark-400">
                {{ value || '-' }}
              </span>
            </template>

            <template #cell-actions="{ row }">
              <div class="flex items-center space-x-1">
                <button
                  type="button"
                  class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                  :title="t('common.edit')"
                  @click="handleEdit(row)"
                >
                  <Icon name="edit" size="sm" />
                </button>
                <button
                  type="button"
                  class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                  :title="t('common.delete')"
                  @click="handleDelete(row)"
                >
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </template>
          </DataTable>
        </template>

        <template #pagination>
          <Pagination
            v-if="pagination.total > 0"
            :page="pagination.page"
            :total="pagination.total"
            :page-size="pagination.page_size"
            @update:page="handlePageChange"
            @update:pageSize="handlePageSizeChange"
          />
        </template>
      </TablePageLayout>

      <div class="grid gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(320px,1fr)]">
        <BillingTrendChart :trend-data="stats.trend" :loading="statsLoading" />

        <div class="card p-4">
          <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.billing.perPersonTitle') }}
          </h3>
          <div v-if="statsLoading" class="flex h-56 items-center justify-center">
            <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
          </div>
          <div
            v-else-if="stats.per_person.length === 0"
            class="flex h-56 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
          >
            {{ t('admin.billing.noStatsData') }}
          </div>
          <div v-else class="max-h-72 overflow-auto">
            <table class="w-full min-w-max text-sm">
              <thead>
                <tr class="border-b border-gray-200 text-left dark:border-dark-700">
                  <th class="px-3 py-2 font-medium text-gray-500 dark:text-gray-400">
                    {{ t('admin.billing.personName') }}
                  </th>
                  <th class="px-3 py-2 text-right font-medium text-gray-500 dark:text-gray-400">
                    {{ t('admin.billing.cost') }}
                  </th>
                  <th class="px-3 py-2 text-right font-medium text-gray-500 dark:text-gray-400">
                    {{ t('admin.billing.profit') }}
                  </th>
                  <th class="px-3 py-2 text-right font-medium text-gray-500 dark:text-gray-400">
                    {{ t('admin.billing.revenue') }}
                  </th>
                  <th class="px-3 py-2 text-right font-medium text-gray-500 dark:text-gray-400">
                    {{ t('admin.billing.count') }}
                  </th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <tr v-for="person in stats.per_person" :key="person.person_name">
                  <td class="px-3 py-3 font-medium text-gray-900 dark:text-white">
                    {{ person.person_name }}
                  </td>
                  <td class="px-3 py-3 text-right text-red-600 dark:text-red-400">
                    {{ formatBillingCurrency(person.total_cost) }}
                  </td>
                  <td class="px-3 py-3 text-right text-emerald-600 dark:text-emerald-400">
                    {{ formatBillingCurrency(person.total_profit) }}
                  </td>
                  <td class="px-3 py-3 text-right text-gray-900 dark:text-white">
                    {{ formatBillingCurrency(person.total_revenue) }}
                  </td>
                  <td class="px-3 py-3 text-right text-gray-500 dark:text-gray-400">
                    {{ person.count }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>

    <BaseDialog
      :show="showCreateDialog"
      :title="t('admin.billing.createRecord')"
      width="normal"
      @close="closeCreateDialog"
    >
      <form id="create-billing-form" class="space-y-4" @submit.prevent="handleCreate">
        <BillingRecordForm
          v-model:person-name="createForm.person_name"
          v-model:source="createForm.source"
          v-model:cost="createForm.cost"
          v-model:profit="createForm.profit"
          v-model:occurred-at="createForm.occurred_at_str"
          v-model:note="createForm.note"
          datalist-id="billing-person-create-suggestions"
          source-datalist-id="billing-source-create-suggestions"
          :suggestions="nameSuggestions"
          :source-suggestions="sourceSuggestions"
          @person-input="handleSuggestionInput"
          @source-input="handleSourceSuggestionInput"
        />
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeCreateDialog">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="create-billing-form" :disabled="creating" class="btn btn-primary">
            {{ creating ? t('admin.billing.creating') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showEditDialog"
      :title="t('admin.billing.editRecord')"
      width="normal"
      @close="closeEditDialog"
    >
      <form id="edit-billing-form" class="space-y-4" @submit.prevent="handleUpdate">
        <BillingRecordForm
          v-model:person-name="editForm.person_name"
          v-model:source="editForm.source"
          v-model:cost="editForm.cost"
          v-model:profit="editForm.profit"
          v-model:occurred-at="editForm.occurred_at_str"
          v-model:note="editForm.note"
          datalist-id="billing-person-edit-suggestions"
          source-datalist-id="billing-source-edit-suggestions"
          :suggestions="nameSuggestions"
          :source-suggestions="sourceSuggestions"
          @person-input="handleSuggestionInput"
          @source-input="handleSourceSuggestionInput"
        />
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeEditDialog">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="edit-billing-form" :disabled="updating" class="btn btn-primary">
            {{ updating ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.billing.deleteRecord')"
      :message="t('admin.billing.deleteConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatCurrency, formatDateTime, parseDateTimeLocalInput } from '@/utils/format'
import type {
  AdminBillingRecord,
  AdminBillingStats,
  CreateAdminBillingRecordRequest
} from '@/types'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import BillingTrendChart from '@/components/charts/BillingTrendChart.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const ADMIN_BILLING_CURRENCY = 'CNY'

const records = ref<AdminBillingRecord[]>([])
const stats = ref<AdminBillingStats>(emptyStats())
const nameSuggestions = ref<string[]>([])
const sourceSuggestions = ref<string[]>([])
const loading = ref(false)
const statsLoading = ref(false)
const creating = ref(false)
const updating = ref(false)

const filters = reactive({
  q: '',
  from: '',
  to: ''
})

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0
})

const sortState = reactive({
  sort_by: 'occurred_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const showDeleteDialog = ref(false)
const editingRecord = ref<AdminBillingRecord | null>(null)
const deletingRecord = ref<AdminBillingRecord | null>(null)

type BillingSummaryIcon = 'creditCard' | 'chart' | 'chartBar' | 'calendar'
interface BillingSummaryCard {
  key: string
  label: string
  value: number
  icon: BillingSummaryIcon
  iconClass: string
}

const createForm = reactive({
  person_name: '',
  source: '',
  cost: 0,
  profit: 0,
  occurred_at_str: '',
  note: ''
})

const editForm = reactive({
  person_name: '',
  source: '',
  cost: 0,
  profit: 0,
  occurred_at_str: '',
  note: ''
})

const summaryCards = computed<BillingSummaryCard[]>(() => [
  {
    key: 'cost',
    label: t('admin.billing.totalCost'),
    value: stats.value.summary.total_cost,
    icon: 'creditCard',
    iconClass: 'bg-red-50 text-red-600 dark:bg-red-900/20 dark:text-red-400'
  },
  {
    key: 'profit',
    label: t('admin.billing.totalProfit'),
    value: stats.value.summary.total_profit,
    icon: 'chart',
    iconClass: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-400'
  },
  {
    key: 'revenue',
    label: t('admin.billing.totalRevenue'),
    value: stats.value.summary.total_revenue,
    icon: 'chartBar',
    iconClass: 'bg-blue-50 text-blue-600 dark:bg-blue-900/20 dark:text-blue-400'
  },
  {
    key: 'net',
    label: t('admin.billing.netProfit'),
    value: stats.value.summary.net_profit,
    icon: 'calendar',
    iconClass: 'bg-amber-50 text-amber-600 dark:bg-amber-900/20 dark:text-amber-400'
  }
])

const columns = computed<Column[]>(() => [
  { key: 'person_name', label: t('admin.billing.columns.personName'), sortable: true },
  { key: 'source', label: t('admin.billing.columns.source'), sortable: true },
  { key: 'cost', label: t('admin.billing.columns.cost'), sortable: true },
  { key: 'profit', label: t('admin.billing.columns.profit'), sortable: true },
  { key: 'revenue', label: t('admin.billing.columns.revenue') },
  { key: 'occurred_at', label: t('admin.billing.columns.occurredAt'), sortable: true },
  { key: 'note', label: t('admin.billing.columns.note') },
  { key: 'actions', label: t('admin.billing.columns.actions') }
])

let recordsAbortController: AbortController | null = null
let statsAbortController: AbortController | null = null
let suggestionsAbortController: AbortController | null = null
let sourceSuggestionsAbortController: AbortController | null = null
let filterTimeout: ReturnType<typeof setTimeout> | null = null
let suggestionTimeout: ReturnType<typeof setTimeout> | null = null
let sourceSuggestionTimeout: ReturnType<typeof setTimeout> | null = null

const loadData = async () => {
  await Promise.allSettled([loadRecords(), loadStats()])
}

const loadRecords = async () => {
  recordsAbortController?.abort()
  const controller = new AbortController()
  recordsAbortController = controller
  loading.value = true

  try {
    const response = await adminAPI.billing.list(
      pagination.page,
      pagination.page_size,
      {
        q: filters.q || undefined,
        from: filters.from || undefined,
        to: filters.to || undefined,
        sort_by: sortState.sort_by,
        sort_order: sortState.sort_order
      },
      { signal: controller.signal }
    )
    if (controller.signal.aborted || recordsAbortController !== controller) return
    records.value = response.items
    pagination.total = response.total
  } catch (error: any) {
    if (isAbortError(error, controller, recordsAbortController)) return
    appStore.showError(error?.message || t('admin.billing.failedToLoad'))
    console.error('Error loading billing records:', error)
  } finally {
    if (recordsAbortController === controller) {
      loading.value = false
      recordsAbortController = null
    }
  }
}

const loadStats = async () => {
  statsAbortController?.abort()
  const controller = new AbortController()
  statsAbortController = controller
  statsLoading.value = true

  try {
    const response = await adminAPI.billing.stats(
      {
        q: filters.q || undefined,
        from: filters.from || undefined,
        to: filters.to || undefined
      },
      { signal: controller.signal }
    )
    if (controller.signal.aborted || statsAbortController !== controller) return
    stats.value = normalizeStats(response)
  } catch (error: any) {
    if (isAbortError(error, controller, statsAbortController)) return
    appStore.showError(error?.message || t('admin.billing.failedToLoadStats'))
    console.error('Error loading billing stats:', error)
  } finally {
    if (statsAbortController === controller) {
      statsLoading.value = false
      statsAbortController = null
    }
  }
}

const loadSuggestions = async (q = '') => {
  suggestionsAbortController?.abort()
  const controller = new AbortController()
  suggestionsAbortController = controller

  try {
    const response = await adminAPI.billing.suggestions(q, 10, { signal: controller.signal })
    if (controller.signal.aborted || suggestionsAbortController !== controller) return
    nameSuggestions.value = response
  } catch (error: any) {
    if (isAbortError(error, controller, suggestionsAbortController)) return
    console.error('Error loading billing person suggestions:', error)
  } finally {
    if (suggestionsAbortController === controller) {
      suggestionsAbortController = null
    }
  }
}

const loadSourceSuggestions = async (q = '') => {
  sourceSuggestionsAbortController?.abort()
  const controller = new AbortController()
  sourceSuggestionsAbortController = controller

  try {
    const response = await adminAPI.billing.sourceSuggestions(q, 10, { signal: controller.signal })
    if (controller.signal.aborted || sourceSuggestionsAbortController !== controller) return
    sourceSuggestions.value = response
  } catch (error: any) {
    if (isAbortError(error, controller, sourceSuggestionsAbortController)) return
    console.error('Error loading billing source suggestions:', error)
  } finally {
    if (sourceSuggestionsAbortController === controller) {
      sourceSuggestionsAbortController = null
    }
  }
}

const handleFilterInput = () => {
  scheduleSuggestions(filters.q)
  if (filterTimeout) clearTimeout(filterTimeout)
  filterTimeout = setTimeout(() => {
    pagination.page = 1
    loadData()
  }, 300)
}

const handleFilterChange = () => {
  pagination.page = 1
  loadData()
}

const handleSuggestionInput = (value: string) => {
  scheduleSuggestions(value)
}

const handleSourceSuggestionInput = (value: string) => {
  scheduleSourceSuggestions(value)
}

const scheduleSuggestions = (value: string) => {
  if (suggestionTimeout) clearTimeout(suggestionTimeout)
  suggestionTimeout = setTimeout(() => {
    loadSuggestions(value)
  }, 200)
}

const scheduleSourceSuggestions = (value: string) => {
  if (sourceSuggestionTimeout) clearTimeout(sourceSuggestionTimeout)
  sourceSuggestionTimeout = setTimeout(() => {
    loadSourceSuggestions(value)
  }, 200)
}

const handlePageChange = (page: number) => {
  pagination.page = page
  loadRecords()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  loadRecords()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadRecords()
}

const openCreateDialog = () => {
  resetCreateForm()
  showCreateDialog.value = true
  loadSuggestions(createForm.person_name)
  loadSourceSuggestions(createForm.source)
}

const closeCreateDialog = () => {
  showCreateDialog.value = false
}

const handleCreate = async () => {
  creating.value = true
  try {
    await adminAPI.billing.create(buildRequest(createForm))
    appStore.showSuccess(t('admin.billing.recordCreated'))
    showCreateDialog.value = false
    resetCreateForm()
    await loadData()
    loadSuggestions()
    loadSourceSuggestions()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.billing.failedToCreate'))
  } finally {
    creating.value = false
  }
}

const handleEdit = (record: AdminBillingRecord) => {
  editingRecord.value = record
  editForm.person_name = record.person_name
  editForm.source = record.source || ''
  editForm.cost = record.cost
  editForm.profit = record.profit
  editForm.occurred_at_str = toDateTimeLocal(record.occurred_at)
  editForm.note = record.note || ''
  showEditDialog.value = true
  loadSuggestions(record.person_name)
  loadSourceSuggestions(record.source || '')
}

const closeEditDialog = () => {
  showEditDialog.value = false
  editingRecord.value = null
}

const handleUpdate = async () => {
  if (!editingRecord.value) return

  updating.value = true
  try {
    await adminAPI.billing.update(editingRecord.value.id, buildRequest(editForm))
    appStore.showSuccess(t('admin.billing.recordUpdated'))
    closeEditDialog()
    await loadData()
    loadSuggestions()
    loadSourceSuggestions()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.billing.failedToUpdate'))
  } finally {
    updating.value = false
  }
}

const handleDelete = (record: AdminBillingRecord) => {
  deletingRecord.value = record
  showDeleteDialog.value = true
}

const confirmDelete = async () => {
  if (!deletingRecord.value) return

  try {
    await adminAPI.billing.delete(deletingRecord.value.id)
    appStore.showSuccess(t('admin.billing.recordDeleted'))
    showDeleteDialog.value = false
    deletingRecord.value = null
    await loadData()
    loadSuggestions()
    loadSourceSuggestions()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.billing.failedToDelete'))
  }
}

const resetCreateForm = () => {
  createForm.person_name = ''
  createForm.source = ''
  createForm.cost = 0
  createForm.profit = 0
  createForm.occurred_at_str = nowDateTimeLocal()
  createForm.note = ''
}

const buildRequest = (form: typeof createForm): CreateAdminBillingRecordRequest => ({
  person_name: form.person_name.trim(),
  source: form.source.trim(),
  cost: Number(form.cost) || 0,
  profit: Number(form.profit) || 0,
  occurred_at: form.occurred_at_str ? parseDateTimeLocalInput(form.occurred_at_str) : null,
  note: form.note.trim() || null
})

const formatBillingCurrency = (amount: number | null | undefined): string => {
  const value = Number(amount || 0)
  return formatCurrency(value, ADMIN_BILLING_CURRENCY)
}

const toDateTimeLocal = (value: string | Date | null | undefined): string => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return formatDateForInput(date)
}

const nowDateTimeLocal = () => formatDateForInput(new Date())

const formatDateForInput = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day}T${hours}:${minutes}`
}

function emptyStats(): AdminBillingStats {
  return {
    summary: {
      total_cost: 0,
      total_profit: 0,
      total_revenue: 0,
      net_profit: 0,
      count: 0
    },
    per_person: [],
    trend: []
  }
}

const normalizeStats = (value: AdminBillingStats | null | undefined): AdminBillingStats => ({
  summary: {
    ...emptyStats().summary,
    ...(value?.summary || {})
  },
  per_person: value?.per_person || [],
  trend: value?.trend || []
})

const isAbortError = (
  error: any,
  controller: AbortController,
  activeController: AbortController | null
) =>
  controller.signal.aborted ||
  activeController !== controller ||
  error?.name === 'AbortError' ||
  error?.code === 'ERR_CANCELED'

const BillingRecordForm = defineComponent({
  name: 'BillingRecordForm',
  props: {
    personName: { type: String, required: true },
    source: { type: String, required: true },
    cost: { type: Number, required: true },
    profit: { type: Number, required: true },
    occurredAt: { type: String, required: true },
    note: { type: String, required: true },
    datalistId: { type: String, required: true },
    sourceDatalistId: { type: String, required: true },
    suggestions: { type: Array as () => string[], required: true },
    sourceSuggestions: { type: Array as () => string[], required: true }
  },
  emits: [
    'update:personName',
    'update:source',
    'update:cost',
    'update:profit',
    'update:occurredAt',
    'update:note',
    'personInput',
    'sourceInput'
  ],
  setup(props, { emit }) {
    const { t } = useI18n()
    const inputClass = 'input'
    const labelClass = 'input-label'

    return () =>
      h('div', { class: 'space-y-4' }, [
        h('div', { class: 'grid gap-4 sm:grid-cols-2' }, [
          h('div', [
            h('label', { class: labelClass }, t('admin.billing.personName')),
            h('input', {
              value: props.personName,
              type: 'text',
              required: true,
              maxlength: 100,
              list: props.datalistId,
              class: inputClass,
              placeholder: t('admin.billing.personNamePlaceholder'),
              onInput: (event: Event) => {
                const value = (event.target as HTMLInputElement).value
                emit('update:personName', value)
                emit('personInput', value)
              }
            }),
            h(
              'datalist',
              { id: props.datalistId },
              (Array.isArray(props.suggestions) ? props.suggestions : []).map((name) =>
                h('option', { key: name, value: name })
              )
            )
          ]),
          h('div', [
            h('label', { class: labelClass }, [
              t('admin.billing.source'),
              h(
                'span',
                { class: 'ml-1 text-xs font-normal text-gray-400' },
                `(${t('common.optional')})`
              )
            ]),
            h('input', {
              value: props.source,
              type: 'text',
              maxlength: 100,
              list: props.sourceDatalistId,
              class: inputClass,
              placeholder: t('admin.billing.sourcePlaceholder'),
              onInput: (event: Event) => {
                const value = (event.target as HTMLInputElement).value
                emit('update:source', value)
                emit('sourceInput', value)
              }
            }),
            h(
              'datalist',
              { id: props.sourceDatalistId },
              (Array.isArray(props.sourceSuggestions) ? props.sourceSuggestions : []).map((source) =>
                h('option', { key: source, value: source })
              )
            )
          ])
        ]),
        h('div', { class: 'grid gap-4 sm:grid-cols-2' }, [
          h('div', [
            h('label', { class: labelClass }, t('admin.billing.cost')),
            h('input', {
              value: props.cost,
              type: 'number',
              min: 0,
              step: '0.01',
              required: true,
              class: inputClass,
              onInput: (event: Event) =>
                emit('update:cost', Number((event.target as HTMLInputElement).value))
            })
          ]),
          h('div', [
            h('label', { class: labelClass }, t('admin.billing.profit')),
            h('input', {
              value: props.profit,
              type: 'number',
              min: 0,
              step: '0.01',
              required: true,
              class: inputClass,
              onInput: (event: Event) =>
                emit('update:profit', Number((event.target as HTMLInputElement).value))
            })
          ])
        ]),
        h('div', [
          h('label', { class: labelClass }, t('admin.billing.occurredAt')),
          h('input', {
            value: props.occurredAt,
            type: 'datetime-local',
            required: true,
            class: inputClass,
            onInput: (event: Event) =>
              emit('update:occurredAt', (event.target as HTMLInputElement).value)
          })
        ]),
        h('div', [
          h('label', { class: labelClass }, [
            t('admin.billing.note'),
            h(
              'span',
              { class: 'ml-1 text-xs font-normal text-gray-400' },
              `(${t('common.optional')})`
            )
          ]),
          h('textarea', {
            value: props.note,
            rows: 3,
            class: inputClass,
            placeholder: t('admin.billing.notePlaceholder'),
            onInput: (event: Event) =>
              emit('update:note', (event.target as HTMLTextAreaElement).value)
          })
        ])
      ])
  }
})

onMounted(() => {
  loadData()
  loadSuggestions()
  loadSourceSuggestions()
})

onUnmounted(() => {
  if (filterTimeout) clearTimeout(filterTimeout)
  if (suggestionTimeout) clearTimeout(suggestionTimeout)
  if (sourceSuggestionTimeout) clearTimeout(sourceSuggestionTimeout)
  recordsAbortController?.abort()
  statsAbortController?.abort()
  suggestionsAbortController?.abort()
  sourceSuggestionsAbortController?.abort()
})
</script>

<style scoped>
.billing-table-layout {
  height: auto;
  min-height: 32rem;
}

.billing-table-layout :deep(.layout-section-scrollable) {
  min-height: 24rem;
}

.billing-table-layout :deep(.table-scroll-container) {
  min-height: 24rem;
  max-height: 46rem;
}
</style>
