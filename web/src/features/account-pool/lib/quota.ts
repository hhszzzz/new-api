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
import type {
  AccountPoolAccount,
  AccountPoolWindow,
  AccountPoolWindowGroup,
} from '../types'

export function formatAccountPoolCountdown(target: string | null, now: number) {
  if (!target) return null
  const targetTime = Date.parse(target)
  if (!Number.isFinite(targetTime)) return null
  const totalSeconds = Math.max(0, Math.ceil((targetTime - now) / 1000))
  if (totalSeconds <= 0) return '0s'
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m ${seconds}s`
  return `${seconds}s`
}

export function getAccountPoolSecondaryWindowLabel(
  window: AccountPoolWindow | null
) {
  const seconds = window?.limit_window_seconds
  if (seconds !== null && seconds !== undefined && seconds >= 28 * 86400) {
    return 'Monthly quota'
  }
  return 'Weekly quota'
}

// Upstream group display names (e.g. antigravity) mapped to i18n keys;
// unknown labels fall back to the raw upstream text.
const ACCOUNT_POOL_WINDOW_GROUP_LABEL_KEYS: Record<string, string> = {
  'gemini models': 'Gemini models',
  'claude and gpt models': 'Claude and GPT models',
}

export function getAccountPoolWindowGroupLabelKey(label: string): string {
  return (
    ACCOUNT_POOL_WINDOW_GROUP_LABEL_KEYS[label.trim().toLowerCase()] ?? label
  )
}

// Older snapshots and error rows carry no window_groups; fall back to the
// top-level primary/secondary windows as a single unlabeled group.
export function getAccountPoolWindowGroups(
  account: Pick<AccountPoolAccount, 'primary_window' | 'secondary_window' | 'window_groups'>
): AccountPoolWindowGroup[] {
  if (account.window_groups?.length) return account.window_groups
  return [
    {
      label: null,
      primary_window: account.primary_window ?? null,
      secondary_window: account.secondary_window ?? null,
    },
  ]
}
