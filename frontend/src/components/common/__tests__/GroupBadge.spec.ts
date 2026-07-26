import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import GroupBadge from '../GroupBadge.vue'
import GroupOptionItem from '../GroupOptionItem.vue'
import PlatformIcon from '../PlatformIcon.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: {}
  })
}))

const platformColorPattern = /\b(?:bg|text|border)-(?:orange|emerald|green|blue|sky|purple|fuchsia|violet|zinc|amber)-/

describe('GroupBadge', () => {
  it('does not show platform icons for non-OpenAI groups', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'Claude Group',
        platform: 'anthropic'
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(0)
  })

  it('does not show platform icons for OpenAI groups', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'OpenAI Group',
        platform: 'openai'
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(0)
  })

  it('ignores explicit platform icon lists', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'OpenAI Group',
        platform: 'openai',
        iconPlatforms: ['openai', 'anthropic']
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(0)
    expect(wrapper.text()).toContain('OpenAI Group')
  })

  it('keeps group badges neutral-colored across platforms', () => {
    const platforms = ['anthropic', 'openai', 'gemini', 'antigravity', 'grok'] as const

    for (const platform of platforms) {
      const wrapper = mount(GroupBadge, {
        props: {
          name: `${platform} Group`,
          platform,
          rateMultiplier: 1
        }
      })

      expect(wrapper.html()).not.toMatch(platformColorPattern)
    }
  })
})

describe('GroupOptionItem', () => {
  it('keeps group options icon-free', () => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'OpenAI Group',
        platform: 'openai',
        rateMultiplier: 1
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(0)
  })

  it('keeps group option rate pills neutral-colored', () => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'OpenAI Group',
        platform: 'openai',
        rateMultiplier: 1
      }
    })

    expect(wrapper.html()).not.toMatch(platformColorPattern)
  })
})
