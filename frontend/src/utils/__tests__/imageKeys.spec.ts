import { describe, expect, it } from 'vitest'
import { listImageCapableAPIKeys, pickDefaultImageAPIKey } from '@/utils/imageKeys'
import type { ApiKey } from '@/types'

function key(partial: Partial<ApiKey>): ApiKey {
  return {
    id: 1,
    user_id: 1,
    key: 'sk-test',
    name: 'test',
    group_id: 1,
    status: 'active',
    ip_whitelist: [],
    ip_blacklist: [],
    last_used_at: null,
    quota: 0,
    quota_used: 0,
    expires_at: null,
    created_at: '',
    updated_at: '',
    rate_limit_5h: 0,
    rate_limit_1d: 0,
    rate_limit_7d: 0,
    usage_5h: 0,
    usage_1d: 0,
    usage_7d: 0,
    window_5h_start: null,
    window_1d_start: null,
    window_7d_start: null,
    reset_5h_at: null,
    reset_1d_at: null,
    reset_7d_at: null,
    ...partial,
  }
}

describe('image key selection', () => {
  it('filters active keys with usable key material', () => {
    const keys = [
      key({ id: 1, group: { platform: 'anthropic', allow_image_generation: true } as any }),
      key({ id: 2, status: 'inactive', group: { platform: 'openai', allow_image_generation: true } as any }),
      key({ id: 3, key: '', group: { platform: 'openai', allow_image_generation: true } as any }),
      key({ id: 4, group: { platform: 'openai', allow_image_generation: false } as any }),
    ]

    expect(listImageCapableAPIKeys(keys).map((item) => item.id)).toEqual([1, 4])
  })

  it('selects the first eligible key unless the current key is still eligible', () => {
    const keys = [
      key({ id: 1, group: { platform: 'openai', allow_image_generation: true } as any }),
      key({ id: 2, group: { platform: 'openai', allow_image_generation: true } as any }),
    ]

    expect(pickDefaultImageAPIKey(keys)?.id).toBe(1)
    expect(pickDefaultImageAPIKey(keys, 2)?.id).toBe(2)
  })
})
