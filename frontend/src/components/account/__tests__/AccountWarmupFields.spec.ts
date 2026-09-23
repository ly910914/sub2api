import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountWarmupFields from '../AccountWarmupFields.vue'
import { readAccountWarmup } from '../accountWarmup'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: ReturnType<typeof mount>[] = []
function render(props = {}) {
  const wrapper = mount(AccountWarmupFields, { props: { modelValue: readAccountWarmup(), ...props } })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => { vi.useFakeTimers(); vi.resetAllMocks(); api.get.mockResolvedValue({ data: { extra: {} } }) })
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); vi.useRealTimers(); vi.restoreAllMocks() })

describe('account warmup', () => {
  it('defaults off and enables only through an explicit account edit', async () => {
    const wrapper = render()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('textarea').exists()).toBe(false)
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ ...readAccountWarmup(), enabled: true }])
    expect(api.post).not.toHaveBeenCalled()
  })
  it('collects a prompt, model, per-account concurrency and success target', async () => {
    const wrapper = render({ modelValue: { ...readAccountWarmup(), enabled: true }, maxConcurrency: 3 })
    expect(wrapper.get('input[type="number"]').attributes('max')).toBe('3')
    await wrapper.get('textarea').setValue('Continue the conversation')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toMatchObject({ prompt: 'Continue the conversation' })
    expect(wrapper.findAll('input[required]').length + wrapper.findAll('select[required]').length).toBe(3)
  })
  it('does not turn an unlimited account concurrency of zero into max=0', () => {
    const wrapper = render({ modelValue: { ...readAccountWarmup(), enabled: true }, maxConcurrency: 0 })
    expect(wrapper.get('input[type="number"]').attributes('max')).toBeUndefined()
  })
  it('refreshes completion without changing unsaved config or restarting the task', async () => {
    api.get.mockResolvedValue({ data: { extra: { openai_warmup_state: { status: 'running', success_count: 2, success_threshold: 3 } } } })
    const wrapper = render({ accountId: 7, visible: true })
    await flushPromises()
    expect(wrapper.text()).toContain('2 / 3')
    api.get.mockResolvedValue({ data: { extra: { openai_warmup_state: { status: 'completed', success_count: 3, success_threshold: 3 } } } })
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.text()).toContain('states.completed')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(api.post).not.toHaveBeenCalled()
  })
  it('keeps progress when reset is cancelled and retries through the dedicated endpoint', async () => {
    api.get.mockResolvedValue({ data: { extra: { openai_warmup_state: { status: 'failed', success_count: 2, success_threshold: 3, last_error: 'timeout' } } } })
    const wrapper = render({ accountId: 7, visible: true })
    await flushPromises()
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    await wrapper.findAll('button').find(b => b.text().includes('warmup.reset'))!.trigger('click')
    expect(api.post).not.toHaveBeenCalled()
    api.post.mockResolvedValue({ data: { status: 'pending', success_count: 2, success_threshold: 3 } })
    await wrapper.findAll('button').find(b => b.text().includes('warmup.retry'))!.trigger('click')
    await flushPromises()
    expect(api.post).toHaveBeenCalledWith('/admin/accounts/7/warmup/retry')
    expect(wrapper.text()).toContain('2 / 3')
  })
})
