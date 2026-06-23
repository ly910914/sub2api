/**
 * Admin manual billing API endpoints.
 */

import { apiClient } from '../client'
import type {
  AdminBillingRecord,
  AdminBillingStats,
  BasePaginationResponse,
  CreateAdminBillingRecordRequest,
  UpdateAdminBillingRecordRequest
} from '@/types'

export interface AdminBillingFilters {
  q?: string
  from?: string
  to?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: AdminBillingFilters,
  options?: {
    signal?: AbortSignal
  }
): Promise<BasePaginationResponse<AdminBillingRecord>> {
  const { data } = await apiClient.get<BasePaginationResponse<AdminBillingRecord>>(
    '/admin/billing-records',
    {
      params: { page, page_size: pageSize, ...filters },
      signal: options?.signal
    }
  )
  return data
}

export async function create(
  request: CreateAdminBillingRecordRequest
): Promise<AdminBillingRecord> {
  const { data } = await apiClient.post<AdminBillingRecord>('/admin/billing-records', request)
  return data
}

export async function update(
  id: number,
  request: UpdateAdminBillingRecordRequest
): Promise<AdminBillingRecord> {
  const { data } = await apiClient.put<AdminBillingRecord>(`/admin/billing-records/${id}`, request)
  return data
}

export async function deleteRecord(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/billing-records/${id}`)
  return data
}

export async function stats(
  filters?: Omit<AdminBillingFilters, 'sort_by' | 'sort_order'>,
  options?: {
    signal?: AbortSignal
  }
): Promise<AdminBillingStats> {
  const { data } = await apiClient.get<AdminBillingStats>('/admin/billing-records/stats', {
    params: filters,
    signal: options?.signal
  })
  return data
}

export async function suggestions(
  q: string = '',
  limit: number = 10,
  options?: {
    signal?: AbortSignal
  }
): Promise<string[]> {
  const { data } = await apiClient.get<string[] | null>('/admin/billing-records/suggestions', {
    params: { q, limit },
    signal: options?.signal
  })
  return Array.isArray(data) ? data : []
}

export async function sourceSuggestions(
  q: string = '',
  limit: number = 10,
  options?: {
    signal?: AbortSignal
  }
): Promise<string[]> {
  const { data } = await apiClient.get<string[] | null>(
    '/admin/billing-records/source-suggestions',
    {
      params: { q, limit },
      signal: options?.signal
    }
  )
  return Array.isArray(data) ? data : []
}

const billingAPI = {
  list,
  create,
  update,
  delete: deleteRecord,
  stats,
  suggestions,
  sourceSuggestions
}

export default billingAPI
