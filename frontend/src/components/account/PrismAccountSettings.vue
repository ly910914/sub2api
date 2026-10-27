<template>
  <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="prism-settings">
    <div class="flex items-start justify-between gap-4">
      <div>
        <label class="input-label mb-0">{{ t('admin.accounts.openai.prismTitle') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.prismDescription') }}</p>
      </div>
      <button
        type="button" role="switch" :aria-checked="enabled" :aria-label="t('admin.accounts.openai.prismTitle')"
        data-testid="prism-toggle" @click="$emit('update:enabled', !enabled)"
        :class="['relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500', enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600']"
      >
        <span :class="['inline-block h-5 w-5 rounded-full bg-white shadow transition-transform', enabled ? 'translate-x-5' : 'translate-x-0']" />
      </button>
    </div>
    <div v-if="enabled" class="space-y-2">
      <button type="button" class="btn btn-primary" :disabled="generating" data-testid="prism-generate-login" @click="generateLogin">
        {{ t(generating ? 'admin.accounts.openai.prismGenerating' : 'admin.accounts.openai.prismGenerateLogin') }}
      </button>
      <div v-if="loginURL && !completed" class="space-y-2">
        <a :href="loginURL" target="_blank" rel="noopener noreferrer" class="block break-all text-sm text-primary-600 underline">{{ t('admin.accounts.openai.prismOpenLogin') }}</a>
        <input class="input w-full" readonly :value="loginURL" :aria-label="t('admin.accounts.openai.prismGenerateLogin')" @focus="($event.target as HTMLInputElement).select()" />
        <p class="text-xs text-gray-500">{{ t('admin.accounts.openai.prismWaiting') }}</p>
      </div>
      <p v-if="completed" class="text-sm text-green-600" role="status">{{ t('admin.accounts.openai.prismLoginCompleted') }}</p>
      <p v-else-if="saved" class="text-xs text-gray-500">{{ t('admin.accounts.openai.prismSaved') }}</p>
      <details>
        <summary class="cursor-pointer text-xs text-gray-500">{{ t('admin.accounts.openai.prismManualImport') }}</summary>
        <label class="input-label" for="prism-cookie">{{ t('admin.accounts.openai.prismCredentials') }}</label>
        <input id="prism-cookie" type="password" autocomplete="new-password" class="input w-full"
          :value="cookie" :placeholder="t(saved ? 'admin.accounts.openai.prismSaved' : 'admin.accounts.openai.prismPlaceholder')"
          data-testid="prism-cookie" @input="setCookie(($event.target as HTMLInputElement).value)" />
        <input type="file" accept=".json,application/json" class="block w-full text-xs" :aria-label="t('admin.accounts.openai.prismImport')" @change="importAuth" />
      </details>
      <p v-if="error" class="text-xs text-red-600" role="alert">{{ error }}</p>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.prismLoginHint') }}</p>
      <p class="text-xs text-amber-700 dark:text-amber-400">{{ t('admin.accounts.openai.prismLimitations') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { prismLogin } from '@/api/admin/prism'

const props = defineProps<{ enabled: boolean; cookie: string; saved?: boolean; accountId?: number; proxyId?: number | null; loginSession?: string }>()
const emit = defineEmits<{ 'update:enabled': [value: boolean]; 'update:cookie': [value: string]; 'update:loginSession': [value: string] }>()
const { t } = useI18n()
const error = ref('')
const generating = ref(false)
const completed = ref(false)
const loginURL = ref('')
let activeSession = ''
let generation = 0
let timer: ReturnType<typeof setTimeout> | undefined
function discardLogin() {
  generation++
  if (timer) clearTimeout(timer)
  const previous = activeSession
  activeSession = ''
  loginURL.value = ''
  completed.value = false
  emit('update:loginSession', '')
  if (previous) void prismLogin.cancel(previous).catch(() => {})
}
async function generateLogin() {
  discardLogin()
  const version = generation
  generating.value = true
  error.value = ''
  try {
    const result = await prismLogin.create(props.accountId, props.proxyId)
    if (version !== generation) {
      void prismLogin.cancel(result.session_id).catch(() => {})
      return
    }
    activeSession = result.session_id
    loginURL.value = new URL(result.login_url, window.location.origin).href
    emit('update:cookie', '')
    void pollLogin(version)
  } catch {
    if (version === generation) error.value = t('admin.accounts.openai.prismLoginError')
  } finally {
    generating.value = false
  }
}
async function pollLogin(version: number) {
  try {
    const state = await prismLogin.state(activeSession)
    if (version !== generation) return
    if (state.status === 'completed') {
      completed.value = true
      emit('update:loginSession', activeSession)
      return
    }
    if (['failed', 'expired', 'consumed'].includes(state.status)) throw new Error('login')
    timer = setTimeout(() => void pollLogin(version), 1500)
  } catch {
    if (version === generation) {
      discardLogin()
      error.value = t('admin.accounts.openai.prismLoginError')
    }
  }
}
function setCookie(value: string) {
  discardLogin()
  emit('update:cookie', value)
}
watch(() => [props.enabled, props.accountId, props.proxyId], discardLogin)
onBeforeUnmount(discardLogin)
async function importAuth(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  error.value = ''
  if (!file) return
  try {
    if (file.size > 65536) throw new Error('size')
    const auth = JSON.parse(await file.text()) as { cookie?: unknown }
    if (typeof auth.cookie !== 'string' || !auth.cookie.includes('prism_oai_access_token=')) throw new Error('cookie')
    setCookie(auth.cookie)
  } catch {
    error.value = t('admin.accounts.openai.prismInvalidAuth')
  } finally {
    input.value = ''
  }
}
</script>
