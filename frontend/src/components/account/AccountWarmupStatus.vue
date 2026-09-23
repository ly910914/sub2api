<template>
  <div v-if="state" class="max-w-48 break-words text-xs" :class="state.status === 'completed' ? 'text-emerald-600' : 'text-amber-700 dark:text-amber-400'" :title="state.last_error">
    {{ t(`admin.accounts.warmup.states.${state.status}`) }}
    <span v-if="state.status !== 'disabled'" class="tabular-nums">{{ state.success_count }} / {{ state.success_threshold }}</span>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { readAccountWarmupState } from './accountWarmup'
const props = defineProps<{ extra?: Record<string, unknown> }>()
const { t } = useI18n()
const state = computed(() => readAccountWarmupState(props.extra))
</script>
