<template>
  <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="account-warmup">
    <div class="flex items-center justify-between gap-3">
      <span class="text-sm font-medium">{{ t('admin.accounts.warmup.title') }}</span>
      <Toggle :model-value="modelValue.enabled" :aria-label="t('admin.accounts.warmup.enabled')" @update:model-value="update('enabled', $event)" />
    </div>
    <template v-if="modelValue.enabled">
      <div class="space-y-2">
        <div class="flex items-center justify-between gap-2">
          <span class="input-label mb-0">{{ t('admin.accounts.warmup.models') }}</span>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="modelValue.models.length >= concurrencyMaxValue" @click="addModel">{{ t('admin.accounts.warmup.addModel') }}</button>
        </div>
        <div v-for="(model, index) in modelValue.models" :key="`${index}-${model}`" class="flex items-center gap-2">
          <select :value="model" required class="input min-w-0 flex-1" @change="updateModel(index, ($event.target as HTMLSelectElement).value)">
            <option v-for="option in modelOptions" :key="option.id" :value="option.id">{{ option.display_name || option.id }}</option>
            <option v-if="!modelOptions.some((option) => option.id === model)" :value="model">{{ model }}</option>
          </select>
          <button v-if="modelValue.models.length > 1" type="button" class="btn btn-secondary btn-sm" :aria-label="t('admin.accounts.warmup.removeModel')" @click="removeModel(index)">×</button>
        </div>
        <p class="input-hint">{{ t('admin.accounts.warmup.modelHint') }}</p>
      </div>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label class="block">
          <span class="input-label">{{ t('admin.accounts.warmup.concurrency') }}</span>
          <input :value="modelValue.concurrency" required type="number" min="1" :max="concurrencyMax" step="1" class="input w-full" @input="updateConcurrency(Number(($event.target as HTMLInputElement).value))" />
        </label>
        <label class="block">
          <span class="input-label">{{ t('admin.accounts.warmup.threshold') }}</span>
          <input :value="modelValue.success_threshold || ''" required type="number" min="1" step="1" class="input w-full" @input="update('success_threshold', Number(($event.target as HTMLInputElement).value))" />
        </label>
      </div>
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.warmup.prompt') }}</span>
        <textarea :value="modelValue.prompt" required rows="4" class="input w-full resize-y" @input="update('prompt', ($event.target as HTMLTextAreaElement).value)" />
        <p class="input-hint">{{ t('admin.accounts.warmup.contextHint') }}</p>
      </label>
    </template>
    <div v-if="state" class="flex flex-wrap items-center gap-3 text-xs" aria-live="polite">
      <span>{{ t(`admin.accounts.warmup.states.${state.status}`) }}</span>
      <span class="tabular-nums">{{ state.success_count }} / {{ state.success_threshold || modelValue.success_threshold }}</span>
      <button v-if="state.status === 'failed'" type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="control(false)">{{ t('admin.accounts.warmup.retry') }}</button>
      <button v-if="accountId" type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="control(true)">{{ t('admin.accounts.warmup.reset') }}</button>
    </div>
    <p v-if="state?.last_error || error" class="break-words text-xs text-red-600">{{ error || state?.last_error }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { apiClient } from '@/api/client'
import type { Account } from '@/types'
import { normalizeAccountWarmup, readAccountWarmupState, type AccountWarmupConfig, type AccountWarmupModelOption, type AccountWarmupState } from './accountWarmup'

const props = defineProps<{ modelValue: AccountWarmupConfig; maxConcurrency?: number; accountId?: number; visible?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: AccountWarmupConfig] }>()
const { t } = useI18n()
const state = ref<AccountWarmupState>()
const busy = ref(false)
const error = ref('')
const modelOptions = ref<AccountWarmupModelOption[]>([
  { id: 'gpt-5.6-sol', display_name: 'GPT-5.6 Sol' },
  { id: 'gpt-6-astra', display_name: 'GPT-6 Astra' },
  { id: 'gpt-6', display_name: 'GPT-6 (Astra)' },
  { id: 'gpt-5.5', display_name: 'GPT-5.5' },
])
const concurrencyMax = computed(() => (props.maxConcurrency && props.maxConcurrency > 0 ? props.maxConcurrency : undefined))
const concurrencyMaxValue = computed(() => concurrencyMax.value || 10)
let timer: ReturnType<typeof setTimeout> | undefined
let epoch = 0
function update<K extends keyof AccountWarmupConfig>(key: K, value: AccountWarmupConfig[K]) {
  emit('update:modelValue', normalizeAccountWarmup({ ...props.modelValue, [key]: value }))
}
function updateModel(index: number, value: string) {
  const models = [...props.modelValue.models]
  models[index] = value
  emit('update:modelValue', normalizeAccountWarmup({ ...props.modelValue, models }))
}
function addModel() {
  if (props.modelValue.models.length >= concurrencyMaxValue.value) return
  const fallback = modelOptions.value.find((option) => !props.modelValue.models.includes(option.id))?.id || 'gpt-5.6-sol'
  emit('update:modelValue', normalizeAccountWarmup({ ...props.modelValue, models: [...props.modelValue.models, fallback], concurrency: Math.max(props.modelValue.concurrency, props.modelValue.models.length + 1) }))
}
function removeModel(index: number) {
  if (props.modelValue.models.length <= 1) return
  emit('update:modelValue', normalizeAccountWarmup({ ...props.modelValue, models: props.modelValue.models.filter((_, current) => current !== index) }))
}
function updateConcurrency(value: number) {
  const concurrency = Math.max(1, Math.min(concurrencyMax.value || 100, Number.isFinite(value) ? value : 1))
  const models = props.modelValue.models.slice(0, concurrency)
  emit('update:modelValue', normalizeAccountWarmup({ ...props.modelValue, concurrency, models }))
}
async function loadModelOptions() {
  if (!props.accountId || !props.visible) return
  try {
    const { data } = await apiClient.get<AccountWarmupModelOption[]>(`/admin/accounts/${props.accountId}/models`)
    const merged = [...(Array.isArray(data) ? data : []), ...modelOptions.value]
    const seen = new Set<string>()
    modelOptions.value = merged.filter((option) => option?.id && !seen.has(option.id) && seen.add(option.id))
  } catch { /* The built-in model list remains available when discovery is unavailable. */ }
}
async function refresh(version: number) {
  if (!props.accountId || !props.visible) return
  try {
    const { data } = await apiClient.get<Account>(`/admin/accounts/${props.accountId}`)
    if (version === epoch) state.value = readAccountWarmupState(data.extra)
  } catch { /* The next poll retries without interrupting account editing. */ }
  if (version === epoch) timer = setTimeout(() => refresh(version), 3000)
}
async function control(reset: boolean) {
  if (reset && !window.confirm(t('admin.accounts.warmup.resetConfirm'))) return
  busy.value = true
  error.value = ''
  try {
    const { data } = await apiClient.post<AccountWarmupState>(`/admin/accounts/${props.accountId}/warmup/${reset ? 'reset' : 'retry'}`)
    state.value = data
  } catch (err: unknown) {
    error.value = (err as { message?: string }).message || t('admin.accounts.warmup.operationFailed')
  } finally { busy.value = false }
}
watch(() => [props.accountId, props.visible], () => {
  epoch++
  clearTimeout(timer)
  state.value = undefined
  void refresh(epoch)
  void loadModelOptions()
}, { immediate: true })
onUnmounted(() => { epoch++; clearTimeout(timer) })
</script>
