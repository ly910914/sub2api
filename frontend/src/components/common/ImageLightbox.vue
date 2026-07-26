<template>
  <Teleport to="body">
    <div
      v-if="open && current"
      class="fixed inset-0 z-[100000030] flex items-center justify-center"
      @click="close"
    >
      <div class="absolute inset-0 bg-black/85 backdrop-blur-sm"></div>

      <!-- Top-right controls -->
      <div
        class="absolute right-4 z-10 flex items-center gap-2"
        :style="{ top: 'calc(env(safe-area-inset-top) + 1rem)' }"
        @click.stop
      >
        <span v-if="metaLabel" class="rounded-full bg-black/50 px-3 py-1.5 text-xs font-medium text-white/90">
          {{ metaLabel }}
        </span>
        <span v-if="images.length > 1" class="rounded-full bg-black/50 px-3 py-1.5 text-xs font-medium text-white/90">
          {{ index + 1 }} / {{ images.length }}
        </span>
        <button type="button" class="lightbox-btn" :aria-label="downloadLabel" @click="download">
          <Icon name="download" size="sm" />
        </button>
        <button type="button" class="lightbox-btn" :aria-label="closeLabel" @click="close">
          <Icon name="x" size="sm" />
        </button>
      </div>

      <!-- Prev -->
      <button
        v-if="hasPrev && transform.scale <= 1"
        type="button"
        class="lightbox-nav left-4"
        :aria-label="prevLabel"
        @click.stop="goPrev"
      >
        <Icon name="chevronLeft" size="md" />
      </button>

      <!-- Stage -->
      <div
        class="relative z-[5] flex h-full w-full touch-none items-center justify-center overflow-hidden"
        @touchstart="onTouchStart"
        @touchmove="onTouchMove"
        @touchend="onTouchEnd"
        @touchcancel="onTouchCancel"
      >
        <img
          :src="current.src"
          alt=""
          draggable="false"
          class="max-h-[90vh] max-w-[92vw] rounded-lg object-contain will-change-transform"
          :class="[
            isGesturing ? '' : 'transition-transform duration-150 ease-out',
            transform.scale > 1 ? 'cursor-grab active:cursor-grabbing' : 'cursor-zoom-in',
          ]"
          :style="{ transform: `translate3d(${transform.x}px, ${transform.y}px, 0) scale(${transform.scale})` }"
          @click.stop
          @dblclick.stop="toggleZoom"
        />
      </div>

      <!-- Next -->
      <button
        v-if="hasNext && transform.scale <= 1"
        type="button"
        class="lightbox-nav right-4"
        :aria-label="nextLabel"
        @click.stop="goNext"
      >
        <Icon name="chevronRight" size="md" />
      </button>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

export interface LightboxItem {
  id: string
  src: string
  sizeLabel?: string
  dimensions?: string
}

interface ImageTransform {
  scale: number
  x: number
  y: number
}

type TouchGesture =
  | { type: 'swipe'; startX: number; startY: number }
  | { type: 'pan'; startX: number; startY: number; startTransform: ImageTransform }
  | { type: 'pinch'; startDistance: number; startCenterX: number; startCenterY: number; startTransform: ImageTransform }

const props = defineProps<{
  open: boolean
  images: LightboxItem[]
  index: number
}>()

const emit = defineEmits<{
  (event: 'update:open', value: boolean): void
  (event: 'update:index', value: number): void
}>()

const { t } = useI18n()

const MIN_SCALE = 1
const MAX_SCALE = 4

const transform = reactive<ImageTransform>({ scale: 1, x: 0, y: 0 })
const isGesturing = ref(false)
let gesture: TouchGesture | null = null
let lastTap = 0

const current = computed(() => props.images[props.index] || null)
const hasPrev = computed(() => props.index > 0)
const hasNext = computed(() => props.index < props.images.length - 1)
const metaLabel = computed(() => [current.value?.sizeLabel, current.value?.dimensions].filter(Boolean).join(' · '))

const downloadLabel = computed(() => t('images.download'))
const closeLabel = computed(() => t('common.close'))
const prevLabel = computed(() => t('images.prevImage'))
const nextLabel = computed(() => t('images.nextImage'))

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

function getTouchDistance(touches: TouchList) {
  return Math.hypot(touches[0].clientX - touches[1].clientX, touches[0].clientY - touches[1].clientY)
}

function getTouchCenter(touches: TouchList) {
  return {
    x: (touches[0].clientX + touches[1].clientX) / 2,
    y: (touches[0].clientY + touches[1].clientY) / 2,
  }
}

function normalize(next: ImageTransform): ImageTransform {
  if (next.scale <= MIN_SCALE) {
    return { scale: MIN_SCALE, x: 0, y: 0 }
  }
  const maxX = window.innerWidth * (next.scale - 1) * 0.5
  const maxY = window.innerHeight * (next.scale - 1) * 0.5
  return { scale: next.scale, x: clamp(next.x, -maxX, maxX), y: clamp(next.y, -maxY, maxY) }
}

function applyTransform(next: ImageTransform) {
  transform.scale = next.scale
  transform.x = next.x
  transform.y = next.y
}

function resetTransform() {
  applyTransform({ scale: 1, x: 0, y: 0 })
  isGesturing.value = false
  gesture = null
}

function close() {
  emit('update:open', false)
}

function goPrev() {
  if (hasPrev.value) emit('update:index', props.index - 1)
}

function goNext() {
  if (hasNext.value) emit('update:index', props.index + 1)
}

function toggleZoom() {
  applyTransform(transform.scale > MIN_SCALE ? { scale: 1, x: 0, y: 0 } : { scale: 2.5, x: 0, y: 0 })
}

function download() {
  if (!current.value) return
  const link = document.createElement('a')
  link.href = current.value.src
  link.download = `image-${current.value.id}.png`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

function onTouchStart(event: TouchEvent) {
  if (event.touches.length === 2) {
    event.preventDefault()
    const startDistance = getTouchDistance(event.touches)
    if (startDistance < 1) {
      gesture = null
      return
    }
    const center = getTouchCenter(event.touches)
    isGesturing.value = true
    gesture = {
      type: 'pinch',
      startDistance,
      startCenterX: center.x,
      startCenterY: center.y,
      startTransform: { ...transform },
    }
    return
  }

  if (event.touches.length !== 1) {
    gesture = null
    return
  }

  const touch = event.touches[0]
  if (transform.scale > MIN_SCALE) {
    isGesturing.value = true
    gesture = { type: 'pan', startX: touch.clientX, startY: touch.clientY, startTransform: { ...transform } }
  } else {
    gesture = { type: 'swipe', startX: touch.clientX, startY: touch.clientY }
  }
}

function onTouchMove(event: TouchEvent) {
  if (!gesture) return

  if (gesture.type === 'pinch' && event.touches.length === 2) {
    event.preventDefault()
    const targetScale = clamp(
      (getTouchDistance(event.touches) / gesture.startDistance) * gesture.startTransform.scale,
      MIN_SCALE,
      MAX_SCALE
    )
    const ratio = targetScale / gesture.startTransform.scale
    const center = getTouchCenter(event.touches)
    const cx = window.innerWidth / 2
    const cy = window.innerHeight / 2
    const nextX = center.x - cx - (gesture.startCenterX - cx - gesture.startTransform.x) * ratio
    const nextY = center.y - cy - (gesture.startCenterY - cy - gesture.startTransform.y) * ratio
    applyTransform(normalize({ scale: targetScale, x: nextX, y: nextY }))
    return
  }

  if (gesture.type === 'pan' && event.touches.length === 1) {
    event.preventDefault()
    const touch = event.touches[0]
    applyTransform(
      normalize({
        scale: gesture.startTransform.scale,
        x: gesture.startTransform.x + touch.clientX - gesture.startX,
        y: gesture.startTransform.y + touch.clientY - gesture.startY,
      })
    )
    return
  }

  if (event.touches.length !== 1) {
    gesture = null
  }
}

function onTouchEnd(event: TouchEvent) {
  isGesturing.value = false
  const finished = gesture
  gesture = null
  if (!finished || finished.type !== 'swipe' || event.changedTouches.length !== 1) return

  const touch = event.changedTouches[0]
  const deltaX = touch.clientX - finished.startX
  const deltaY = touch.clientY - finished.startY
  const now = Date.now()

  if (Math.abs(deltaX) < 10 && Math.abs(deltaY) < 10 && now - lastTap < 280) {
    event.preventDefault()
    lastTap = 0
    toggleZoom()
    return
  }
  lastTap = now

  if (Math.abs(deltaX) < 48 || Math.abs(deltaX) < Math.abs(deltaY) * 1.4) return

  if (deltaX > 0) {
    goPrev()
  } else {
    goNext()
  }
}

function onTouchCancel() {
  isGesturing.value = false
  gesture = null
}

function onKeydown(event: KeyboardEvent) {
  if (!props.open) return
  if (event.key === 'ArrowLeft') {
    event.preventDefault()
    goPrev()
  } else if (event.key === 'ArrowRight') {
    event.preventDefault()
    goNext()
  } else if (event.key === 'Escape') {
    event.preventDefault()
    close()
  }
}

watch(
  () => [props.open, current.value?.id],
  () => resetTransform()
)

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<style scoped>
.lightbox-btn {
  display: inline-flex;
  height: 2.25rem;
  width: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: rgb(0 0 0 / 0.5);
  color: rgb(255 255 255 / 0.9);
  transition: background-color 150ms ease;
}

.lightbox-btn:hover {
  background: rgb(0 0 0 / 0.7);
}

.lightbox-nav {
  position: absolute;
  top: 50%;
  z-index: 10;
  display: inline-flex;
  height: 2.5rem;
  width: 2.5rem;
  transform: translateY(-50%);
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: rgb(0 0 0 / 0.4);
  color: rgb(255 255 255 / 0.9);
  transition: background-color 150ms ease;
}

.lightbox-nav:hover {
  background: rgb(0 0 0 / 0.6);
}
</style>
