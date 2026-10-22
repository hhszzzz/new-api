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
import { STATUS_PRIORITY } from '@/features/performance-metrics/lib/model-status'
import type { ModelStatusModel } from '@/features/performance-metrics/status-types'

import {
  SORT_OPTIONS,
  FILTER_ALL,
  QUOTA_TYPES,
  ENDPOINT_TYPES,
  type SortOption,
} from '../constants'
import type { PricingModel } from '../types'
import {
  getCardExamplePrice,
  getDynamicPricingSummary,
  isUnconfiguredTaskUsageModel,
} from './dynamic-price'
import {
  getBillingModeLabelKey,
  type BillingModeLabelKey,
} from './billing-mode'
import { isTokenBasedModel } from './model-helpers'
import { calculateTokenPrice } from './price'

// ----------------------------------------------------------------------------
// Filter Utilities
// ----------------------------------------------------------------------------

/**
 * Filter models by search query
 */
export function filterBySearch(
  models: PricingModel[],
  query: string
): PricingModel[] {
  if (!query) return models

  const lowerQuery = query.toLowerCase()
  return models.filter(
    (m) =>
      m.model_name?.toLowerCase().includes(lowerQuery) ||
      m.description?.toLowerCase().includes(lowerQuery) ||
      m.tags?.toLowerCase().includes(lowerQuery) ||
      m.vendor_name?.toLowerCase().includes(lowerQuery)
  )
}

/**
 * Filter models by vendor
 */
export function filterByVendor(
  models: PricingModel[],
  vendor: string
): PricingModel[] {
  if (vendor === FILTER_ALL) return models
  return models.filter((m) => m.vendor_name === vendor)
}

/**
 * Filter models by group
 */
export function filterByGroup(
  models: PricingModel[],
  group: string
): PricingModel[] {
  if (group === FILTER_ALL) return models
  return models.filter((m) => m.enable_groups?.includes(group))
}

/**
 * Filter models by quota type. Every model lands in exactly one bucket: the
 * bucket names are the billing badges, so a filtered list never disagrees
 * with the badge shown on each row.
 */
export function filterByQuotaType(
  models: PricingModel[],
  quotaType: string
): PricingModel[] {
  if (quotaType === QUOTA_TYPES.ALL) return models
  const labels: Partial<Record<string, BillingModeLabelKey>> = {
    [QUOTA_TYPES.TOKEN]: 'Token-based',
    [QUOTA_TYPES.REQUEST]: 'Per Request',
    [QUOTA_TYPES.DYNAMIC]: 'Dynamic Pricing',
    [QUOTA_TYPES.TASK]: 'Task billing',
  }
  const target = labels[quotaType]
  if (!target) return models
  return models.filter((m) => getBillingModeLabelKey(m) === target)
}

/**
 * Filter models by endpoint type
 */
export function filterByEndpointType(
  models: PricingModel[],
  endpointType: string
): PricingModel[] {
  if (endpointType === ENDPOINT_TYPES.ALL) return models
  return models.filter((m) =>
    m.supported_endpoint_types?.includes(endpointType)
  )
}

export function filterModelsByStatus(
  models: PricingModel[],
  statuses: ReadonlyMap<string, ModelStatusModel> | undefined,
  health: string,
  sortBy: string
): PricingModel[] {
  if (!statuses) return models
  const filtered =
    health === FILTER_ALL
      ? models
      : models.filter(
          (model) => statuses.get(model.model_name)?.status === health
        )
  if (sortBy !== SORT_OPTIONS.STATUS) return filtered
  return [...filtered].sort((left, right) => {
    const leftStatus = statuses.get(left.model_name)?.status
    const rightStatus = statuses.get(right.model_name)?.status
    return (
      (leftStatus ? STATUS_PRIORITY[leftStatus] : 4) -
        (rightStatus ? STATUS_PRIORITY[rightStatus] : 4) ||
      left.model_name.localeCompare(right.model_name)
    )
  })
}

type SortPriceKind = 'input' | 'output' | 'request' | 'price'

const PRICE_SORTS: Partial<
  Record<SortOption, { kind: SortPriceKind; dir: 1 | -1 }>
> = {
  [SORT_OPTIONS.PRICE_LOW]: { kind: 'price', dir: 1 },
  [SORT_OPTIONS.PRICE_HIGH]: { kind: 'price', dir: -1 },
  [SORT_OPTIONS.INPUT_PRICE_LOW]: { kind: 'input', dir: 1 },
  [SORT_OPTIONS.INPUT_PRICE_HIGH]: { kind: 'input', dir: -1 },
  [SORT_OPTIONS.OUTPUT_PRICE_LOW]: { kind: 'output', dir: 1 },
  [SORT_OPTIONS.OUTPUT_PRICE_HIGH]: { kind: 'output', dir: -1 },
  [SORT_OPTIONS.REQUEST_PRICE_LOW]: { kind: 'request', dir: 1 },
  [SORT_OPTIONS.REQUEST_PRICE_HIGH]: { kind: 'request', dir: -1 },
}

/** Sort by the card's example total for generic price ordering. */
function getSortPrice(
  model: PricingModel,
  kind: SortPriceKind
): number | undefined {
  if (kind === 'price') {
    const example = getCardExamplePrice(model, { tokenUnit: 'M' })
    if (example) return example.total
    const summary = getDynamicPricingSummary(model, { tokenUnit: 'M' })
    if (summary) {
      return summary.entries.find(
        (entry) =>
          entry.field === 'inputPrice' ||
          entry.field === 'fixedPrice' ||
          entry.field === 'modelPrice'
      )?.value
    }
    return isTokenBasedModel(model)
      ? calculateTokenPrice(model, 'input', 1)
      : model.model_price
  }
  if (kind === 'request') {
    const example = getCardExamplePrice(model, { tokenUnit: 'M' })
    if (example) return example.total
  }
  const summary = getDynamicPricingSummary(model, { tokenUnit: 'M' })
  if (kind === 'request') {
    if (summary) {
      return summary.entries.find(
        (entry) => entry.field === 'fixedPrice' || entry.field === 'modelPrice'
      )?.value
    }
    return isTokenBasedModel(model) ? undefined : model.model_price
  }
  if (summary) {
    const field = kind === 'input' ? 'inputPrice' : 'outputPrice'
    return summary.entries.find(
      (entry) => entry.field === field && entry.unit === 'token'
    )?.value
  }
  if (!isTokenBasedModel(model) || isUnconfiguredTaskUsageModel(model)) {
    return undefined
  }
  return calculateTokenPrice(model, kind, 1)
}

/**
 * Sort models by specified option
 */
export function sortModels(
  models: PricingModel[],
  sortBy: string
): PricingModel[] {
  const sorted = [...models]

  const priceSort = PRICE_SORTS[sortBy as SortOption]
  if (priceSort) {
    // Extracting the price parses the billing expression, so compute once
    const price = new Map(
      sorted.map((m) => [m, getSortPrice(m, priceSort.kind)])
    )
    // Models without this kind of price stay last in both directions
    sorted.sort((a, b) => {
      const priceA = price.get(a)
      const priceB = price.get(b)
      if (priceA === undefined || priceB === undefined) {
        return Number(priceA === undefined) - Number(priceB === undefined)
      }
      return priceSort.dir * (priceA - priceB)
    })
    return sorted
  }

  switch (sortBy) {
    case SORT_OPTIONS.NAME:
    case SORT_OPTIONS.STATUS:
      sorted.sort((a, b) =>
        (a.model_name || '').localeCompare(b.model_name || '')
      )
      break
  }

  return sorted
}

/**
 * Apply all filters and sorting to models
 */
export function filterAndSortModels(
  models: PricingModel[],
  filters: {
    search: string
    vendor: string
    group: string
    quotaType: string
    endpointType: string
    tag: string
    sortBy: string
  }
): PricingModel[] {
  let result = filterBySearch(models, filters.search)
  result = filterByVendor(result, filters.vendor)
  result = filterByGroup(result, filters.group)
  result = filterByQuotaType(result, filters.quotaType)
  result = filterByEndpointType(result, filters.endpointType)
  result = filterByTag(result, filters.tag)
  result = sortModels(result, filters.sortBy)

  return result
}

/**
 * Parse tags from comma-separated string
 */
export function parseTags(tagsString?: string): string[] {
  if (!tagsString) return []
  return tagsString
    .split(/[,;|\s]+/)
    .map((t) => t.trim())
    .filter(Boolean)
}

/**
 * Extract all unique tags from models
 */
export function extractAllTags(models: PricingModel[]): string[] {
  const tagSet = new Set<string>()

  models.forEach((model) => {
    if (model.tags) {
      const tags = parseTags(model.tags)
      tags.forEach((tag) => {
        tagSet.add(tag.toLowerCase())
      })
    }
  })

  return [...tagSet].sort((a, b) => a.localeCompare(b))
}

/**
 * Filter models by tag
 */
export function filterByTag(
  models: PricingModel[],
  tag: string
): PricingModel[] {
  if (tag === FILTER_ALL) return models

  const tagLower = tag.toLowerCase()
  return models.filter((m) => {
    if (!m.tags) return false
    const modelTags = parseTags(m.tags).map((t) => t.toLowerCase())
    return modelTags.includes(tagLower)
  })
}
