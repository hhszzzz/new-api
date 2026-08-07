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
} from './types'

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
