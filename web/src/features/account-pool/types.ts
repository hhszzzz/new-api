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
export type AccountPoolStatus =
  | 'available'
  | 'limited'
  | 'disabled'
  | 'unavailable'
  | 'error'

export type AccountPoolProvider = 'codex' | 'claude' | 'antigravity'

export type AccountPoolSummaryCounts = {
  total: number
  available: number
  limited: number
  error: number
}

export type AccountPoolWindow = {
  used_percent: number | null
  remaining_percent: number | null
  reset_at: string | null
  limit_window_seconds: number | null
}

export type AccountPoolWindowGroup = {
  label: string | null
  primary_window: AccountPoolWindow | null
  secondary_window: AccountPoolWindow | null
}

export type AccountPoolAccount = {
  public_id: string
  display_name: string
  provider: AccountPoolProvider
  email?: string
  status: AccountPoolStatus
  plan: string
  subscription_active_until: string | null
  primary_window: AccountPoolWindow | null
  secondary_window: AccountPoolWindow | null
  window_groups: AccountPoolWindowGroup[]
  updated_at: string
  stale: boolean
}

export type AccountPoolSnapshot = {
  server_time: string
  updated_at: string
  next_refresh_at: string
  manual_refresh_available_at: string
  stale: boolean
  partial: boolean
  summary: AccountPoolSummaryCounts
  provider_summaries: Partial<Record<AccountPoolProvider, AccountPoolSummaryCounts>>
  accounts: AccountPoolAccount[]
}

export type AccountPoolSettings = {
  enabled: boolean
  hide_email_from_non_admins: boolean
  provider_groups: Partial<Record<AccountPoolProvider, string[]>>
  regular_refresh_seconds: number
  near_reset_threshold_seconds: number
  near_reset_refresh_seconds: number
  post_reset_delay_seconds: number
  manual_refresh_cooldown_seconds: number
}

export type AccountPoolSettingsStatus = {
  management_key_configured: boolean
  management_ready: boolean
  last_sync_at: string | null
  last_sync_status: 'never' | 'success' | 'partial' | 'failed'
}

export type AccountPoolSettingsResponseData = AccountPoolSettings &
  AccountPoolSettingsStatus

export type AccountPoolApiResponse<T> = {
  success: boolean
  message: string
  code?: string
  data: T
}
