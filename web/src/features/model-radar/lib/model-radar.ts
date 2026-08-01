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

import type { ModelRadarConfiguration, ModelRadarHistoryFrame } from '../types'

export const EFFORT_ORDER = [
  'low',
  'medium',
  'high',
  'xhigh',
  'max',
  'ultra',
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

// Lowercase model-name prefixes mapped to direct @lobehub/icons provider keys,
// matching the icon rendering used by Rankings.
// Ordered longest-first so specific prefixes win over generic ones.
const MODEL_ICON_PREFIXES: Array<[prefix: string, icon: string]> = [
  ['chatgpt', 'OpenAI'],
  ['gpt', 'OpenAI'],
  ['o1', 'OpenAI'],
  ['o3', 'OpenAI'],
  ['o4', 'OpenAI'],
  ['claude', 'Claude'],
  ['gemini', 'Gemini'],
  ['gemma', 'Google'],
  ['deepseek', 'DeepSeek'],
  ['qwq', 'Qwen'],
  ['qwen', 'Qwen'],
  ['doubao', 'Doubao'],
  ['kimi', 'Moonshot'],
  ['moonshot', 'Moonshot'],
  ['grok', 'XAI'],
  ['mistral', 'Mistral'],
  ['mixtral', 'Mistral'],
  ['minimax', 'Minimax'],
  ['hunyuan', 'Hunyuan'],
  ['meta-llama', 'Meta'],
  ['llama', 'Meta'],
  ['chatglm', 'Zhipu'],
  ['glm', 'Zhipu'],
  ['ernie', 'Baidu'],
  ['wenxin', 'Wenxin'],
  ['spark', 'Spark'],
  ['command', 'Cohere'],
  ['cohere', 'Cohere'],
  ['sonar', 'Perplexity'],
  ['perplexity', 'Perplexity'],
  ['baichuan', 'Baichuan'],
  ['internlm', 'InternLM'],
  ['step', 'Stepfun'],
  ['mimo', 'XiaomiMiMo'],
  ['yi', 'Yi'],
]

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
  for (const lookupKey of getModelIconLookupKeys(model)) {
    const configuredIcon = iconRegistry?.modelIcons.get(lookupKey)
    if (configuredIcon) return configuredIcon
  }

  const normalized = model.trim().toLowerCase()
  const candidates = [normalized, ...normalized.split(/[/:_]+/)]
  for (const [prefix, icon] of MODEL_ICON_PREFIXES) {
    if (candidates.some((candidate) => candidate.startsWith(prefix))) {
      return (
        iconRegistry?.providerIcons.get(icon.toLowerCase()) ??
        resolveDefaultProviderIconKey(icon)
      )
    }
  }
  return null
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
  color: string
  configurations: ModelRadarConfiguration[]
}

export function groupConfigurations(
  configurations: ModelRadarConfiguration[]
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
  const knownEfforts = new Set<string>(EFFORT_ORDER)
  const unknownEfforts = new Set<string>()
  for (const configuration of configurations) {
    const effort = configuration.effort.trim().toLowerCase()
    if (effort && !knownEfforts.has(effort)) unknownEfforts.add(effort)
  }
  return [
    ...EFFORT_ORDER,
    ...[...unknownEfforts].sort((left, right) => compareEfforts(left, right)),
  ]
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

export function compareModelsByBestIq(
  left: ModelRadarGroup,
  right: ModelRadarGroup
): number {
  const leftBest = Math.max(...left.configurations.map((item) => item.iq))
  const rightBest = Math.max(...right.configurations.map((item) => item.iq))
  if (leftBest !== rightBest) return rightBest - leftBest
  return left.model.localeCompare(right.model)
}

// Builds the 48h IQ sparkline for one configuration, oldest first.
export function getHistorySeries(
  history: ModelRadarHistoryFrame[],
  model: string,
  effort: string
): number[] {
  return history.flatMap((frame) =>
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
