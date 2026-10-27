<template>
  <main class="mx-auto max-w-6xl space-y-4 p-4">
    <h1 class="text-xl font-semibold">{{ t('admin.accounts.openai.prismGenerateLogin') }}</h1>
    <p class="text-sm text-gray-500">{{ t('admin.accounts.openai.prismRemoteHint') }}</p>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <p v-if="done" role="status" class="text-green-600">{{ t('admin.accounts.openai.prismLoginCompleted') }}</p>
    <template v-else>
      <p v-if="state.origin" class="font-mono text-sm">{{ state.origin }}</p>
      <p v-if="!state.frame && !error">{{ t('admin.accounts.openai.prismGenerating') }}</p>
      <div v-if="state.frame" class="space-y-2">
        <img :src="`data:image/jpeg;base64,${state.frame}`" :alt="t('admin.accounts.openai.prismBrowser')"
          tabindex="0" draggable="false" class="w-full rounded border focus:outline-primary-500"
          @click="click" @keydown="keydown" @paste.prevent="paste" @wheel.prevent="scroll" />
        <form class="flex flex-wrap gap-2" @submit.prevent="insertText">
          <input v-model="text" type="password" autocomplete="off" maxlength="4096" class="input flex-1" :placeholder="t('admin.accounts.openai.prismInputHint')" :aria-label="t('admin.accounts.openai.prismInputHint')" />
          <button type="submit" class="btn btn-primary" :disabled="!text || busy">{{ t('admin.accounts.openai.prismSendText') }}</button>
          <button type="button" class="btn btn-secondary" @click="send({ type: 'key', key: 'Tab' })">Tab</button>
          <button type="button" class="btn btn-secondary" @click="send({ type: 'key', key: 'Enter' })">Enter</button>
          <button type="button" class="btn btn-secondary" @click="send({ type: 'key', key: 'Backspace' })">⌫</button>
        </form>
      </div>
      <button type="button" class="btn btn-secondary" :disabled="cancelled" @click="cancel">{{ t('admin.accounts.openai.prismCancelLogin') }}</button>
    </template>
  </main>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { prismLogin, type PrismLoginState } from '@/api/admin/prism'

const { t } = useI18n()
const session = window.location.hash.slice(1)
const state = ref<PrismLoginState>({ status: 'starting' })
const error = ref('')
const text = ref('')
const done = ref(false)
const busy = ref(false)
const cancelled = ref(false)
let disposed = false
let timer: ReturnType<typeof setTimeout> | undefined
let actions: Promise<void> = Promise.resolve()

async function poll() {
  try {
    const next = await prismLogin.state(session, true)
    if (disposed || cancelled.value) return
    state.value = next
    if (next.status === 'completed' || next.status === 'consumed') {
      done.value = true
      return
    }
    if (next.status === 'failed' || next.status === 'expired') throw new Error('login')
    timer = setTimeout(() => void poll(), 650)
  } catch {
    if (!disposed) error.value = t('admin.accounts.openai.prismLoginError')
  }
}
function send(action: Record<string, unknown>) {
  actions = actions.then(async () => {
    if (disposed || done.value || cancelled.value) return
    busy.value = true
    try {
      await prismLogin.action(session, action)
      error.value = ''
    } catch {
      error.value = t('admin.accounts.openai.prismActionError')
    } finally {
      busy.value = false
    }
  })
  return actions
}
function click(event: MouseEvent) {
  const image = event.currentTarget as HTMLImageElement
  const box = image.getBoundingClientRect()
  image.focus()
  void send({ type: 'click', x: Math.min(1099, Math.max(0, (event.clientX - box.left) * 1100 / box.width)), y: Math.min(799, Math.max(0, (event.clientY - box.top) * 800 / box.height)) })
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing) return
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'v') return
  event.preventDefault()
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'a') {
    void send({ type: 'key', key: 'ControlOrMeta+A' })
  } else if (!event.ctrlKey && !event.metaKey && !event.altKey && event.key.length === 1) {
    void send({ type: 'text', text: event.key })
  } else if (['Tab', 'Enter', 'Backspace', 'Delete', 'Escape', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) {
    void send({ type: 'key', key: event.shiftKey && event.key === 'Tab' ? 'Shift+Tab' : event.key })
  }
}
function paste(event: ClipboardEvent) {
  const value = event.clipboardData?.getData('text/plain')
  if (value) void send({ type: 'text', text: value.slice(0, 4096) })
}
function scroll(event: WheelEvent) {
  if (!busy.value) void send({ type: 'scroll', delta: event.deltaY })
}
async function insertText() {
  const value = text.value
  text.value = ''
  if (value) await send({ type: 'text', text: value })
}
async function cancel() {
  cancelled.value = true
  if (timer) clearTimeout(timer)
  try { await prismLogin.cancel(session) } catch { /* Already expired. */ }
  state.value = { status: 'expired' }
  error.value = t('admin.accounts.openai.prismLoginCancelled')
}
onMounted(() => {
  if (/^[a-f0-9]{64}$/.test(session)) void poll()
  else error.value = t('admin.accounts.openai.prismLoginError')
})
onBeforeUnmount(() => {
  disposed = true
  if (timer) clearTimeout(timer)
  text.value = ''
})
</script>
