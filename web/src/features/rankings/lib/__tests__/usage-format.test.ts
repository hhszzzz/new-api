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
import { formatUSD } from '../format'
import { buildRankingPieSlices, formatRankingUserTooltip } from '../user-usage'

describe('rankings charged-amount presentation', () => {
  test('formats fixed USD independently from site currency settings', () => {
    assert.equal(formatUSD(0), '$0.00')
    assert.equal(formatUSD(1.25), '$1.25')
    assert.equal(formatUSD(0.000002), '$0.000002')
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

  test('adds user group usage to the existing pie tooltip', () => {
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
        },
      ],
    }

    const tooltip = formatRankingUserTooltip(
      buildRankingPieSlices(usage)[0],
      (key) => key
    )

    assert.match(tooltip, /Usage by group/)
    assert.match(tooltip, /team: \$1\.50 · 150 tokens · 75\.0%/)
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
