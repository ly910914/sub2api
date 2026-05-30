import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import GroupBadge from '../GroupBadge.vue'
import GroupOptionItem from '../GroupOptionItem.vue'
import GroupPlatformIcons from '../GroupPlatformIcons.vue'
import PlatformIcon from '../PlatformIcon.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

describe('GroupBadge', () => {
  it('shows one group platform icon by default for non-OpenAI groups', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'Claude Group',
        platform: 'anthropic'
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(1)
    expect(icons[0].props('platform')).toBe('anthropic')
  })

  it('shows OpenAI and Anthropic icons by default for OpenAI groups', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'OpenAI Group',
        platform: 'openai'
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(2)
    expect(icons.map((icon) => icon.props('platform'))).toEqual(['openai', 'anthropic'])
  })

  it('shows multiple platform icons when provided', () => {
    const wrapper = mount(GroupBadge, {
      props: {
        name: 'OpenAI Group',
        platform: 'openai',
        iconPlatforms: ['openai', 'anthropic']
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(2)
    expect(icons.map((icon) => icon.props('platform'))).toEqual(['openai', 'anthropic'])
    expect(wrapper.html()).toContain('text-emerald-500')
    expect(wrapper.html()).toContain('text-orange-500')
  })
})

describe('GroupPlatformIcons', () => {
  it('shows OpenAI and Anthropic icons for OpenAI group platform displays', () => {
    const wrapper = mount(GroupPlatformIcons, {
      props: {
        platform: 'openai'
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(2)
    expect(icons.map((icon) => icon.props('platform'))).toEqual(['openai', 'anthropic'])
  })
})

describe('GroupOptionItem', () => {
  it('uses the GroupBadge OpenAI default icons', () => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'OpenAI Group',
        platform: 'openai',
        rateMultiplier: 1
      }
    })

    const icons = wrapper.findAllComponents(PlatformIcon)

    expect(icons).toHaveLength(2)
    expect(icons.map((icon) => icon.props('platform'))).toEqual(['openai', 'anthropic'])
  })
})
