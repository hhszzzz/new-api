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
import type { AccountPoolApiResponse, AccountPoolSnapshot } from '../types'

export function getAccountPoolServerNow(
  serverTime: string | undefined,
  dataUpdatedAt: number,
  clientNow = Date.now()
) {
  const parsedServerTime = Date.parse(serverTime ?? '')
  if (!Number.isFinite(parsedServerTime) || dataUpdatedAt <= 0) return clientNow
  return parsedServerTime + Math.max(0, clientNow - dataUpdatedAt)
}

export function getAccountPoolRefetchInterval(
  response: AccountPoolApiResponse<AccountPoolSnapshot> | undefined,
  dataUpdatedAt: number,
  clientNow = Date.now()
) {
  const nextRefreshAt = response?.data.next_refresh_at
  if (!nextRefreshAt) return false
  const next = Date.parse(nextRefreshAt)
  if (!Number.isFinite(next)) return false
  const serverNow = getAccountPoolServerNow(
    response?.data.server_time,
    dataUpdatedAt,
    clientNow
  )
  return Math.max(1000, next - serverNow)
}

export function shouldRefreshAccountPoolOnVisibility(
  nextRefreshAt: string | undefined,
  now = Date.now()
) {
  if (!nextRefreshAt) return false
  const next = Date.parse(nextRefreshAt)
  return Number.isFinite(next) && next <= now
}
