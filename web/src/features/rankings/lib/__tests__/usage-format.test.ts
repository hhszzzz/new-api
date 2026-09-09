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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import type { RankingUserUsage } from '../../types'
import { formatUSD, formatUsageColumns } from '../format'
import { buildRankingPieSlices, rankingBreakdownRows } from '../user-usage'

describe('rankings charged-amount presentation', () => {
  // Charged amounts sit beside a token count in every ranking row, so they are
  // rounded to a single decimal to keep the columns scannable. A tiny non-zero
  // amount must still read as "spent something" rather than as free usage.
  test('formats fixed USD to one decimal independently from site currency settings', () => {
    assert.equal(formatUSD(0), '$0.0')
    assert.equal(formatUSD(1.25), '$1.3')
    assert.equal(formatUSD(12), '$12.0')
    assert.equal(formatUSD(1234.567), '$1,234.6')
    assert.equal(formatUSD(0.000002), '<$0.1')
  })

  test('builds top five user slices plus one exact Other slice', () => {
    const users = Array.from({ length: 12 }, (_, index) => ({
      rank: index + 1,
      username: `user-${index + 1}`,
      total_tokens: 100 - index,
      total_quota: 12 - index,
      total_usd: 12 - index,
      quota_share: (12 - index) / 78,
      token_share: 0,
      groups: [],
      models: [],
    }))
    const usage: RankingUserUsage = {
      total_tokens: 0,
      total_quota: 78,
      total_usd: 78,
      users,
    }

    const slices = buildRankingPieSlices(usage)

    assert.equal(slices.length, 6)
    assert.equal(slices.at(-1)?.name, 'Other')
    assert.equal(slices.at(-1)?.quota, 28)
    assert.equal(
      slices.reduce((sum, slice) => sum + slice.share, 0),
      1
    )
  })

  test('lists per-group usage rows for the tooltip card', () => {
    const usage: RankingUserUsage = {
      total_tokens: 200,
      total_quota: 1_000_000,
      total_usd: 2,
      users: [
        {
          rank: 1,
          username: 'user-1',
          total_tokens: 200,
          total_quota: 1_000_000,
          total_usd: 2,
          quota_share: 1,
          token_share: 1,
          groups: [
            {
              use_group: 'team',
              total_tokens: 150,
              total_quota: 750_000,
              total_usd: 1.5,
              quota_share: 0.75,
              token_share: 0.75,
            },
          ],
          models: [
            {
              model_name: 'gpt-5',
              total_tokens: 120,
              total_quota: 600_000,
              total_usd: 1.2,
              quota_share: 0.6,
              token_share: 0.6,
            },
          ],
        },
      ],
    }

    const rows = rankingBreakdownRows(buildRankingPieSlices(usage)[0], 'group')

    assert.deepEqual(rows, [{ label: 'team', tokens: 150, usd: 1.5 }])
  })

  test('switches the tooltip card rows to per-model usage', () => {
    const usage: RankingUserUsage = {
      total_tokens: 200,
      total_quota: 1_000_000,
      total_usd: 2,
      users: [
        {
          rank: 1,
          username: 'user-1',
          total_tokens: 200,
          total_quota: 1_000_000,
          total_usd: 2,
          quota_share: 1,
          token_share: 1,
          groups: [
            {
              use_group: 'team',
              total_tokens: 150,
              total_quota: 750_000,
              total_usd: 1.5,
              quota_share: 0.75,
              token_share: 0.75,
            },
          ],
          models: [
            {
              model_name: 'gpt-5',
              total_tokens: 120,
              total_quota: 600_000,
              total_usd: 1.2,
              quota_share: 0.6,
              token_share: 0.6,
            },
          ],
        },
      ],
    }

    const rows = rankingBreakdownRows(buildRankingPieSlices(usage)[0], 'model')

    assert.deepEqual(rows, [{ label: 'gpt-5', tokens: 120, usd: 1.2 }])
  })

  // Ranking tooltips list several rows at once, and each value is formatted to
  // its own width (`4.70M` beside `670.5M`). Padding every row against the same
  // set is what keeps the charge column under the charge column.
  test('aligns usage rows into shared share, token, and charge columns', () => {
    const [team, fallback] = formatUsageColumns([
      { share: 0.99, tokens: 670_500_000, usd: 590.4 },
      { share: 0.01, tokens: 4_700_000, usd: 10.8 },
    ])

    // The narrower row is padded, not reformatted: same values, same total
    // width, and each column as wide as the one above it.
    assert.equal(team, '99.0% · 670.5M · $590.4')
    assert.equal(fallback.replaceAll(' ', ''), '1.0% · 4.70M · $10.8')
    assert.equal(fallback.length, team.length)
    assert.deepEqual(
      fallback.split(' · ').map((cell) => cell.length),
      team.split(' · ').map((cell) => cell.length)
    )
  })

  test('normalizes shares when the reported total is lower than user rows', () => {
    const usage: RankingUserUsage = {
      total_tokens: 0,
      total_quota: 5,
      total_usd: 5,
      users: [
        {
          rank: 1,
          username: 'user-1',
          total_tokens: 0,
          total_quota: 4,
          total_usd: 4,
          quota_share: 0.8,
          token_share: 0,
          groups: [],
          models: [],
        },
        {
          rank: 2,
          username: 'user-2',
          total_tokens: 0,
          total_quota: 3,
          total_usd: 3,
          quota_share: 0.6,
          token_share: 0,
          groups: [],
          models: [],
        },
      ],
    }

    const slices = buildRankingPieSlices(usage)

    assert.equal(
      slices.reduce((sum, slice) => sum + slice.share, 0),
      1
    )
    assert.equal(
      slices.some((slice) => slice.isOther),
      false
    )
  })
})
