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
import {
  BILLING_PRICING_VARS,
  languageTimezone,
} from '@/features/pricing/lib/billing-expr'
import {
  parseVisualBillingDocument,
  visualConditionExpression,
  type VisualCondition,
  type VisualPricingNode,
} from '@/features/pricing/lib/billing-expression/visual'

export type HourWindow = { id: string; start: string; end: string }
export type TimePricingConfig = {
  enabled: boolean
  timezone: string
  /** Weekdays that count as peak, 0=Sunday. Empty or all seven means every day. */
  weekdays: number[]
  windows: HourWindow[]
  /** Set when the existing condition cannot be represented by the schedule UI. */
  preservedCondition: string | null
  peak: Record<string, string>
  offPeak: Record<string, string>
}

const PRICE_KEYS = BILLING_PRICING_VARS.map((variable) => variable.key)
const PRICE_KEY_SET = new Set(PRICE_KEYS)
const TIME_PROBES = new Set(['hour', 'minute', 'weekday', 'month', 'day'])

/**
 * Price variables that can be expressed as a multiplier of the standard
 * (off-peak) price. Media prices stay in the advanced absolute-price section.
 */
const MULTIPLIER_PRICE_KEYS = ['p', 'c', 'cr', 'cc'] as const

/** Derive each peak price as a multiplier of the off-peak baseline. */
export function peakMultipliersFromPrices(
  peak: Record<string, string>,
  offPeak: Record<string, string>
): Record<string, string> {
  const multipliers: Record<string, string> = {}
  for (const key of MULTIPLIER_PRICE_KEYS) {
    const base = Number(offPeak[key])
    const value = Number(peak[key])
    if (
      !Number.isFinite(base) ||
      base <= 0 ||
      !Number.isFinite(value) ||
      value <= 0
    ) {
      continue
    }
    const ratio = value / base
    const rounded = Math.round(ratio * 100) / 100
    multipliers[key] = String(rounded)
  }
  return multipliers
}

/** Apply multipliers to the off-peak prices to rebuild absolute peak prices. */
export function peakPricesFromMultipliers(
  multipliers: Record<string, string>,
  offPeak: Record<string, string>
): Record<string, string> {
  const prices: Record<string, string> = {
    ...peakMultipliersFromPrices({}, offPeak),
  }
  for (const key of MULTIPLIER_PRICE_KEYS) {
    const base = Number(offPeak[key])
    const ratio = Number(multipliers[key])
    if (!Number.isFinite(base) || base <= 0 || !Number.isFinite(ratio)) {
      continue
    }
    const value = Math.round(base * ratio * 10000) / 10000
    prices[key] = String(value)
  }
  return prices
}

let nextWindowId = 0
export function createHourWindow(start: string, end: string): HourWindow {
  nextWindowId += 1
  return { id: `window-${nextWindowId}`, start, end }
}

export function defaultTimePricingConfig(): TimePricingConfig {
  return {
    enabled: false,
    timezone: 'Asia/Shanghai',
    weekdays: [1, 2, 3, 4, 5],
    windows: [createHourWindow('9', '18')],
    preservedCondition: null,
    peak: {},
    offPeak: {},
  }
}

function pricesFromNode(node: VisualPricingNode): Record<string, string> {
  if (node.kind !== 'tier') return {}
  const prices: Record<string, string> = {}
  for (const price of node.prices) {
    if (PRICE_KEY_SET.has(price.variable)) {
      prices[price.variable] = price.value
    }
  }
  return prices
}

function findTimezone(condition: VisualCondition): string | null {
  if (condition.kind === 'comparison') {
    return TIME_PROBES.has(condition.probe) ? condition.timezone : null
  }
  if (condition.kind === 'not') return findTimezone(condition.child)
  for (const child of condition.children) {
    const timezone = findTimezone(child)
    if (timezone) return timezone
  }
  return null
}

function isValidWeekday(value: string): boolean {
  const day = Number(value)
  return Number.isInteger(day) && day >= 0 && day <= 6
}

function isHourComparison(
  condition: VisualCondition,
  operator: '>=' | '<'
): boolean {
  if (condition.kind !== 'comparison' || condition.probe !== 'hour') {
    return false
  }
  if (condition.operator !== operator) return false
  const hour = Number(condition.value)
  return Number.isInteger(hour) && hour >= 0 && hour <= 24
}

/** Parse a single window node (`hour >= s && hour < e`). */
function parseWindow(node: VisualCondition): HourWindow | null {
  if (node.kind !== 'all' && node.kind !== 'any') return null
  if (node.children.length !== 2) return null
  return windowFromComparisons(node.children)
}

/** Parse a flat list that is exactly one window (`[hour >= s, hour < e]`). */
function windowFromComparisons(nodes: VisualCondition[]): HourWindow | null {
  if (nodes.length !== 2) return null
  const lower = nodes.find((node) => isHourComparison(node, '>='))
  const upper = nodes.find((node) => isHourComparison(node, '<'))
  if (
    !lower ||
    !upper ||
    lower.kind !== 'comparison' ||
    upper.kind !== 'comparison'
  ) {
    return null
  }
  return createHourWindow(lower.value, upper.value)
}

function parseWeekdays(nodes: VisualCondition[]): number[] | null {
  const equals: number[] = []
  let lower: number | null = null
  let upper: number | null = null
  for (const node of nodes) {
    if (node.kind !== 'comparison' || node.probe !== 'weekday') return null
    if (!isValidWeekday(node.value)) return null
    const day = Number(node.value)
    if (node.operator === '==') {
      equals.push(day)
    } else if (node.operator === '>=') {
      if (lower !== null) return null
      lower = day
    } else if (node.operator === '<=') {
      if (upper !== null) return null
      upper = day
    } else {
      return null
    }
  }
  if (equals.length > 0) {
    return lower === null && upper === null ? equals : null
  }
  if (lower === null && upper === null) return null
  const start = lower ?? 0
  const end = upper ?? 6
  if (start > end) return null
  return Array.from({ length: end - start + 1 }, (_, index) => start + index)
}

function extractSchedule(
  condition: VisualCondition
): Pick<TimePricingConfig, 'timezone' | 'weekdays' | 'windows'> | null {
  const timezone = findTimezone(condition)
  if (!timezone) return null

  let weekdayNodes: VisualCondition[] = []
  let windowNodes: VisualCondition[]
  if (condition.kind === 'all') {
    weekdayNodes = condition.children.filter(
      (child) => child.kind === 'comparison' && child.probe === 'weekday'
    )
    windowNodes = condition.children.filter(
      (child) => !(child.kind === 'comparison' && child.probe === 'weekday')
    )
  } else if (condition.kind === 'any') {
    windowNodes = condition.children
  } else {
    return null
  }

  let weekdays: number[] = []
  if (weekdayNodes.length > 0) {
    const parsed = parseWeekdays(weekdayNodes)
    if (!parsed) return null
    weekdays = parsed
  }

  const direct = windowFromComparisons(windowNodes)
  let windows: HourWindow[]
  if (direct) {
    windows = [direct]
  } else if (windowNodes.length === 1) {
    const single = windowNodes.at(0)
    if (!single) return null
    const window = parseWindow(single)
    if (window) {
      windows = [window]
    } else if (single.kind === 'all' || single.kind === 'any') {
      windows = []
      for (const child of single.children) {
        const parsed = parseWindow(child)
        if (!parsed) return null
        windows.push(parsed)
      }
    } else {
      return null
    }
  } else {
    windows = []
    for (const node of windowNodes) {
      const window = parseWindow(node)
      if (!window) return null
      windows.push(window)
    }
  }
  if (windows.length === 0) return null
  return { timezone, weekdays, windows }
}

/**
 * Snap an existing expression timezone onto the timezone implied by the
 * current interface language. The editor has no timezone selector; new
 * windows are written in the language's timezone while an existing condition
 * keeps whichever timezone it was saved with.
 */
export function timezoneForLanguage(
  language: string | undefined | null
): string {
  return languageTimezone(language)
}

export function parseTimePricing(expression: string): TimePricingConfig {
  const config = defaultTimePricingConfig()
  if (!expression) return config
  const document = parseVisualBillingDocument(expression)
  if (!document) return config
  const root = document.root
  if (root.kind === 'tier') {
    return { ...config, offPeak: pricesFromNode(root) }
  }
  if (root.yes.kind !== 'tier' || root.no.kind !== 'tier') return config
  const yesIsPeak = /peak/i.test(root.yes.label) && !/off/i.test(root.yes.label)
  const peakNode = yesIsPeak ? root.yes : root.no
  const offNode = yesIsPeak ? root.no : root.yes
  const schedule = extractSchedule(root.condition)
  return {
    enabled: true,
    timezone:
      schedule?.timezone ?? findTimezone(root.condition) ?? config.timezone,
    weekdays: schedule ? schedule.weekdays : config.weekdays,
    windows: schedule ? schedule.windows : config.windows,
    preservedCondition: schedule
      ? null
      : visualConditionExpression(root.condition, document.source),
    peak: pricesFromNode(peakNode),
    offPeak: pricesFromNode(offNode),
  }
}

function trimNumber(value: string): string {
  return value.trim()
}

function priceExpression(prices: Record<string, string>): string {
  const terms = PRICE_KEYS.filter((key) => {
    const raw = prices[key]
    if (raw === undefined || trimNumber(raw) === '') return false
    const value = Number(raw)
    return Number.isFinite(value) && value > 0
  }).map((key) => `${key} * ${trimNumber(prices[key])}`)
  return terms.length > 0 ? terms.join(' + ') : 'p * 0'
}

/** Whether any peak price cannot be derived from the off-peak multipliers. */
export function hasAbsolutePeakPrices(
  multipliers: Record<string, string>,
  peak: Record<string, string>,
  offPeak: Record<string, string>
): boolean {
  const derived = peakPricesFromMultipliers(multipliers, offPeak)
  return MULTIPLIER_PRICE_KEYS.some((key) => {
    const raw = peak[key]
    if (raw === undefined || trimNumber(raw) === '') return false
    const value = Number(raw)
    if (!Number.isFinite(value) || value <= 0) return false
    const expected = Number(derived[key])
    if (!Number.isFinite(expected)) return true
    return Math.abs(value - expected) > expected * 0.005 + 1e-9
  })
}

export function buildTimePricingCondition(
  timezone: string,
  weekdays: number[],
  windows: HourWindow[]
): string | null {
  const valid = windows.filter(
    (window) => trimNumber(window.start) !== '' && trimNumber(window.end) !== ''
  )
  if (valid.length === 0) return null
  const tz = JSON.stringify(timezone || 'UTC')
  const windowParts = valid.map((window) => {
    const start = Number(window.start)
    const end = Number(window.end)
    if (start < end) {
      return `hour(${tz}) >= ${start} && hour(${tz}) < ${end}`
    }
    // start > end spans midnight. start == end never matches, so treat it as a
    // full-day window instead of silently billing nothing.
    if (start === end) return `hour(${tz}) >= 0 && hour(${tz}) < 24`
    return `hour(${tz}) >= ${start} || hour(${tz}) < ${end}`
  })
  const windowExpression =
    windowParts.length === 1
      ? windowParts[0]
      : `(${windowParts.map((part) => `(${part})`).join(' || ')})`

  const days = [...new Set(weekdays)].sort((a, b) => a - b)
  if (days.length === 0 || days.length === 7) return windowExpression
  const contiguous = days.every(
    (day, index) => index === 0 || day === days[index - 1] + 1
  )
  let dayExpression: string
  if (contiguous) {
    const lastDay = days.at(-1)
    dayExpression =
      days.length === 1
        ? `weekday(${tz}) == ${days[0]}`
        : `weekday(${tz}) >= ${days[0]} && weekday(${tz}) <= ${lastDay}`
  } else {
    dayExpression = `(${days.map((day) => `weekday(${tz}) == ${day}`).join(' || ')})`
  }
  return `${dayExpression} && ${windowExpression}`
}

export function buildTimePricingExpression(config: TimePricingConfig): string {
  const offPeak = `tier("off_peak", ${priceExpression(config.offPeak)})`
  if (!config.enabled) {
    return `tier("base", ${priceExpression(config.offPeak)})`
  }
  const condition =
    config.preservedCondition ??
    buildTimePricingCondition(config.timezone, config.weekdays, config.windows)
  if (!condition) return `tier("base", ${priceExpression(config.offPeak)})`
  return `${condition} ? tier("peak", ${priceExpression(config.peak)}) : ${offPeak}`
}
