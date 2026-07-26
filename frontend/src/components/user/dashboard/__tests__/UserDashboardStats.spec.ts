import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import UserDashboardStats from '../UserDashboardStats.vue'
import type { UserDashboardStats as UserStatsType } from '@/api/usage'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const baseStats: UserStatsType = {
  total_api_keys: 2,
  active_api_keys: 1,
  total_requests: 100,
  total_input_tokens: 1000,
  total_output_tokens: 2000,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 3000,
  total_cost: 1.5,
  total_actual_cost: 1.2,
  today_requests: 10,
  today_input_tokens: 100,
  today_output_tokens: 200,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 300,
  today_cost: 0.15,
  today_actual_cost: 0.12,
  average_duration_ms: 250,
  rpm: 3,
  tpm: 90,
  by_platform: [
    {
      platform: 'anthropic',
      total_requests: 20,
      total_tokens: 400,
      total_actual_cost: 0.4,
      today_requests: 2,
      today_tokens: 40,
      today_actual_cost: 0.04
    },
    {
      platform: 'openai',
      total_requests: 30,
      total_tokens: 600,
      total_actual_cost: 0.5,
      today_requests: 3,
      today_tokens: 60,
      today_actual_cost: 0.05
    },
    {
      platform: 'gemini',
      total_requests: 40,
      total_tokens: 800,
      total_actual_cost: 0.6,
      today_requests: 4,
      today_tokens: 80,
      today_actual_cost: 0.06
    },
    {
      platform: 'antigravity',
      total_requests: 50,
      total_tokens: 1000,
      total_actual_cost: 0.7,
      today_requests: 5,
      today_tokens: 100,
      today_actual_cost: 0.07
    }
  ]
}

describe('UserDashboardStats', () => {
  it('does not show platform breakdown cards on the user dashboard', () => {
    const wrapper = mount(UserDashboardStats, {
      props: {
        stats: baseStats,
        balance: 10,
        isSimple: false
      }
    })

    const text = wrapper.text()

    expect(text).not.toContain('dashboard.platformBreakdown')
    expect(text).not.toContain('Claude')
    expect(text).not.toContain('OpenAI')
    expect(text).not.toContain('Gemini')
    expect(text).not.toContain('Antigravity')
  })
})
