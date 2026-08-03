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
import { getStatus } from '@/lib/api'
import { isHttpUrl } from '@/lib/content-format'
import { normalizeReactIconName } from '@/lib/react-icon-name'

export type ModuleAccess = { enabled: boolean; requireAuth: boolean }

export type CustomHeaderNavItem = {
  id: string
  title: string
  url: string
  icon?: string
  enabled: boolean
}

export type HeaderNavModule =
  | 'rankings'
  | 'pricing'
  | 'modelStatus'
  | 'modelRadar'

export const HEADER_NAV_BUILT_IN_KEYS = [
  'home',
  'console',
  'pricing',
  'modelStatus',
  'modelRadar',
  'rankings',
  'docs',
  'about',
] as const

export const DEFAULT_HEADER_NAV_ORDER: string[] = [...HEADER_NAV_BUILT_IN_KEYS]

export type HeaderNavModules = {
  home: boolean
  console: boolean
  pricing: ModuleAccess
  modelStatus: ModuleAccess
  modelRadar: ModuleAccess
  rankings: ModuleAccess
  docs: boolean
  about: boolean
  custom: CustomHeaderNavItem[]
  order: string[]
  [key: string]: boolean | ModuleAccess | CustomHeaderNavItem[] | string[]
}

const DEFAULT_HEADER_NAV_MODULES: HeaderNavModules = {
  home: true,
  console: true,
  pricing: { enabled: true, requireAuth: false },
  modelStatus: { enabled: true, requireAuth: false },
  modelRadar: { enabled: true, requireAuth: false },
  rankings: { enabled: true, requireAuth: false },
  docs: true,
  about: true,
  custom: [],
  order: DEFAULT_HEADER_NAV_ORDER,
}

const DEFAULTS: Record<HeaderNavModule, ModuleAccess> = {
  pricing: DEFAULT_HEADER_NAV_MODULES.pricing,
  modelStatus: DEFAULT_HEADER_NAV_MODULES.modelStatus,
  modelRadar: DEFAULT_HEADER_NAV_MODULES.modelRadar,
  rankings: DEFAULT_HEADER_NAV_MODULES.rankings,
}

function cloneHeaderNavDefaults(): HeaderNavModules {
  return {
    ...DEFAULT_HEADER_NAV_MODULES,
    pricing: { ...DEFAULT_HEADER_NAV_MODULES.pricing },
    modelStatus: { ...DEFAULT_HEADER_NAV_MODULES.modelStatus },
    modelRadar: { ...DEFAULT_HEADER_NAV_MODULES.modelRadar },
    rankings: { ...DEFAULT_HEADER_NAV_MODULES.rankings },
    custom: [],
    order: [...DEFAULT_HEADER_NAV_ORDER],
  }
}

export function getCustomHeaderNavOrderKey(id: string): string {
  return `custom:${id}`
}

export function getCustomHeaderNavPath(id: string): string {
  return `/custom/${encodeURIComponent(id)}`
}

export function parseHeaderNavBoolean(
  raw: unknown,
  fallback: boolean
): boolean {
  if (typeof raw === 'boolean') return raw
  if (typeof raw === 'number') {
    if (raw === 1) return true
    if (raw === 0) return false
    return fallback
  }
  if (typeof raw === 'string') {
    const normalized = raw.trim().toLowerCase()
    if (normalized === 'true' || normalized === '1') return true
    if (normalized === 'false' || normalized === '0') return false
  }
  return fallback
}

function parseAccess(raw: unknown, fallback: ModuleAccess): ModuleAccess {
  if (
    typeof raw === 'boolean' ||
    typeof raw === 'number' ||
    typeof raw === 'string'
  ) {
    return {
      enabled: parseHeaderNavBoolean(raw, fallback.enabled),
      requireAuth: fallback.requireAuth,
    }
  }
  if (raw && typeof raw === 'object') {
    const r = raw as Record<string, unknown>
    return {
      enabled: parseHeaderNavBoolean(r.enabled, fallback.enabled),
      requireAuth: parseHeaderNavBoolean(r.requireAuth, fallback.requireAuth),
    }
  }
  return { ...fallback }
}

function parseHeaderNavRecord(raw: unknown): Record<string, unknown> | null {
  if (!raw || String(raw).trim() === '') return null
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
    return raw as Record<string, unknown>
  }

  try {
    return JSON.parse(String(raw)) as Record<string, unknown>
  } catch {
    return null
  }
}

function parseCustomHeaderNavItems(raw: unknown): CustomHeaderNavItem[] {
  if (!Array.isArray(raw)) return []

  const seenIds = new Set<string>()
  return raw.slice(0, 20).flatMap((value) => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return []

    const item = value as Record<string, unknown>
    const id = typeof item.id === 'string' ? item.id.trim() : ''
    const title = typeof item.title === 'string' ? item.title.trim() : ''
    const url = typeof item.url === 'string' ? item.url.trim() : ''
    const icon = normalizeReactIconName(item.icon)
    if (
      !/^[a-zA-Z0-9_-]{1,64}$/.test(id) ||
      seenIds.has(id) ||
      title.length === 0 ||
      [...title].length > 40 ||
      url.length > 2048 ||
      !isHttpUrl(url)
    ) {
      return []
    }

    seenIds.add(id)
    return [
      {
        id,
        title,
        url,
        icon,
        enabled: parseHeaderNavBoolean(item.enabled, true),
      },
    ]
  })
}

function normalizeHeaderNavOrder(
  raw: unknown,
  customItems: CustomHeaderNavItem[]
): string[] {
  const customKeys = customItems.map((item) =>
    getCustomHeaderNavOrderKey(item.id)
  )
  const fallback = [...DEFAULT_HEADER_NAV_ORDER, ...customKeys]
  if (!Array.isArray(raw)) return fallback

  const allowed = new Set(fallback)
  const seen = new Set<string>()
  const restored = raw.flatMap((value) => {
    if (typeof value !== 'string' || !allowed.has(value) || seen.has(value)) {
      return []
    }
    seen.add(value)
    return [value]
  })

  return [...restored, ...fallback.filter((key) => !seen.has(key))]
}

export function parseHeaderNavModules(raw: unknown): HeaderNavModules {
  const result = cloneHeaderNavDefaults()
  const parsed = parseHeaderNavRecord(raw)
  if (!parsed) return result

  result.pricing = parseAccess(parsed.pricing, result.pricing)
  result.modelStatus = parseAccess(parsed.modelStatus, result.pricing)
  result.modelRadar = parseAccess(parsed.modelRadar, result.modelStatus)
  result.rankings = parseAccess(parsed.rankings, result.rankings)
  result.custom = parseCustomHeaderNavItems(parsed.custom)
  result.order = normalizeHeaderNavOrder(parsed.order, result.custom)

  Object.entries(parsed).forEach(([key, value]) => {
    if (
      key === 'pricing' ||
      key === 'modelStatus' ||
      key === 'modelRadar' ||
      key === 'rankings'
    ) {
      return
    }

    const fallback = result[key]
    if (
      typeof fallback === 'boolean' ||
      typeof value === 'boolean' ||
      typeof value === 'number' ||
      typeof value === 'string'
    ) {
      result[key] = parseHeaderNavBoolean(
        value,
        typeof fallback === 'boolean' ? fallback : true
      )
    }
  })

  return result
}

export function parseHeaderNavModulesFromStatus(
  status: Record<string, unknown> | null
): HeaderNavModules {
  return parseHeaderNavModules(status?.HeaderNavModules)
}

export function getCustomHeaderNavItemFromStatus(
  status: Record<string, unknown> | null,
  id: string
): CustomHeaderNavItem | null {
  const item = parseHeaderNavModulesFromStatus(status).custom.find(
    (candidate) => candidate.enabled && candidate.id === id
  )
  return item ? { ...item } : null
}

function getCachedStatus(): Record<string, unknown> | null {
  try {
    if (typeof window === 'undefined') return null
    const raw = window.localStorage.getItem('status')
    return raw ? (JSON.parse(raw) as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function cacheStatus(status: Record<string, unknown> | null): void {
  try {
    if (typeof window !== 'undefined' && status) {
      window.localStorage.setItem('status', JSON.stringify(status))
    }
  } catch {
    /* empty */
  }
}

export function getModuleAccessFromStatus(
  status: Record<string, unknown> | null,
  module: HeaderNavModule
): ModuleAccess {
  return parseHeaderNavModulesFromStatus(status)[module] ?? DEFAULTS[module]
}

export function getModuleAccess(module: HeaderNavModule): ModuleAccess {
  return getModuleAccessFromStatus(getCachedStatus(), module)
}

export async function getFreshModuleAccess(
  module: HeaderNavModule
): Promise<ModuleAccess> {
  try {
    const status = (await getStatus()) as Record<string, unknown> | null
    cacheStatus(status)
    return getModuleAccessFromStatus(status, module)
  } catch {
    return { enabled: false, requireAuth: true }
  }
}

export async function getFreshCustomHeaderNavItem(
  id: string
): Promise<CustomHeaderNavItem | null> {
  try {
    const status = (await getStatus()) as Record<string, unknown> | null
    cacheStatus(status)
    return getCustomHeaderNavItemFromStatus(status, id)
  } catch {
    return null
  }
}

export function isSidebarModuleEnabled(
  section: string,
  module: string
): boolean {
  const status = getCachedStatus()
  if (!status) return true

  const raw = status.SidebarModulesAdmin
  if (!raw || String(raw).trim() === '') return true

  try {
    const parsed = JSON.parse(String(raw)) as Record<
      string,
      Record<string, boolean>
    >
    const sectionConfig = parsed[section]
    if (!sectionConfig) return true
    if (sectionConfig.enabled === false) return false
    if (sectionConfig[module] === false) return false
    return true
  } catch {
    return true
  }
}
