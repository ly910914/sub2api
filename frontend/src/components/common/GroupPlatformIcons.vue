<template>
  <span v-if="displayPlatforms.length > 0" class="inline-flex items-center gap-1">
    <span
      v-for="iconPlatform in displayPlatforms"
      :key="iconPlatform"
      :class="platformIconClass(iconPlatform)"
      class="inline-flex shrink-0 items-center"
    >
      <PlatformIcon
        :platform="iconPlatform"
        :size="size"
      />
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { GroupPlatform } from '@/types'
import PlatformIcon from './PlatformIcon.vue'
import { platformIconClass } from '@/utils/platformColors'

interface Props {
  platform?: GroupPlatform
  platforms?: GroupPlatform[]
  size?: 'xs' | 'sm' | 'md' | 'lg'
}

const props = withDefaults(defineProps<Props>(), {
  size: 'sm'
})

const displayPlatforms = computed<GroupPlatform[]>(() => {
  if (props.platforms && props.platforms.length > 0) return props.platforms
  if (props.platform === 'openai') return ['openai', 'anthropic']
  return props.platform ? [props.platform] : []
})
</script>
