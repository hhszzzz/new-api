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
import type { RadarAutoEffortPolicy } from '@/features/profile/types'
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
  auto_effort_enabled: true,
  models: {},
}

const VENDOR_KEY_PATTERN = /^[a-z0-9-]{1,32}$/

/** Limits the backend enforces on administrator-declared model aliases. */
export const MAX_RADAR_ALIASES_PER_MODEL = 16
export const MAX_RADAR_ALIAS_RUNES = 128

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
  if (typeof value.auto_effort_enabled === 'boolean') {
    settings.auto_effort_enabled = value.auto_effort_enabled
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
    if (override.auto_effort === true) {
      normalized.auto_effort = true
    }
    if (Array.isArray(override.aliases)) {
      const aliases: string[] = []
      for (const alias of override.aliases.slice(
        0,
        MAX_RADAR_ALIASES_PER_MODEL
      )) {
        if (typeof alias !== 'string') continue
        const name = alias.trim().toLowerCase()
        if (
          !name ||
          [...name].length > MAX_RADAR_ALIAS_RUNES ||
          name === model.toLowerCase()
        ) {
          continue
        }
        if (!aliases.includes(name)) aliases.push(name)
      }
      if (aliases.length) normalized.aliases = aliases
    }
    entries.push([model, normalized])
  }
  settings.models = Object.fromEntries(entries)
  return settings
}

/**
 * Splits a comma- or whitespace-separated alias list into normalized gateway
 * model names: lowercased, trimmed, de-duplicated, order preserved.
 */
export function splitRadarAliases(raw: string): string[] {
  const names: string[] = []
  for (const part of raw.split(/[,\s]+/)) {
    const name = part.trim().toLowerCase()
    if (!name || names.includes(name)) continue
    names.push(name)
  }
  return names
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

// ============================================================================
// Radar auto-effort
// ============================================================================

// Gateway-owned reasoning tiers, weakest first (the tie-break order). The radar
// publishes tiers the gateway cannot express (ultra), so those never take part
// in automatic selection.
export const GATEWAY_EFFORT_ORDER = [
  'none',
  'minimal',
  'low',
  'medium',
  'high',
  'xhigh',
  'max',
] as const

export type GatewayEffort = (typeof GATEWAY_EFFORT_ORDER)[number]

// A tier graded on fewer tasks than this is too noisy to drive an automatic
// change. The backend applies the same floor.
export const AUTO_EFFORT_MIN_VALID_TASKS = 3

export const AUTO_EFFORT_POLICIES = [
  {
    value: 'highest_iq',
    labelKey: 'Highest IQ',
    descriptionKey: 'Always use the tier with the highest radar IQ',
  },
  {
    value: 'iq_per_cost',
    labelKey: 'Best value for the price',
    descriptionKey: 'Balance IQ against price and pick the best value tier',
  },
  {
    value: 'min_iq_delta',
    labelKey: 'Only when clearly better',
    descriptionKey:
      'Switch when the best tier beats your current one by a certain IQ',
  },
] as const

/**
 * IQ-gap bounds the backend accepts for the min_iq_delta strategy. Zero is
 * excluded here because the API reads a zero gap as "use the default", so the
 * stored value would not be what the field shows.
 */
export const MIN_IQ_DELTA_RANGE = { min: 1, max: 150 } as const

export type RadarAutoEffortCandidate = {
  effort: GatewayEffort
  radarEffort: string
  iq: number
  priceUsd: number | null
}

export type RadarAutoEffortPick = {
  effort: GatewayEffort
  iq: number
  /** False when the client's own tier is already the chosen one. */
  changed: boolean
}

function asGatewayEffort(effort: string): GatewayEffort | null {
  const normalized = effort.trim().toLowerCase()
  return (
    GATEWAY_EFFORT_ORDER.find((candidate) => candidate === normalized) ?? null
  )
}

function radarCandidatePrice(
  configuration: ModelRadarConfiguration
): number | null {
  if (configuration.average_price_usd !== null) {
    return configuration.average_price_usd
  }
  const offPeak = configuration.average_price_usd_by_band?.off_peak ?? null
  const peak = configuration.average_price_usd_by_band?.peak ?? null
  if (offPeak === null || peak === null) return null
  return (offPeak + peak) / 2
}

// Orders the tiers a radar model may be switched to: descending IQ, and on
// equal IQ the weaker tier first so a tie resolves to the cheaper tier.
export function gatewayEffortCandidates(
  configurations: ModelRadarConfiguration[],
  minValidTasks = AUTO_EFFORT_MIN_VALID_TASKS
): RadarAutoEffortCandidate[] {
  const byEffort = new Map<GatewayEffort, RadarAutoEffortCandidate>()
  for (const configuration of configurations) {
    if (configuration.valid_tasks < minValidTasks) continue
    const effort = asGatewayEffort(configuration.effort)
    if (!effort) continue
    const existing = byEffort.get(effort)
    if (existing && existing.iq >= configuration.iq) continue
    byEffort.set(effort, {
      effort,
      radarEffort: configuration.effort.trim().toLowerCase(),
      iq: configuration.iq,
      priceUsd: radarCandidatePrice(configuration),
    })
  }
  return [...byEffort.values()].sort(
    (left, right) =>
      right.iq - left.iq ||
      GATEWAY_EFFORT_ORDER.indexOf(left.effort) -
        GATEWAY_EFFORT_ORDER.indexOf(right.effort)
  )
}

// Mirrors the gateway's selection so the page can preview the replacement.
export function pickAutoEffort(
  candidates: RadarAutoEffortCandidate[],
  policy: RadarAutoEffortPolicy,
  minIQDelta: number,
  clientEffort?: string
): RadarAutoEffortPick | null {
  if (!candidates.length) return null
  const client = clientEffort ? asGatewayEffort(clientEffort) : null
  let best = candidates[0]
  if (policy === 'iq_per_cost') {
    const priced = candidates.filter(
      (candidate) => candidate.priceUsd !== null && candidate.priceUsd > 0
    )
    if (priced.length) {
      best = priced.reduce((left, right) =>
        right.iq / (right.priceUsd ?? 1) > left.iq / (left.priceUsd ?? 1)
          ? right
          : left
      )
    }
  }
  if (policy === 'min_iq_delta' && client) {
    const current = candidates.find((candidate) => candidate.effort === client)
    if (current && best.iq - current.iq < minIQDelta) {
      return { effort: current.effort, iq: current.iq, changed: false }
    }
  }
  if (client && best.effort === client) {
    return { effort: best.effort, iq: best.iq, changed: false }
  }
  return { effort: best.effort, iq: best.iq, changed: true }
}

function modelLookupKeys(model: string): string[] {
  const key = model.trim().toLowerCase()
  if (!key) return []
  const tail = key.slice(key.lastIndexOf('/') + 1)
  return tail && tail !== key ? [key, tail] : [key]
}

// A radar model covers a user's model when either name matches directly or the
// gateway name resolves through an administrator-declared alias.
export function matchRadarModelToUserModels(
  model: string,
  aliases: string[] | undefined,
  userModels: string[]
): boolean {
  const available = new Set(userModels.flatMap(modelLookupKeys))
  return [model, ...(aliases ?? [])].some((name) =>
    modelLookupKeys(name).some((key) => available.has(key))
  )
}

/**
 * Reports whether the administrator opted this radar model into automatic
 * reasoning-tier replacement. Radar auto-effort is opt-in per model.
 */
export function isRadarAutoEffortAllowed(
  settings: ModelRadarSettings | undefined,
  model: string
): boolean {
  return settings?.models[model]?.auto_effort === true
}
