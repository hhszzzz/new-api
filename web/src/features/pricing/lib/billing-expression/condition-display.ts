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
import type { TOptions } from 'i18next'

import { languageTimezone } from '@/features/pricing/lib/billing-expr'
import { toIntlLocale } from '@/i18n/languages'

import { flattenBinary } from './display'
import { compileBillingExpression } from './parser'
import { TIME_FUNCTIONS, type ExpressionNode, type TimeFunction } from './types'

type Translate = (key: string, options?: TOptions) => string
type Description = {
  text: string
  kind: 'calendar' | 'clock' | 'combined'
  timezone: string
  disjunction?: boolean
}
type TimeComparison = {
  name: TimeFunction
  timezone: string
  operator: string
  value: number
}
const DOMAINS: Record<TimeFunction, [number, number]> = {
  hour: [0, 24],
  minute: [0, 60],
  weekday: [0, 7],
  month: [1, 13],
  day: [1, 32],
}

function timeComparison(node: ExpressionNode): TimeComparison | null {
  if (
    node.kind !== 'binary' ||
    !['==', '>=', '>', '<=', '<'].includes(node.operator) ||
    node.left.kind !== 'call' ||
    !(TIME_FUNCTIONS as readonly string[]).includes(node.left.name) ||
    node.left.args[0].kind !== 'literal' ||
    typeof node.left.args[0].value !== 'string' ||
    node.right.kind !== 'literal' ||
    typeof node.right.value !== 'number' ||
    !Number.isInteger(node.right.value)
  ) {
    return null
  }
  return {
    name: node.left.name as TimeFunction,
    timezone: node.left.args[0].value.trim() || 'UTC',
    operator: node.operator,
    value: node.right.value,
  }
}

function describeTimeRange(
  comparisons: TimeComparison[],
  t: Translate,
  locale: string
): Description | null {
  const first = comparisons[0]
  let [start, end] = DOMAINS[first.name]
  for (const comparison of comparisons) {
    switch (comparison.operator) {
      case '>=':
        start = Math.max(start, comparison.value)
        break
      case '>':
        start = Math.max(start, comparison.value + 1)
        break
      case '<':
        end = Math.min(end, comparison.value)
        break
      case '<=':
        end = Math.min(end, comparison.value + 1)
        break
      case '==':
        start = Math.max(start, comparison.value)
        end = Math.min(end, comparison.value + 1)
        break
    }
  }
  if (start >= end) return null
  let text: string
  let kind: Description['kind'] = 'calendar'
  if (first.name === 'hour') {
    kind = 'clock'
    text = t('{{start}} - {{end}}', {
      start: `${String(start).padStart(2, '0')}:00`,
      end: `${String(end).padStart(2, '0')}:00`,
    })
  } else if (first.name === 'weekday') {
    const formatter = new Intl.DateTimeFormat(toIntlLocale(locale), {
      weekday: 'short',
      timeZone: 'UTC',
    })
    // 2026-01-04 is Sunday, matching Go's weekday numbering.
    const from = formatter.format(new Date(Date.UTC(2026, 0, 4 + start)))
    const to = formatter.format(new Date(Date.UTC(2026, 0, 4 + end - 1)))
    text =
      start === end - 1
        ? from
        : t('{{start}} - {{end}}', { start: from, end: to })
  } else {
    const labels: Record<string, string> = {
      minute: 'Minute',
      month: 'Month',
      day: 'Day',
    }
    const values =
      start === end - 1
        ? String(start)
        : t('{{start}} - {{end}}', { start, end: end - 1 })
    text = `${t(labels[first.name])}: ${values}`
  }
  return { text, kind, timezone: first.timezone }
}

function describeBillingCondition(
  node: ExpressionNode,
  t: Translate,
  locale: string
): Description | null {
  if (node.kind === 'unary' && node.operator === '!') {
    const description = describeBillingCondition(node.operand, t, locale)
    if (!description) return null
    return {
      ...description,
      text: t('Outside these times: {{condition}}', {
        condition: description.text,
      }),
      kind: 'combined',
    }
  }
  if (
    node.kind === 'binary' &&
    ['<', '<=', '>', '>=', '==', '!='].includes(node.operator) &&
    node.left.kind === 'variable' &&
    node.right.kind === 'literal' &&
    typeof node.right.value === 'number'
  ) {
    const labels: Record<string, string> = {
      p: 'Input',
      c: 'Output',
      len: 'Full input length',
    }
    const label = labels[node.left.name]
    if (!label) return null
    return {
      text: `${t(label)} ${node.operator} ${node.right.value.toLocaleString(toIntlLocale(locale))}`,
      kind: 'combined',
      timezone: '',
    }
  }
  const single = timeComparison(node)
  if (single) return describeTimeRange([single], t, locale)
  if (node.kind !== 'binary' || !['&&', '||'].includes(node.operator)) {
    return null
  }
  const nodes = flattenBinary(node, node.operator)
  const parts: Description[] = []
  if (node.operator === '&&') {
    const ranges = new Map<string, TimeComparison[]>()
    for (const part of nodes) {
      const comparison = timeComparison(part)
      if (comparison) {
        const key = `${comparison.name}:${comparison.timezone}`
        const range = ranges.get(key) ?? []
        range.push(comparison)
        ranges.set(key, range)
      } else {
        const description = describeBillingCondition(part, t, locale)
        if (!description) return null
        parts.push(description)
      }
    }
    for (const range of ranges.values()) {
      const description = describeTimeRange(range, t, locale)
      if (!description) return null
      parts.push(description)
    }
  } else {
    for (const part of nodes) {
      const description = describeBillingCondition(part, t, locale)
      if (!description) return null
      parts.push(description)
    }
  }
  const zones = [...new Set(parts.map((part) => part.timezone).filter(Boolean))]
  if (parts.length === 0 || zones.length > 1) return null
  const timezone = zones[0] ?? ''
  if (parts.length === 1) return parts[0]
  const calendars = parts.filter((part) => part.kind === 'calendar')
  const clocks = parts.filter((part) => part.kind === 'clock')
  if (
    node.operator === '&&' &&
    parts.length === 2 &&
    calendars.length === 1 &&
    clocks.length === 1
  ) {
    return {
      text: `${calendars[0].text} ${clocks[0].text}`,
      kind: 'combined',
      timezone: parts[0].timezone,
    }
  }
  const separator =
    node.operator === '&&'
      ? '{{first}} and {{second}}'
      : '{{first}} or {{second}}'
  let text =
    node.operator === '&&' && parts[0].disjunction
      ? `(${parts[0].text})`
      : parts[0].text
  for (const part of parts.slice(1)) {
    const second =
      node.operator === '&&' && part.disjunction ? `(${part.text})` : part.text
    text = t(separator, { first: text, second })
  }
  return {
    text,
    kind: clocks.length === parts.length ? 'clock' : 'combined',
    timezone,
    disjunction: node.operator === '||',
  }
}

/** Presentation only. Unknown conditions retain their source; this never changes tier selection. */
export function formatBillingCondition(
  source: string,
  t: Translate,
  locale = 'en'
): string | null {
  const compiled = compileBillingExpression(source)
  if (compiled.status !== 'ready') return null
  try {
    // The returned value is rendered as React text, never as HTML.
    const translate: Translate = (key, options) =>
      t(key, { ...options, interpolation: { escapeValue: false } })
    const description = describeBillingCondition(
      compiled.ast,
      translate,
      locale
    )
    if (!description) return null
    if (!description.timezone) return description.text
    // The timezone is implied by the interface language, so hide the suffix
    // unless the expression targets a zone other than the language default.
    if (description.timezone === languageTimezone(locale)) {
      return description.text
    }
    return translate('{{condition}} ({{timezone}})', {
      condition: description.text,
      timezone: description.timezone,
    })
  } catch {
    return null
  }
}

/**
 * Format a condition as one line per disjunct so UIs can stack time windows
 * vertically ("22:00 - 24:00" / "00:00 - 06:00"). Falls back to the single
 * joined text when the condition is not a plain disjunction of time windows.
 */
export function formatBillingConditionLines(
  source: string,
  t: Translate,
  locale = 'en'
): string[] | null {
  const compiled = compileBillingExpression(source)
  if (compiled.status !== 'ready') return null
  try {
    const translate: Translate = (key, options) =>
      t(key, { ...options, interpolation: { escapeValue: false } })
    const node = compiled.ast
    // A negated pure clock window (the standard tier of a peak/off-peak
    // expression) reads better as the concrete complement: "06:00 - 22:00"
    // instead of "Outside these times: 22:00 - 24:00 or 00:00 - 06:00".
    if (node.kind === 'unary' && node.operator === '!') {
      const complement = formatIdleClockRanges(
        [compiled.source.slice(node.operand.start, node.operand.end)],
        t,
        locale
      )
      if (complement) return complement
    }
    const disjuncts =
      node.kind === 'binary' && node.operator === '||'
        ? flattenBinary(node, '||')
        : [node]
    if (disjuncts.length > 1) {
      const parts: Array<Description | null> = disjuncts.map((disjunct) =>
        describeBillingCondition(disjunct, translate, locale)
      )
      if (parts.every(Boolean)) {
        const zones = [
          ...new Set(
            parts.flatMap((part) =>
              part && part.timezone ? [part.timezone] : []
            )
          ),
        ]
        if (zones.length <= 1) {
          const zone = zones[0]
          if (!zone || zone === languageTimezone(locale)) {
            return parts.map((part) => part?.text ?? '')
          }
        }
      }
    }
    const joined = formatBillingCondition(source, t, locale)
    return joined ? [joined] : null
  } catch {
    return null
  }
}

type ClockWindow = { start: number; end: number }

/**
 * Fold one disjunct of hour() comparisons into a single [start, end) window.
 * Returns null when the disjunct contains anything other than hour bounds in
 * one timezone (weekday/month limits, len checks, mixed zones...).
 */
function clockWindowFromDisjunct(
  node: ExpressionNode
): { window: ClockWindow; timezone: string } | null {
  const conjuncts =
    node.kind === 'binary' && node.operator === '&&'
      ? flattenBinary(node, '&&')
      : [node]
  let start = 0
  let end = 24
  let timezone: string | null = null
  for (const conjunct of conjuncts) {
    const comparison = timeComparison(conjunct)
    if (!comparison || comparison.name !== 'hour') return null
    if (timezone && comparison.timezone !== timezone) return null
    timezone = comparison.timezone
    switch (comparison.operator) {
      case '>=':
        start = Math.max(start, comparison.value)
        break
      case '>':
        start = Math.max(start, comparison.value + 1)
        break
      case '<':
        end = Math.min(end, comparison.value)
        break
      case '<=':
        end = Math.min(end, comparison.value + 1)
        break
      case '==':
        start = Math.max(start, comparison.value)
        end = Math.min(end, comparison.value + 1)
        break
      default:
        return null
    }
  }
  if (start >= end || !timezone) return null
  return { window: { start, end }, timezone }
}

/**
 * Complement of pure hour-window conditions over the 24h day, e.g. peak
 * "22:00 - 24:00 or 00:00 - 06:00" leaves the idle range "06:00 - 22:00".
 * Returns null when any condition is not a pure clock window, so callers can
 * keep their generic fallback text.
 */
export function formatIdleClockRanges(
  sources: Array<string | null | undefined>,
  t: Translate,
  locale = 'en'
): string[] | null {
  const usable = sources.filter(Boolean) as string[]
  if (usable.length === 0) return null
  const windows: ClockWindow[] = []
  let timezone: string | null = null
  for (const source of usable) {
    const compiled = compileBillingExpression(source)
    if (compiled.status !== 'ready') return null
    const node = compiled.ast
    const disjuncts =
      node.kind === 'binary' && node.operator === '||'
        ? flattenBinary(node, '||')
        : [node]
    for (const disjunct of disjuncts) {
      const parsed = clockWindowFromDisjunct(disjunct)
      if (!parsed) return null
      if (timezone && parsed.timezone !== timezone) return null
      timezone = parsed.timezone
      windows.push(parsed.window)
    }
  }
  const merged: ClockWindow[] = []
  for (const window of [...windows].sort((a, b) => a.start - b.start)) {
    const last = merged.at(-1)
    if (last && window.start <= last.end) {
      last.end = Math.max(last.end, window.end)
    } else {
      merged.push({ ...window })
    }
  }
  const complement: ClockWindow[] = []
  let cursor = 0
  for (const window of merged) {
    if (window.start > cursor) {
      complement.push({ start: cursor, end: window.start })
    }
    cursor = Math.max(cursor, window.end)
  }
  if (cursor < 24) complement.push({ start: cursor, end: 24 })
  const translate: Translate = (key, options) =>
    t(key, { ...options, interpolation: { escapeValue: false } })
  return complement.map((range) => {
    const text = t('{{start}} - {{end}}', {
      start: `${String(range.start).padStart(2, '0')}:00`,
      end: `${String(range.end).padStart(2, '0')}:00`,
    })
    if (!timezone || timezone === languageTimezone(locale)) return text
    return translate('{{condition}} ({{timezone}})', {
      condition: text,
      timezone,
    })
  })
}
