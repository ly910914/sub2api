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
})
