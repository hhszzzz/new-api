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
// ----------------------------------------------------------------------------
// Rankings formatting helpers
// ----------------------------------------------------------------------------

/** Format a token count as `1.2B`, `42M`, `980K`, or `512`. */
export function formatTokens(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  if (value >= 1_000_000_000_000) {
    return `${(value / 1_000_000_000_000).toFixed(2)}T`
  }
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(value >= 10_000_000_000 ? 1 : 2)}B`
  }
  if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(value >= 10_000_000 ? 1 : 2)}M`
  }
  if (value >= 1_000) {
    return `${(value / 1_000).toFixed(value >= 10_000 ? 0 : 1)}K`
  }
  return value.toLocaleString()
}

/**
 * Format a rankings amount as USD, independent of the site's token currency.
 *
 * Charged amounts are shown next to a token count, so a single decimal keeps the
 * columns readable; the exact sub-cent value carries no meaning at a glance and
 * a long fraction only makes the ranking harder to scan. A tiny non-zero amount
 * is reported as a threshold rather than rounded to `$0.0`, which would read as
 * free usage.
 */
export function formatUSD(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '$0.0'
  if (value < 0.05) return '<$0.1'
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(value)
}

/**
 * Padding for tooltip columns.
 *
 * A regular space collapses in the DOM tooltips VChart renders, so columns are
 * padded with FIGURE SPACE (U+2007), which survives collapsing and is exactly
 * one digit wide in the numeric fonts these values use.
 */
const COLUMN_PAD = ' '

/** Right-align `text` within `width` columns. */
function padColumn(text: string, width: number): string {
  return text.length >= width
    ? text
    : `${COLUMN_PAD.repeat(width - text.length)}${text}`
}

export type UsageAmount = {
  tokens: number
  usd: number
  /** Optional leading share column, used by the market share tooltip. */
  share?: number
}

/**
 * Render a set of tooltip rows as right-aligned `tokens · usd` columns, with an
 * optional leading share column when every row carries one.
 *
 * Token counts and charged amounts vary in width (`4.70M` next to `670.5M`), so
 * formatting each row on its own leaves the `$` wandering between lines. Widths
 * are measured across the whole set instead, which keeps the columns aligned no
 * matter which rows a tooltip happens to show.
 */
export function formatUsageColumns(amounts: UsageAmount[]): string[] {
  const columns: string[][] = []
  if (amounts.every((amount) => amount.share !== undefined)) {
    columns.push(amounts.map((amount) => formatShare(amount.share ?? 0)))
  }
  columns.push(amounts.map((amount) => formatTokens(amount.tokens)))
  columns.push(amounts.map((amount) => formatUSD(amount.usd)))

  const widths = columns.map((column) =>
    Math.max(0, ...column.map((text) => text.length))
  )

  return amounts.map((_, row) =>
    columns
      .map((column, index) => padColumn(column[row], widths[index]))
      .join(' · ')
  )
}

/** Format a 0..1 share as a percentage with two decimals. */
export function formatShare(share: number): string {
  if (!Number.isFinite(share) || share <= 0) return '0%'
  if (share < 0.001) return '<0.1%'
  return `${(share * 100).toFixed(share < 0.01 ? 2 : 1)}%`
}

/** Format a release date like `Oct 12, 2025`. */
export function formatReleaseDate(iso: string): string {
  const ts = Date.parse(iso)
  if (!Number.isFinite(ts)) return iso
  return new Date(ts).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}
