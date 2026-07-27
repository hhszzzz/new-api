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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import type { RankingUserUsage } from '../../types'
import { UserUsageSection } from '../user-usage-section'

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

  test('selects users with rows and arrow keys without a group table', async () => {
    const user = userEvent.setup()
    render(<UserUsageSection isAuthenticated usage={usage} />)

    const first = screen.getByRole('button', { name: 'Select a***e' })
    const second = screen.getByRole('button', { name: 'Select b***b' })
    expect(first).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText('Usage by group · a***e')).toBeNull()

    await user.click(second)
    expect(second).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText('Usage by group · b***b')).toBeNull()

    await user.click(first)
    await user.keyboard('{ArrowDown}')
    await waitFor(() => expect(second).toHaveFocus())
    expect(second).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: 'Chart a***e' }))
    expect(first).toHaveAttribute('aria-pressed', 'true')
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

  test('localizes server-provided privacy labels without changing selection', async () => {
    const user = userEvent.setup()
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

    const row = screen.getByRole('button', { name: 'Select 其他用户' })
    expect(row).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Chart 其他用户' })).toBeVisible()
    expect(screen.queryByText('未知')).toBeNull()

    await user.click(row)
    expect(row).toHaveAttribute('aria-pressed', 'true')
  })
})
