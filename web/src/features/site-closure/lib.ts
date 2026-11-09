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
import type { SiteClosureStatus } from '@/features/auth/types'
import { ROLE } from '@/lib/roles'

const EXACT_ALLOWED_PATHS = new Set([
  '/maintenance',
  '/setup',
  '/sign-in',
  '/otp',
  '/401',
  '/403',
  '/404',
  '/500',
  '/503',
])

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function parseSiteClosureStatus(value: unknown): SiteClosureStatus {
  const root = isRecord(value) ? value : {}
  const raw = isRecord(root.site_closure) ? root.site_closure : {}
  return {
    web_closed: raw.web_closed === true,
    api_paused: raw.api_paused === true,
    title: typeof raw.title === 'string' ? raw.title : '',
    message: typeof raw.message === 'string' ? raw.message : '',
    expected_recovery_at:
      typeof raw.expected_recovery_at === 'number' &&
      Number.isFinite(raw.expected_recovery_at) &&
      raw.expected_recovery_at > 0
        ? raw.expected_recovery_at
        : 0,
    bypass: raw.bypass === true,
  }
}

export function isSiteClosurePathAllowed(pathname: string): boolean {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname
  if (EXACT_ALLOWED_PATHS.has(path)) return true
  return path === '/oauth' || path.startsWith('/oauth/')
}

export function isSiteClosureAdmin(role: number | undefined): boolean {
  return (role ?? ROLE.GUEST) >= ROLE.ADMIN
}

export function isSiteClosureBlocked(
  closure: SiteClosureStatus,
  role: number | undefined,
  pathname: string
): boolean {
  if (!closure.web_closed) return false
  if (closure.bypass || isSiteClosureAdmin(role)) return false
  return !isSiteClosurePathAllowed(pathname)
}
