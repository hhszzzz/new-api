/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  ApiResponse,
  PromptAuditCategory,
  PromptAuditConfig,
  PromptAuditConfigUpdate,
  PromptAuditDeleteFilter,
  PromptAuditDeletePreview,
  PromptAuditEvent,
  PromptAuditListData,
  PromptAuditStats,
  PromptAuditScope,
  PromptWordlist,
  PromptWordlistMatch,
} from './types'

export async function listPromptWordlists(): Promise<PromptWordlist[]> {
  const response = await api.get<ApiResponse<PromptWordlist[]>>(
    '/api/prompt-audit/wordlists'
  )
  const result = requireServerSuccess(response.data)
  if (!Array.isArray(result.data)) throw new Error('Invalid wordlist response')
  return result.data
}

export async function createPromptWordlist(payload: {
  name: string
  source_url: string
  scopes: PromptAuditScope[]
  auto_update: boolean
}) {
  return requireServerSuccess(
    (
      await api.post<ApiResponse<{ id: string }>>(
        '/api/prompt-audit/wordlists',
        payload
      )
    ).data
  )
}

export async function updatePromptWordlist(
  id: string,
  payload: Partial<Pick<PromptWordlist, 'name' | 'enabled' | 'auto_update'>>
) {
  return requireServerSuccess(
    (
      await api.put<ApiResponse<never>>(
        `/api/prompt-audit/wordlists/${encodeURIComponent(id)}`,
        payload
      )
    ).data
  )
}

export async function syncPromptWordlist(id: string) {
  return requireServerSuccess(
    (
      await api.post<ApiResponse<never>>(
        `/api/prompt-audit/wordlists/${encodeURIComponent(id)}/sync`
      )
    ).data
  )
}

export async function deletePromptWordlist(id: string) {
  return requireServerSuccess(
    (
      await api.delete<ApiResponse<never>>(
        `/api/prompt-audit/wordlists/${encodeURIComponent(id)}`
      )
    ).data
  )
}

export async function getManualPromptWordlist(): Promise<string> {
  const response = await api.get<ApiResponse<{ words: string }>>(
    '/api/prompt-audit/wordlists/manual/content'
  )
  return requireServerSuccess(response.data).data?.words ?? ''
}

export async function updateManualPromptWordlist(words: string) {
  return requireServerSuccess(
    (
      await api.put<ApiResponse<never>>(
        '/api/prompt-audit/wordlists/manual/content',
        { words }
      )
    ).data
  )
}

export async function testPromptWordlists(
  scope: PromptAuditScope,
  text: string
) {
  const response = await api.post<
    ApiResponse<{ match: PromptWordlistMatch | null; model_audit: boolean }>
  >('/api/prompt-audit/wordlists/test', { scope, text })
  const result = requireServerSuccess(response.data)
  if (!result.data) throw new Error('Invalid wordlist test response')
  return result.data
}

export async function getPromptAuditConfig() {
  const response = await api.get<ApiResponse<PromptAuditConfig>>(
    '/api/prompt-audit/config'
  )
  return response.data
}

export async function updatePromptAuditConfig(
  payload: PromptAuditConfigUpdate
) {
  const response = await api.put<ApiResponse<PromptAuditConfig>>(
    '/api/prompt-audit/config',
    payload
  )
  return response.data
}

export async function getPromptAuditCategories() {
  const response = await api.get<ApiResponse<PromptAuditCategory[]>>(
    '/api/prompt-audit/categories'
  )
  return response.data
}

export async function testPromptAuditNode(id: string) {
  const response = await api.post<
    ApiResponse<{ endpoint_id: string; latency_ms: number; safety: string }>
  >(`/api/prompt-audit/nodes/${encodeURIComponent(id)}/test`)
  return response.data
}

export async function listPromptAudits(
  params: Record<string, string | number | undefined>
) {
  const response = await api.get<ApiResponse<PromptAuditListData>>(
    '/api/prompt-audit/events',
    { params }
  )
  return response.data
}

export async function getPromptAudit(id: number) {
  const response = await api.get<ApiResponse<PromptAuditEvent>>(
    `/api/prompt-audit/events/${id}`
  )
  return response.data
}

export async function getPromptAuditStats(
  params: Record<string, string | number | undefined>
) {
  const response = await api.get<ApiResponse<PromptAuditStats>>(
    '/api/prompt-audit/stats',
    { params }
  )
  return response.data
}

export async function retryPromptAudit(id: number) {
  const response = await api.post<ApiResponse<{ id: number; status: string }>>(
    `/api/prompt-audit/events/${id}/retry`
  )
  return response.data
}

export async function previewPromptAuditDelete(
  filter: PromptAuditDeleteFilter
) {
  const response = await api.post<ApiResponse<PromptAuditDeletePreview>>(
    '/api/prompt-audit/events/delete-preview',
    { filter }
  )
  return response.data
}

export async function deletePromptAudits(
  filter: PromptAuditDeleteFilter,
  preview: PromptAuditDeletePreview
) {
  const response = await api.delete<ApiResponse<{ deleted_count: number }>>(
    '/api/prompt-audit/events',
    {
      data: {
        filter,
        expected_count: preview.eligible_count,
        max_id: preview.max_id,
      },
    }
  )
  return response.data
}
