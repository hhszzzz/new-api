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
import { isAxiosError } from 'axios'

import { api } from '@/lib/api'

import type {
  AccountPoolApiResponse,
  AccountPoolSettings,
  AccountPoolSettingsResponseData,
  AccountPoolSnapshot,
} from './types'

export async function getAccountPool(): Promise<
  AccountPoolApiResponse<AccountPoolSnapshot>
> {
  const response = await api.get('/api/account-pool', {
    skipErrorHandler: true,
  })
  return response.data
}

export async function refreshAccountPool(): Promise<
  AccountPoolApiResponse<AccountPoolSnapshot>
> {
  const response = await api.post('/api/account-pool/refresh', undefined, {
    skipErrorHandler: true,
  })
  return response.data
}

export async function getAccountPoolSettings(): Promise<
  AccountPoolApiResponse<AccountPoolSettingsResponseData>
> {
  const response = await api.get('/api/account-pool/settings', {
    skipErrorHandler: true,
  })
  return response.data
}

export async function updateAccountPoolSettings(
  settings: AccountPoolSettings
): Promise<AccountPoolApiResponse<AccountPoolSettingsResponseData>> {
  const response = await api.put('/api/account-pool/settings', settings, {
    skipErrorHandler: true,
  })
  return response.data
}

export function getAccountPoolApiError(error: unknown): {
  code?: string
  message?: string
  retryAfter?: number
} {
  if (!isAxiosError(error)) return {}
  const data = error.response?.data
  const record =
    data && typeof data === 'object' ? (data as Record<string, unknown>) : null
  const retryAfterHeader = error.response?.headers?.['retry-after']
  const retryAfter = Number(retryAfterHeader)
  return {
    code: typeof record?.code === 'string' ? record.code : undefined,
    message: typeof record?.message === 'string' ? record.message : undefined,
    retryAfter:
      Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined,
  }
}
