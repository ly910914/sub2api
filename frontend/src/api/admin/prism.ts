import { apiClient } from '../client'

export interface PrismLoginState {
  status: 'starting' | 'pending' | 'completed' | 'consumed' | 'failed' | 'expired'
  frame?: string
  origin?: string
}

const base = '/admin/prism/login'
export const prismLogin = {
  async create(accountID = 0, proxyID?: number | null) {
    const { data } = await apiClient.post<{ session_id: string; login_url: string; expires_at: string }>(base, {
      account_id: accountID, proxy_id: proxyID ?? 0
    })
    return data
  },
  async state(session: string, frame = false) {
    const { data } = await apiClient.get<PrismLoginState>(`${base}/${encodeURIComponent(session)}`, { params: { frame: frame ? '1' : '0' } })
    return data
  },
  async action(session: string, action: Record<string, unknown>) {
    await apiClient.post(`${base}/${encodeURIComponent(session)}/action`, action)
  },
  async cancel(session: string) {
    await apiClient.delete(`${base}/${encodeURIComponent(session)}`)
  }
}
