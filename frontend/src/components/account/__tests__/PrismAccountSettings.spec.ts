import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PrismAccountSettings from '../PrismAccountSettings.vue'

const { create, state, cancel } = vi.hoisted(() => ({ create: vi.fn(), state: vi.fn(), cancel: vi.fn() }))
vi.mock('@/api/admin/prism', () => ({ prismLogin: { create, state, cancel } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

afterEach(() => { vi.clearAllMocks(); vi.useRealTimers() })

describe('Prism login links', () => {
  it('binds the completed login ticket without exposing cookies', async () => {
    create.mockResolvedValue({ session_id: 'session1', login_url: '/admin/prism-login#session1' })
    state.mockResolvedValue({ status: 'completed' })
    cancel.mockResolvedValue({})
    const wrapper = mount(PrismAccountSettings, { props: { enabled: true, cookie: '', accountId: 42, proxyId: 3 } })
    await wrapper.get('[data-testid="prism-generate-login"]').trigger('click')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(42, 3)
    expect(wrapper.emitted('update:loginSession')?.at(-1)).toEqual(['session1'])
    expect(wrapper.emitted('update:cookie')?.at(-1)).toEqual([''])
    expect(wrapper.get('[role="status"]').text()).toContain('prismLoginCompleted')
    wrapper.unmount()
    expect(cancel).toHaveBeenCalledWith('session1')
  })

  it('shows an openable link while waiting and cancels when the proxy changes', async () => {
    create.mockResolvedValue({ session_id: 'session2', login_url: '/admin/prism-login#session2' })
    state.mockResolvedValue({ status: 'pending' })
    cancel.mockResolvedValue({})
    const wrapper = mount(PrismAccountSettings, { props: { enabled: true, cookie: '', proxyId: 3 } })
    await wrapper.get('[data-testid="prism-generate-login"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('a').attributes('href')).toContain('/admin/prism-login#session2')
    expect(wrapper.get('a').attributes('target')).toBe('_blank')
    await wrapper.setProps({ proxyId: 4 })
    expect(cancel).toHaveBeenCalledWith('session2')
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.emitted('update:loginSession')?.at(-1)).toEqual([''])
    wrapper.unmount()
  })

  it('discards a login created after its account editor has closed', async () => {
    let resolve!: (result: unknown) => void
    create.mockReturnValue(new Promise(r => { resolve = r }))
    cancel.mockResolvedValue({})
    const wrapper = mount(PrismAccountSettings, { props: { enabled: true, cookie: '' } })
    await wrapper.get('[data-testid="prism-generate-login"]').trigger('click')
    wrapper.unmount()
    resolve({ session_id: 'late', login_url: '/admin/prism-login#late' })
    await flushPromises()
    expect(cancel).toHaveBeenCalledWith('late')
    expect(state).not.toHaveBeenCalled()
  })
})
