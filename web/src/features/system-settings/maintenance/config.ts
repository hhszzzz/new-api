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
import { normalizeReactIconName } from '@/lib/react-icon-name'

export type HeaderNavAccessConfig = {
  enabled: boolean
  requireAuth: boolean
}

export type HeaderNavCustomItemConfig = {
  id: string
  title: string
  url: string
  icon?: string
  enabled: boolean
}

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

export const HEADER_NAV_DEFAULT_ORDER: string[] = [...HEADER_NAV_BUILT_IN_KEYS]

export type HeaderNavModulesConfig = {
  home: boolean
  console: boolean
  pricing: HeaderNavAccessConfig
  modelStatus: HeaderNavAccessConfig
  modelRadar: HeaderNavAccessConfig
  rankings: HeaderNavAccessConfig
  docs: boolean
  about: boolean
  custom: HeaderNavCustomItemConfig[]
  order: string[]
  [key: string]:
    | boolean
    | HeaderNavAccessConfig
    | HeaderNavCustomItemConfig[]
    | string[]
}

export type SidebarSectionConfig = {
  enabled: boolean
  [key: string]: boolean
}

export type SidebarModulesAdminConfig = Record<string, SidebarSectionConfig>

export const HEADER_NAV_DEFAULT: HeaderNavModulesConfig = {
  home: true,
  console: true,
  pricing: {
    enabled: true,
    requireAuth: false,
  },
  modelStatus: {
    enabled: true,
    requireAuth: false,
  },
  modelRadar: {
    enabled: true,
    requireAuth: false,
  },
  rankings: {
    enabled: true,
    requireAuth: false,
  },
  docs: true,
  about: true,
  custom: [],
  order: HEADER_NAV_DEFAULT_ORDER,
}

export const SIDEBAR_MODULES_DEFAULT: SidebarModulesAdminConfig = {
  chat: {
    enabled: true,
    playground: true,
    chat: true,
  },
  console: {
    enabled: true,
    detail: true,
    token: true,
    log: true,
    midjourney: true,
    task: true,
  },
  personal: {
    enabled: true,
    topup: true,
    personal: true,
  },
  admin: {
    enabled: true,
    channel: true,
    models: true,
    redemption: true,
    user: true,
    setting: true,
    subscription: true,
    promptAudit: true,
  },
}

const toBoolean = (value: unknown, fallback: boolean): boolean => {
  if (typeof value === 'boolean') return value
  if (typeof value === 'number') {
    if (value === 1) return true
    if (value === 0) return false
    return fallback
  }
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase()
    if (normalized === 'true' || normalized === '1') return true
    if (normalized === 'false' || normalized === '0') return false
  }
  return fallback
}

const cloneHeaderNavDefault = (): HeaderNavModulesConfig => ({
  ...HEADER_NAV_DEFAULT,
  pricing: { ...HEADER_NAV_DEFAULT.pricing },
  modelStatus: { ...HEADER_NAV_DEFAULT.modelStatus },
  modelRadar: { ...HEADER_NAV_DEFAULT.modelRadar },
  rankings: { ...HEADER_NAV_DEFAULT.rankings },
  custom: [],
  order: [...HEADER_NAV_DEFAULT_ORDER],
})

export function getCustomHeaderNavOrderKey(id: string): string {
  return `custom:${id}`
}

function parseCustomHeaderNavItems(raw: unknown): HeaderNavCustomItemConfig[] {
  if (!Array.isArray(raw)) return []

  const seenIds = new Set<string>()
  return raw.slice(0, 20).flatMap((value) => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return []

    const item = value as Record<string, unknown>
    const id = typeof item.id === 'string' ? item.id.trim() : ''
    if (!/^[a-zA-Z0-9_-]{1,64}$/.test(id) || seenIds.has(id)) return []

    seenIds.add(id)
    return [
      {
        id,
        title: typeof item.title === 'string' ? item.title : '',
        url: typeof item.url === 'string' ? item.url : '',
        icon: normalizeReactIconName(item.icon),
        enabled: toBoolean(item.enabled, true),
      },
    ]
  })
}

function normalizeHeaderNavOrder(
  raw: unknown,
  customItems: HeaderNavCustomItemConfig[]
): string[] {
  const customKeys = customItems.map((item) =>
    getCustomHeaderNavOrderKey(item.id)
  )
  const fallback = [...HEADER_NAV_DEFAULT_ORDER, ...customKeys]
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

const parseAccessModule = (
  raw: unknown,
  fallback: HeaderNavAccessConfig
): HeaderNavAccessConfig => {
  if (
    typeof raw === 'boolean' ||
    typeof raw === 'string' ||
    typeof raw === 'number'
  ) {
    return {
      enabled: toBoolean(raw, fallback.enabled),
      requireAuth: fallback.requireAuth,
    }
  }
  if (raw && typeof raw === 'object') {
    const record = raw as Record<string, unknown>
    return {
      enabled: toBoolean(record.enabled, fallback.enabled),
      requireAuth: toBoolean(record.requireAuth, fallback.requireAuth),
    }
  }
  return { ...fallback }
}

const cloneSidebarDefault = (): SidebarModulesAdminConfig =>
  Object.entries(SIDEBAR_MODULES_DEFAULT).reduce<SidebarModulesAdminConfig>(
    (acc, [section, config]) => {
      acc[section] = { ...config }
      return acc
    },
    {}
  )

export function parseHeaderNavModules(
  value: string | null | undefined
): HeaderNavModulesConfig {
  const base = cloneHeaderNavDefault()
  if (!value) {
    return base
  }
  try {
    const parsed = JSON.parse(value) as Record<string, unknown>
    const result: HeaderNavModulesConfig = {
      ...base,
      pricing: { ...base.pricing },
      modelStatus: { ...base.modelStatus },
      modelRadar: { ...base.modelRadar },
      rankings: { ...base.rankings },
    }

    result.pricing = parseAccessModule(parsed.pricing, result.pricing)
    result.modelStatus = parseAccessModule(parsed.modelStatus, result.pricing)
    result.modelRadar = parseAccessModule(parsed.modelRadar, result.modelStatus)
    result.rankings = parseAccessModule(parsed.rankings, result.rankings)
    result.custom = parseCustomHeaderNavItems(parsed.custom)
    result.order = normalizeHeaderNavOrder(parsed.order, result.custom)

    Object.entries(parsed).forEach(([key, raw]) => {
      if (
        key === 'pricing' ||
        key === 'modelStatus' ||
        key === 'modelRadar' ||
        key === 'rankings'
      ) {
        return
      }

      if (typeof raw === 'boolean') {
        result[key] = raw
        return
      }
      if (typeof raw === 'string' || typeof raw === 'number') {
        result[key] = toBoolean(raw, Boolean(base[key]))
        return
      }
    })

    return result
  } catch {
    return base
  }
}

export function serializeHeaderNavModules(
  config: HeaderNavModulesConfig
): string {
  return JSON.stringify(config)
}

export function parseSidebarModulesAdmin(
  value: string | null | undefined
): SidebarModulesAdminConfig {
  const defaults = cloneSidebarDefault()
  // If empty string, null, or undefined, use default config
  if (!value || value.trim() === '') return defaults

  try {
    const parsed = JSON.parse(value) as Record<string, unknown>
    const result: SidebarModulesAdminConfig = {}

    Object.entries(parsed).forEach(([sectionKey, raw]) => {
      if (!raw || typeof raw !== 'object') return

      const defaultSection = defaults[sectionKey] ?? { enabled: true }
      const sectionConfig: SidebarSectionConfig = {
        enabled: toBoolean(
          (raw as Record<string, unknown>).enabled,
          defaultSection.enabled ?? true
        ),
      }

      Object.entries(raw as Record<string, unknown>).forEach(
        ([moduleKey, moduleValue]) => {
          if (moduleKey === 'enabled') return
          sectionConfig[moduleKey] = toBoolean(
            moduleValue,
            defaultSection[moduleKey] ?? true
          )
        }
      )

      result[sectionKey] = sectionConfig
    })

    // Merge defaults to ensure expected sections exist
    Object.entries(defaults).forEach(([sectionKey, config]) => {
      if (!result[sectionKey]) {
        result[sectionKey] = { ...config }
        return
      }

      Object.entries(config).forEach(([moduleKey, moduleValue]) => {
        if (!(moduleKey in result[sectionKey])) {
          result[sectionKey][moduleKey] = moduleValue
        }
      })
    })

    return result
  } catch {
    return defaults
  }
}

export function serializeSidebarModulesAdmin(
  config: SidebarModulesAdminConfig
): string {
  return JSON.stringify(config)
}
