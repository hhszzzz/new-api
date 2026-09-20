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
  ConfirmPaymentComplianceResponse,
  FetchUpstreamRatiosRequest,
  LogCleanupTask,
  ModelRadarManagement,
  SystemTask,
  SystemOptionsResponse,
  SystemTaskListResponse,
  SystemTaskFilters,
  SystemTaskResponse,
  SystemUpdateInfo,
  SystemUpdateResponse,
  SystemUpdateStartResponse,
  SystemUpdateTriggerState,
  SystemUpdateTriggerStateResponse,
  UpdateClientPolicyOptionsRequest,
  UpdateGroupRateLimitOptionsRequest,
  UpdateModelPricingOptionsRequest,
  UpdateOptionRequest,
  UpdateOptionResponse,
  UpdatePasskeyDomainsRequest,
  UpdatePasskeyDomainsResponse,
  UpstreamChannelsResponse,
  UpstreamRatiosResponse,
} from './types'

export async function getSystemOptions() {
  const res = await api.get<SystemOptionsResponse>('/api/option/')
  return res.data
}

export async function updateSystemOption(request: UpdateOptionRequest) {
  const res = await api.put<UpdateOptionResponse>('/api/option/', request)
  return res.data
}

export async function updateModelPricingOptions(
  request: UpdateModelPricingOptionsRequest
) {
  const res = await api.put<UpdateOptionResponse>(
    '/api/option/model-pricing',
    request
  )
  return res.data
}

export async function updateClientPolicyOptions(
  request: UpdateClientPolicyOptionsRequest
) {
  const res = await api.put<UpdateOptionResponse>(
    '/api/option/client-policy',
    request
  )
  return res.data
}

export async function updateGroupRateLimitOptions(
  request: UpdateGroupRateLimitOptionsRequest
) {
  const res = await api.put<UpdateOptionResponse>(
    '/api/option/group-rate-limits',
    request
  )
  return res.data
}

export async function updatePasskeyDomains(
  request: UpdatePasskeyDomainsRequest
) {
  const res = await api.put<UpdatePasskeyDomainsResponse>(
    '/api/option/passkey/domains',
    request,
    {
      validateStatus: (status) =>
        (status >= 200 && status < 300) || status === 409,
    }
  )
  return res.data
}

export async function confirmPaymentCompliance() {
  const res = await api.post<ConfirmPaymentComplianceResponse>(
    '/api/option/payment_compliance',
    { confirmed: true }
  )
  return res.data
}

export async function startLogCleanupTask(targetTimestamp: number) {
  const res = await api.post<SystemTaskResponse<LogCleanupTask>>(
    '/api/system-task/log-cleanup',
    null,
    {
      params: { target_timestamp: targetTimestamp },
    }
  )
  return res.data
}

export async function getCurrentLogCleanupTask() {
  const res = await api.get<SystemTaskResponse<LogCleanupTask | null>>(
    '/api/system-task/current',
    {
      params: { type: 'log_cleanup' },
    }
  )
  return res.data
}

export async function getSystemTask<TTask = LogCleanupTask>(taskId: string) {
  const res = await api.get<SystemTaskResponse<TTask>>(
    `/api/system-task/${taskId}`
  )
  return res.data
}

export async function getModelRadarManagement(): Promise<ModelRadarManagement> {
  const res = await api.get<{
    success: boolean
    message: string
    data: ModelRadarManagement
  }>('/api/model-radar/manage')
  if (!res.data.success) throw new Error(res.data.message)
  return res.data.data
}

export async function triggerModelRadarSync() {
  const res = await api.post<
    SystemTaskResponse<Pick<SystemTask, 'task_id' | 'status' | 'type'>>
  >('/api/model-radar/sync', null, {
    validateStatus: (status) =>
      (status >= 200 && status < 300) || status === 409,
  })
  if (!res.data.data || (!res.data.success && res.status !== 409)) {
    throw new Error(res.data.message)
  }
  return { task: res.data.data, created: res.status !== 409 }
}

export async function listSystemTasks(
  limit = 20,
  filters: SystemTaskFilters = {}
) {
  const res = await api.get<SystemTaskListResponse>('/api/system-task/list', {
    params: { limit, ...filters },
  })
  return res.data
}

export async function getSystemUpdateInfo(): Promise<SystemUpdateInfo> {
  const res = await api.get<SystemUpdateResponse>('/api/system-update/check')
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Failed to check GHCR image updates')
  }
  return res.data.data
}

export async function startSystemUpdate() {
  const res = await api.post<SystemUpdateStartResponse>(
    '/api/system-update/apply'
  )
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Failed to trigger system update')
  }
  return res.data.data
}

export async function getRunningSystemVersion(): Promise<string | null> {
  const res = await api.get<{
    success: boolean
    data?: { version?: string }
  }>('/api/status', { skipErrorHandler: true })
  return res.data.data?.version ?? null
}

export async function getSystemUpdateTriggerState(): Promise<SystemUpdateTriggerState | null> {
  const res = await api.get<SystemUpdateTriggerStateResponse>(
    '/api/system-update/state',
    { skipErrorHandler: true }
  )
  return res.data.data ?? null
}

export async function resetModelRatios() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/rest_model_ratio'
  )
  return res.data
}

export async function getUpstreamChannels() {
  const res = await api.get<UpstreamChannelsResponse>(
    '/api/ratio_sync/channels'
  )
  return res.data
}

export async function fetchUpstreamRatios(request: FetchUpstreamRatiosRequest) {
  const res = await api.post<UpstreamRatiosResponse>(
    '/api/ratio_sync/fetch',
    request
  )
  return res.data
}
