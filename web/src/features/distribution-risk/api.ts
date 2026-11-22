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
  DistributionRiskConfig,
  DistributionRiskConfigUpdate,
  DistributionRiskEvent,
  DistributionRiskEventList,
  DistributionRiskEventQuery,
} from './types'

function requireData<T>(response: ApiResponse<T>): T {
  const result = requireServerSuccess(response)
  if (result.data === undefined || result.data === null) {
    throw new Error(result.message || 'Missing distribution risk response')
  }
  return result.data
}

export async function getDistributionRiskConfig(): Promise<DistributionRiskConfig> {
  const response = await api.get<ApiResponse<DistributionRiskConfig>>(
    '/api/distribution-risk/config'
  )
  return requireData(response.data)
}

export async function updateDistributionRiskConfig(
  update: DistributionRiskConfigUpdate
): Promise<DistributionRiskConfig> {
  const response = await api.put<ApiResponse<DistributionRiskConfig>>(
    '/api/distribution-risk/config',
    update
  )
  return requireData(response.data)
}

export async function listDistributionRiskEvents(
  filters: DistributionRiskEventQuery,
  page: number,
  pageSize: number,
  signal?: AbortSignal
): Promise<DistributionRiskEventList> {
  const response = await api.get<ApiResponse<DistributionRiskEventList>>(
    '/api/distribution-risk/events',
    { params: { ...filters, page, page_size: pageSize }, signal }
  )
  return requireData(response.data)
}

export async function getDistributionRiskEvent(
  id: number
): Promise<DistributionRiskEvent> {
  const response = await api.get<ApiResponse<DistributionRiskEvent>>(
    `/api/distribution-risk/events/${id}`
  )
  return requireData(response.data)
}

export async function reviewDistributionRiskEvent(
  id: number,
  decision: 'confirmed' | 'false_positive',
  reason: string
): Promise<void> {
  const response = await api.patch<ApiResponse<never>>(
    `/api/distribution-risk/events/${id}/review`,
    { decision, reason }
  )
  requireServerSuccess(response.data)
}
