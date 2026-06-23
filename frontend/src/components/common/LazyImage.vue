<template>
  <div ref="rootRef" class="relative">
    <button v-if="visible" type="button" :class="buttonClass" @click="emit('open')">
      <img :src="src" :alt="alt" :class="imgClass" @load="onLoad" />
    </button>
    <div
      v-else
      class="min-h-[200px] w-full animate-pulse rounded-xl bg-stone-100 dark:bg-stone-900 sm:min-h-[280px]"
      :class="buttonClass"
    ></div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    src: string
    alt?: string
    buttonClass?: string
    imgClass?: string
    rootMargin?: string
  }>(),
  {
    alt: '',
    buttonClass: '',
    imgClass: 'block h-full w-full object-cover transition duration-200 group-hover:brightness-90 sm:h-auto sm:object-contain',
    rootMargin: '400px',
  }
)

const emit = defineEmits<{
  (event: 'open'): void
  (event: 'load', payload: { width: number; height: number }): void
}>()

const rootRef = ref<HTMLElement | null>(null)
const visible = ref(false)
let observer: IntersectionObserver | null = null

onMounted(() => {
  const element = rootRef.value
  if (!element) return
  if (typeof IntersectionObserver === 'undefined') {
    visible.value = true
    return
  }
  observer = new IntersectionObserver(
    ([entry]) => {
      if (entry.isIntersecting) {
        visible.value = true
        observer?.disconnect()
        observer = null
      }
    },
    { rootMargin: props.rootMargin }
  )
  observer.observe(element)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})

function onLoad(event: Event) {
  const target = event.target as HTMLImageElement
  emit('load', { width: target.naturalWidth, height: target.naturalHeight })
}
</script>
