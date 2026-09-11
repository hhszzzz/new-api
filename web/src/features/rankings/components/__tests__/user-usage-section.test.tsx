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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import type { RankingUserUsage } from '../../types'
import { UserUsageSection, UserUsageTooltipCard } from '../user-usage-section'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string | number>) => {
      let translated = key
      if (key === 'Other users') translated = '其他用户'
      if (key === 'Unknown') translated = '未知'
      return translated.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      )
    },
  }),
}))

vi.mock('@/lib/use-chart-theme', () => ({
  useChartTheme: () => ({ resolvedTheme: 'light', themeReady: true }),
}))

vi.mock('@/lib/vchart', () => ({ VCHART_OPTION: {} }))
vi.mock('@visactor/react-vchart', () => ({
  VChart: (props: {
    spec: {
      data?: Array<{
        values?: Array<{
          sliceKey: string
          username: string
          rank?: number
        }>
      }>
    }
    onClick?: (event: { datum?: Record<string, unknown> }) => void
  }) => (
    <div data-testid='user-chart'>
      {props.spec.data?.[0]?.values?.map((datum) => (
        <button
          key={datum.sliceKey}
          type='button'
          aria-label={`Chart ${datum.username}`}
          onClick={() => props.onClick?.({ datum })}
        />
      ))}
    </div>
  ),
}))
vi.mock('@tanstack/react-router', () => ({
  Link: (props: Record<string, unknown>) => <a {...props} />,
}))

const usage: RankingUserUsage = {
  total_tokens: 300,
  total_quota: 1_500_000,
  total_usd: 3,
  users: [
    {
      rank: 1,
      username: 'a***e',
      total_tokens: 200,
      total_quota: 1_000_000,
      total_usd: 2,
      quota_share: 2 / 3,
      token_share: 2 / 3,
      groups: [
        {
          use_group: 'team',
          total_tokens: 150,
          total_quota: 750000,
          total_usd: 1.5,
          quota_share: 0.75,
          token_share: 0.75,
        },
      ],
      models: [
        {
          model_name: 'gpt-5',
          total_tokens: 120,
          total_quota: 600000,
          total_usd: 1.2,
          quota_share: 0.6,
          token_share: 0.6,
        },
      ],
    },
    {
      rank: 2,
      username: 'b***b',
      total_tokens: 100,
      total_quota: 500000,
      total_usd: 1,
      quota_share: 1 / 3,
      token_share: 1 / 3,
      groups: [
        {
          use_group: 'default',
          total_tokens: 100,
          total_quota: 500000,
          total_usd: 1,
          quota_share: 1,
          token_share: 1,
        },
      ],
    },
  ],
}

describe('rankings user usage section', () => {
  test('shows a sign-in entry for anonymous viewers', () => {
    render(<UserUsageSection isAuthenticated={false} />)

    expect(screen.getByRole('button', { name: 'Sign in' })).toBeVisible()
    expect(screen.getByText('Sign in to view usage by user')).toBeVisible()
  })

  test('tooltip card puts the breakdown toggle in the header and switches rows by click', async () => {
    const user = userEvent.setup()
    render(
      <UserUsageTooltipCard
        datum={{
          sliceKey: 'user-1',
          username: 'a***e',
          rank: 1,
          quota: 1_000_000,
          usd: 2,
          share: 2 / 3,
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
        }}
      />
    )

    // Compact header: the username appears once and the share/amount
    // summary is dropped in favour of the inline breakdown toggle.
    expect(screen.getAllByText('a***e')).toHaveLength(1)
    expect(screen.queryByText(/66\.7% · \$2\.0/)).toBeNull()

    // Group breakdown is the default; clicking the toggle swaps in models.
    expect(screen.getByText('team')).toBeVisible()
    expect(screen.queryByText('gpt-5')).toBeNull()
    expect(screen.getByRole('button', { name: 'By group' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )

    await user.click(screen.getByRole('button', { name: 'By model' }))

    expect(screen.getByText('gpt-5')).toBeVisible()
    expect(screen.queryByText('team')).toBeNull()
    expect(screen.getByRole('button', { name: 'By model' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
  })

  test('tooltip card hides the breakdown toggle when the slice has no rows', () => {
    render(
      <UserUsageTooltipCard
        datum={{
          sliceKey: 'other',
          username: 'Other',
          quota: 28,
          usd: 0.1,
          share: 0.2,
          groups: [],
          models: [],
        }}
      />
    )

    expect(screen.getByText('Other')).toBeVisible()
    expect(screen.queryByRole('button', { name: 'By group' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'By model' })).toBeNull()
  })

  test('renders ranked users without selection controls', () => {
    render(<UserUsageSection isAuthenticated usage={usage} />)

    // Rows open a breakdown popover now; there is no user selection control.
    expect(screen.queryByRole('button', { name: /Select / })).toBeNull()
    expect(screen.getAllByText('a***e').length).toBeGreaterThan(0)
    expect(screen.getAllByText('b***b').length).toBeGreaterThan(0)
    expect(screen.getByText('Users ranked by charged amount')).toBeVisible()
  })

  test('opens a per-user breakdown popover when a ranked row is clicked', async () => {
    const user = userEvent.setup()
    render(<UserUsageSection isAuthenticated usage={usage} />)

    await user.click(screen.getByRole('button', { name: /^1\.\s*a\*\*\*e/ }))

    expect(
      await screen.findByRole('group', { name: 'Usage breakdown' })
    ).toBeVisible()
    // The ranked-row popover keeps the share/amount summary in its header.
    expect(screen.getByText(/66\.7% · \$2\.0/)).toBeVisible()
    expect(screen.getByText('team')).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'By model' }))

    expect(screen.getByText('gpt-5')).toBeVisible()
    expect(screen.queryByText('team')).toBeNull()
  })

  test('paginates the user ranking in ten-row pages', async () => {
    const user = userEvent.setup()
    const pagedUsage: RankingUserUsage = {
      total_tokens: 120,
      total_quota: 120,
      total_usd: 12,
      users: Array.from({ length: 12 }, (_, index) => ({
        rank: index + 1,
        username: `user-${index + 1}`,
        total_tokens: 10,
        total_quota: 10,
        total_usd: 1,
        quota_share: 1 / 12,
        token_share: 1 / 12,
        groups: [],
        models: [],
      })),
    }

    render(<UserUsageSection isAuthenticated usage={pagedUsage} />)

    // A username also appears in the chart legend, so the assertions look at the
    // ranked rows the pager controls rather than at the whole section.
    const isRanked = (username: string) =>
      screen
        .getAllByRole('listitem')
        .some((row) =>
          new RegExp(`\\b${username}\\b`).test(row.textContent ?? '')
        )

    expect(screen.getByText('Page 1 of 2')).toBeVisible()
    expect(isRanked('user-1')).toBe(true)
    expect(isRanked('user-11')).toBe(false)

    await user.click(screen.getByRole('button', { name: 'Next page' }))

    expect(screen.getByText('Page 2 of 2')).toBeVisible()
    expect(isRanked('user-11')).toBe(true)
    expect(isRanked('user-1')).toBe(false)
  })

  test('shows an authenticated empty state without rendering a chart', () => {
    render(<UserUsageSection isAuthenticated />)

    expect(screen.getByText('No user usage data available')).toBeVisible()
    expect(screen.queryByTestId('user-chart')).toBeNull()
  })

  test('localizes server-provided privacy labels', () => {
    const privateUsage: RankingUserUsage = {
      total_tokens: 10,
      total_quota: 500000,
      total_usd: 1,
      users: [
        {
          rank: 1,
          username: 'Other users',
          total_tokens: 10,
          total_quota: 500000,
          total_usd: 1,
          quota_share: 1,
          token_share: 1,
          groups: [
            {
              use_group: 'Unknown',
              total_tokens: 10,
              total_quota: 500000,
              total_usd: 1,
              quota_share: 1,
              token_share: 1,
            },
          ],
        },
      ],
    }

    render(<UserUsageSection isAuthenticated usage={privateUsage} />)

    expect(screen.getAllByText('其他用户').length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: 'Chart 其他用户' })).toBeVisible()
    expect(screen.queryByText('未知')).toBeNull()
  })
})
