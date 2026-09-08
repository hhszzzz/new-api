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
import type { PricingData } from '@/features/pricing/types'
import {
  resolveDefaultProviderIconKey,
  resolveProviderIconKey,
} from '@/lib/provider-icon'

import type {
  ModelRadarConfiguration,
  ModelRadarDegradationAlert,
  ModelRadarHistoryFrame,
  ModelRadarModelOverride,
  ModelRadarSettings,
} from '../types'

export const ALL_VENDORS = 'all'
export const OTHER_VENDOR = 'other'

export const RADAR_VENDORS = [
  {
    key: 'openai',
    label: 'OpenAI',
    icon: 'OpenAI',
    prefixes: ['chatgpt', 'gpt', 'o1', 'o3', 'o4'],
  },
  {
    key: 'anthropic',
    label: 'Anthropic',
    icon: 'Claude',
    prefixes: ['claude'],
  },
  {
    key: 'deepseek',
    label: 'DeepSeek',
    icon: 'DeepSeek',
    prefixes: ['deepseek'],
  },
  {
    key: 'google',
    label: 'Google',
    icon: 'Gemini',
    prefixes: ['gemini', 'gemma'],
  },
  { key: 'zhipu', label: 'Zhipu', icon: 'Zhipu', prefixes: ['chatglm', 'glm'] },
  { key: 'xai', label: 'xAI', icon: 'XAI', prefixes: ['grok'] },
  {
    key: 'moonshot',
    label: 'Moonshot',
    icon: 'Moonshot',
    prefixes: ['moonshot', 'kimi'],
  },
  { key: 'tencent', label: 'Tencent', icon: 'Hunyuan', prefixes: ['hunyuan'] },
  { key: 'alibaba', label: 'Alibaba', icon: 'Qwen', prefixes: ['qwen', 'qwq'] },
  {
    key: 'bytedance',
    label: 'ByteDance',
    icon: 'Doubao',
    prefixes: ['doubao'],
  },
  { key: 'minimax', label: 'MiniMax', icon: 'Minimax', prefixes: ['minimax'] },
  {
    key: 'mistral',
    label: 'Mistral',
    icon: 'Mistral',
    prefixes: ['mistral', 'mixtral'],
  },
  {
    key: 'meta',
    label: 'Meta',
    icon: 'Meta',
    prefixes: ['meta-llama', 'llama'],
  },
  {
    key: 'cohere',
    label: 'Cohere',
    icon: 'Cohere',
    prefixes: ['command', 'cohere'],
  },
  {
    key: 'perplexity',
    label: 'Perplexity',
    icon: 'Perplexity',
    prefixes: ['sonar', 'perplexity'],
  },
  {
    key: 'baidu',
    label: 'Baidu',
    icon: 'Baidu',
    prefixes: ['ernie', 'wenxin'],
  },
  { key: 'spark', label: 'Spark', icon: 'Spark', prefixes: ['spark'] },
  {
    key: 'baichuan',
    label: 'Baichuan',
    icon: 'Baichuan',
    prefixes: ['baichuan'],
  },
  {
    key: 'internlm',
    label: 'InternLM',
    icon: 'InternLM',
    prefixes: ['internlm'],
  },
  { key: 'stepfun', label: 'Stepfun', icon: 'Stepfun', prefixes: ['step'] },
  {
    key: 'xiaomi',
    label: 'XiaomiMiMo',
    icon: 'XiaomiMiMo',
    prefixes: ['mimo'],
  },
  { key: 'yi', label: 'Yi', icon: 'Yi', prefixes: ['yi'] },
]

export const BUILT_IN_MODEL_OVERRIDES: Readonly<
  Record<string, ModelRadarModelOverride>
> = {
  k3: { display_name: 'kimi-k3', vendor: 'moonshot' },
  'hy4-preview': { vendor: 'tencent' },
}

export const DEFAULT_RADAR_SETTINGS: ModelRadarSettings = {
  default_vendor: 'openai',
  show_degradation_alerts: true,
  models: {},
}

const VENDOR_KEY_PATTERN = /^[a-z0-9-]{1,32}$/

export function resolveRadarSettings(raw: unknown): ModelRadarSettings {
  const settings = {
    ...DEFAULT_RADAR_SETTINGS,
    models: {} as ModelRadarSettings['models'],
  }
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return settings
  const value = raw as Record<string, unknown>
  if (
    typeof value.default_vendor === 'string' &&
    VENDOR_KEY_PATTERN.test(value.default_vendor)
  ) {
    settings.default_vendor = value.default_vendor
  }
  if (typeof value.show_degradation_alerts === 'boolean') {
    settings.show_degradation_alerts = value.show_degradation_alerts
  }
  if (
    !value.models ||
    typeof value.models !== 'object' ||
    Array.isArray(value.models)
  ) {
    return settings
  }
  const entries: Array<[string, ModelRadarModelOverride]> = []
  for (const [key, candidate] of Object.entries(value.models).slice(0, 256)) {
    const model = key.trim()
    if (
      !model ||
      [...model].length > 128 ||
      !candidate ||
      typeof candidate !== 'object' ||
      Array.isArray(candidate)
    ) {
      continue
    }
    const override = candidate as Record<string, unknown>
    const normalized: ModelRadarModelOverride = {}
    if (typeof override.display_name === 'string') {
      const name = override.display_name.trim()
      if (name && [...name].length <= 128) normalized.display_name = name
    }
    if (
      typeof override.vendor === 'string' &&
      VENDOR_KEY_PATTERN.test(override.vendor) &&
      override.vendor !== ALL_VENDORS
    ) {
      normalized.vendor = override.vendor
    }
    if (typeof override.hidden === 'boolean') {
      normalized.hidden = override.hidden
    }
    entries.push([model, normalized])
  }
  settings.models = Object.fromEntries(entries)
  return settings
}

export function getVendorMeta(key: string): {
  key: string
  label: string
  icon: string | null
} {
  return (
    RADAR_VENDORS.find((vendor) => vendor.key === key) ?? {
      key,
      label: key === OTHER_VENDOR ? 'Other' : key,
      icon: null,
    }
  )
}

export const IQ_TEXT_CLASSES = {
  high: 'text-emerald-700 dark:text-emerald-300',
  mid: 'text-amber-700 dark:text-amber-300',
  low: 'text-destructive',
} as const

export function filterVisibleConfigurations(
  configurations: ModelRadarConfiguration[],
  settings: ModelRadarSettings = DEFAULT_RADAR_SETTINGS
): ModelRadarConfiguration[] {
  return configurations.filter(
    (item) => !resolveRadarModel(item.model, settings).hidden
  )
}

export function listVendors(
  configurations: ModelRadarConfiguration[],
  settings: ModelRadarSettings = DEFAULT_RADAR_SETTINGS,
  registry?: ModelRadarIconRegistry
): Array<{
  key: string
  label: string
  icon: string | null
  modelCount: number
}> {
  const models = new Map<string, Set<string>>()
  for (const configuration of configurations) {
    const resolved = resolveRadarModel(configuration.model, settings)
    if (resolved.hidden) continue
    const names = models.get(resolved.vendor) ?? new Set<string>()
    names.add(configuration.model)
    models.set(resolved.vendor, names)
  }
  return Array.from(models, ([key, names]) => {
    const meta = getVendorMeta(key)
    const icon = meta.icon
      ? (registry?.providerIcons.get(meta.icon.toLowerCase()) ??
        resolveDefaultProviderIconKey(meta.icon))
      : null
    return { ...meta, icon, modelCount: names.size }
  }).sort(
    (left, right) =>
      right.modelCount - left.modelCount ||
      left.label.localeCompare(right.label)
  )
}

export function filterByVendor(
  configurations: ModelRadarConfiguration[],
  settings: ModelRadarSettings,
  vendor: string
): ModelRadarConfiguration[] {
  return configurations.filter((item) => {
    const resolved = resolveRadarModel(item.model, settings)
    return (
      !resolved.hidden && (vendor === ALL_VENDORS || resolved.vendor === vendor)
    )
  })
}

export function resolveDefaultVendor(
  settings: ModelRadarSettings,
  vendors: Array<{ key: string }>
): string {
  return vendors.some((item) => item.key === settings.default_vendor)
    ? settings.default_vendor
    : ALL_VENDORS
}

export function filterAlertsByVendor(
  alerts: ModelRadarDegradationAlert[],
  configurations: ModelRadarConfiguration[],
  settings: ModelRadarSettings,
  vendor: string
): ModelRadarDegradationAlert[] {
  const keys = new Set(
    filterByVendor(configurations, settings, vendor).map((item) =>
      JSON.stringify([item.model, item.effort])
    )
  )
  return alerts.filter((alert) =>
    keys.has(JSON.stringify([alert.model, alert.effort]))
  )
}

export const EFFORT_ORDER = [
  'ultra',
  'max',
  'xhigh',
  'high',
  'medium',
  'low',
] as const

export const MODEL_COLORS = [
  '#2563eb',
  '#e11d48',
  '#059669',
  '#d97706',
  '#7c3aed',
  '#0891b2',
  '#db2777',
  '#4f46e5',
  '#65a30d',
  '#dc2626',
] as const

const MODEL_PREFIXES = RADAR_VENDORS.flatMap((vendor) =>
  vendor.prefixes.map((prefix) => ({
    prefix,
    vendor: vendor.key,
    icon: vendor.icon,
  }))
).sort((left, right) => right.prefix.length - left.prefix.length)

export type ModelRadarIconRegistry = {
  modelIcons: ReadonlyMap<string, string>
  providerIcons: ReadonlyMap<string, string>
}

function getModelIconLookupKeys(model: string): string[] {
  const normalized = model.trim().toLowerCase()
  if (!normalized) return []

  const lastSegment = normalized.match(/[^/:]+$/)?.[0]
  return lastSegment && lastSegment !== normalized
    ? [normalized, lastSegment]
    : [normalized]
}

export function createModelRadarIconRegistry(
  pricing: Pick<PricingData, 'data' | 'vendors'> | null | undefined
): ModelRadarIconRegistry {
  const modelIcons = new Map<string, string>()
  const providerIcons = new Map<string, string>()
  if (!Array.isArray(pricing?.data) || !Array.isArray(pricing.vendors)) {
    return { modelIcons, providerIcons }
  }

  const vendors = new Map(pricing.vendors.map((vendor) => [vendor.id, vendor]))
  for (const vendor of pricing.vendors) {
    const iconKey = resolveProviderIconKey(vendor.icon)
    const providerKey = iconKey?.split('.')[0]?.trim().toLowerCase()
    if (iconKey && providerKey && !providerIcons.has(providerKey)) {
      providerIcons.set(providerKey, iconKey)
    }
  }

  for (const model of pricing.data) {
    const vendor = model.vendor_id ? vendors.get(model.vendor_id) : undefined
    const iconKey = resolveProviderIconKey(vendor?.icon, model.icon)
    if (!iconKey) continue

    for (const lookupKey of getModelIconLookupKeys(model.model_name)) {
      if (!modelIcons.has(lookupKey)) modelIcons.set(lookupKey, iconKey)
    }
  }

  return { modelIcons, providerIcons }
}

// Resolves a radar model name to its vendor's @lobehub/icons key.
export function getModelIconKey(
  model: string,
  iconRegistry?: ModelRadarIconRegistry
): string | null {
  return resolveRadarModel(model, DEFAULT_RADAR_SETTINGS, iconRegistry).iconKey
}

export function resolveRadarModel(
  model: string,
  settings: ModelRadarSettings = DEFAULT_RADAR_SETTINGS,
  iconRegistry?: ModelRadarIconRegistry
) {
  const lookupKeys = getModelIconLookupKeys(model)
  // DSH identifies the DeepSeek Harness, not a different model provider.
  const underlyingModelKeys = lookupKeys
    .filter((key) => key.startsWith('dsh-deepseek-'))
    .map((key) => key.slice(4))
  const normalized = model.trim().toLowerCase()
  const candidates = [
    normalized,
    ...normalized.split(/[/:_]+/),
    ...underlyingModelKeys,
  ]
  const prefixMatch = MODEL_PREFIXES.find(({ prefix }) =>
    candidates.some((candidate) => candidate.startsWith(prefix))
  )
  const aliasKey = lookupKeys.find((key) =>
    Object.hasOwn(BUILT_IN_MODEL_OVERRIDES, key)
  )
  const alias = aliasKey ? BUILT_IN_MODEL_OVERRIDES[aliasKey] : undefined
  const override = Object.hasOwn(settings.models, model)
    ? settings.models[model]
    : undefined
  const displayName = override?.display_name || alias?.display_name || model
  const vendor =
    override?.vendor || alias?.vendor || prefixMatch?.vendor || OTHER_VENDOR
  let providerIcon = getVendorMeta(vendor).icon
  // Preserve the existing model-specific brand icons within these vendors.
  if (!override?.vendor && !alias?.vendor) {
    if (prefixMatch?.prefix === 'gemma') providerIcon = 'Google'
    if (prefixMatch?.prefix === 'wenxin') providerIcon = 'Wenxin'
  }
  const iconLookupKeys = [
    ...lookupKeys,
    ...getModelIconLookupKeys(displayName),
    ...getModelIconLookupKeys(alias?.display_name ?? ''),
    ...underlyingModelKeys,
  ]
  const configuredIcon = iconLookupKeys
    .map((key) => iconRegistry?.modelIcons.get(key))
    .find(Boolean)
  let iconKey = configuredIcon ?? null
  if (!iconKey && providerIcon) {
    iconKey =
      iconRegistry?.providerIcons.get(providerIcon.toLowerCase()) ??
      resolveDefaultProviderIconKey(providerIcon)
  }
  let source: 'override' | 'alias' | 'prefix' | 'other' = 'other'
  if (override && Object.keys(override).length > 0) source = 'override'
  else if (alias) source = 'alias'
  else if (prefixMatch) source = 'prefix'
  return {
    model,
    displayName,
    vendor,
    hidden: override?.hidden ?? false,
    iconKey,
    source,
  }
}

export function compareEfforts(left: string, right: string): number {
  const leftIndex = EFFORT_ORDER.indexOf(
    left.toLowerCase() as (typeof EFFORT_ORDER)[number]
  )
  const rightIndex = EFFORT_ORDER.indexOf(
    right.toLowerCase() as (typeof EFFORT_ORDER)[number]
  )
  if (leftIndex === -1 && rightIndex === -1) {
    return left.localeCompare(right)
  }
  if (leftIndex === -1) return 1
  if (rightIndex === -1) return -1
  return leftIndex - rightIndex
}

export type ModelRadarGroup = {
  model: string
  displayName: string
  vendor: string
  color: string
  configurations: ModelRadarConfiguration[]
}

export function groupConfigurations(
  configurations: ModelRadarConfiguration[],
  settings: ModelRadarSettings = DEFAULT_RADAR_SETTINGS
): ModelRadarGroup[] {
  const groups = new Map<string, ModelRadarConfiguration[]>()
  for (const configuration of configurations) {
    const existing = groups.get(configuration.model)
    if (existing) {
      existing.push(configuration)
    } else {
      groups.set(configuration.model, [configuration])
    }
  }

  return Array.from(groups, ([model, modelConfigurations]) => ({
    model,
    displayName: resolveRadarModel(model, settings).displayName,
    vendor: resolveRadarModel(model, settings).vendor,
    color: stableModelColor(model),
    configurations: [...modelConfigurations].sort((left, right) =>
      compareEfforts(left.effort, right.effort)
    ),
  }))
}

function stableModelColor(model: string): string {
  let hash = 2166136261
  for (let index = 0; index < model.length; index += 1) {
    hash ^= model.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return MODEL_COLORS[(hash >>> 0) % MODEL_COLORS.length]
}

export function matrixEfforts(
  configurations: ModelRadarConfiguration[]
): string[] {
  const efforts = new Set<string>()
  for (const configuration of configurations) {
    const effort = configuration.effort.trim().toLowerCase()
    if (effort) efforts.add(effort)
  }
  return [...efforts].sort(compareEfforts)
}

export function createModelColorMap(
  configurations: ModelRadarConfiguration[]
): Map<string, string> {
  return new Map(
    groupConfigurations(configurations).map((group) => [
      group.model,
      group.color,
    ])
  )
}

export function getPassRate(configuration: ModelRadarConfiguration): number {
  if (configuration.valid_tasks <= 0) return 0
  return configuration.passed / configuration.valid_tasks
}

// Builds a source-relative IQ trend, oldest first, even for stale snapshots.
export function getHistorySeries(
  history: ModelRadarHistoryFrame[],
  model: string,
  effort: string,
  windowHours = 72
): number[] {
  const latest = Math.max(...history.map((frame) => frame.ts))
  return history
    .filter((frame) => frame.ts >= latest - windowHours * 3600)
    .sort((left, right) => left.ts - right.ts)
    .flatMap((frame) =>
      frame.points
        .filter((point) => point.model === model && point.effort === effort)
        .map((point) => point.iq)
    )
}

// Maps an IQ value onto the shared capability heat scale.
// low (<50) → destructive, mid (50–85) → amber, high (≥85) → emerald.
export function getIqTone(iq: number): 'low' | 'mid' | 'high' {
  if (iq >= 85) return 'high'
  if (iq >= 50) return 'mid'
  return 'low'
}
