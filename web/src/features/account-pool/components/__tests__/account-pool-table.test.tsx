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
import type { Table } from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { AccountPoolAccount, AccountPoolSnapshot } from '../../types'
import { AccountPoolTable } from '../account-pool-table'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      key.replace('{{time}}', String(values?.time ?? '{{time}}')),
  }),
}))

vi.mock('@/components/data-table', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/components/data-table')>()
  return {
    ...actual,
    DataTablePage: (props: {
      table: Table<AccountPoolAccount>
      toolbar?: ReactNode
      mobile?: ReactNode
      fixedHeight?: boolean
    }) => (
      <>
        <output data-testid='column-order'>
          {props.table
            .getVisibleLeafColumns()
            .map((column) => column.id)
            .join(',')}
        </output>
        <output data-testid='column-sizing'>
          {JSON.stringify(props.table.getState().columnSizing)}
        </output>
        <output data-testid='fixed-height'>{String(props.fixedHeight)}</output>
        {props.toolbar}
        {props.mobile}
      </>
    ),
  }
})

function account(email?: string): AccountPoolAccount {
  return {
    public_id: 'public-1',
    display_name: 'Codex #1',
    provider: 'codex',
    email,
    status: 'available',
    plan: 'plus',
    subscription_active_until: '2026-09-10T00:00:00Z',
    primary_window: {
      used_percent: 25,
      remaining_percent: 75,
      reset_at: '2026-08-29T13:00:00Z',
      limit_window_seconds: 18000,
    },
    secondary_window: {
      used_percent: 50,
      remaining_percent: 50,
      reset_at: '2026-09-05T12:00:00Z',
      limit_window_seconds: 604800,
    },
    window_groups: [],
    updated_at: '2026-08-29T11:55:00Z',
    stale: true,
  }
}

function snapshot(item: AccountPoolAccount): AccountPoolSnapshot {
  return {
    server_time: '2026-08-29T12:00:00Z',
    updated_at: '2026-08-29T12:00:00Z',
    next_refresh_at: '2026-08-29T12:04:05Z',
    manual_refresh_available_at: '2026-08-29T12:01:00Z',
    stale: false,
    partial: false,
    summary: { total: 1, available: 1, limited: 0, error: 0 },
    provider_summaries: {
      codex: { total: 1, available: 1, limited: 0, error: 0 },
    },
    accounts: [item],
  }
}

describe('account pool table', () => {
  beforeEach(() => window.localStorage.clear())

  test('exposes only read-only columns and one global refresh control', () => {
    const { container } = render(
      <AccountPoolTable
        snapshot={snapshot(account('admin@example.com'))}
        isLoading={false}
        isFetching={false}
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )

    expect(screen.getByTestId('column-order')).toHaveTextContent(
      'account,status,plan,primary-window,secondary-window,subscription_active_until'
    )
    expect(
      container.querySelector('time[datetime="2026-08-29T12:00:00Z"]')
    ).toBeInTheDocument()
    expect(
      container.querySelector('time[datetime="2026-08-29T11:55:00Z"]')
    ).not.toBeInTheDocument()
    expect(screen.getByText('Next refresh in 4m 5s')).toBeInTheDocument()
    expect(
      screen.queryByText('Last successful quota data')
    ).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Refresh all' })).toHaveLength(
      1
    )
    expect(screen.queryByText('Reset')).not.toBeInTheDocument()
    expect(screen.queryByText('Edit')).not.toBeInTheDocument()
    expect(screen.queryByText('Test')).not.toBeInTheDocument()
  })

  test('filters accounts and summary badges by the selected provider tab', async () => {
    const user = userEvent.setup()
    const codexAccount = account('codex@example.com')
    const claudeAccount: AccountPoolAccount = {
      ...account('claude@example.com'),
      public_id: 'claude-1',
      display_name: 'Claude #1',
      provider: 'claude',
    }
    const multiProviderSnapshot = snapshot(codexAccount)
    multiProviderSnapshot.accounts = [codexAccount, claudeAccount]
    multiProviderSnapshot.summary = {
      total: 2,
      available: 2,
      limited: 0,
      error: 0,
    }
    multiProviderSnapshot.provider_summaries = {
      codex: { total: 1, available: 1, limited: 0, error: 0 },
      claude: { total: 1, available: 1, limited: 0, error: 0 },
    }

    render(
      <AccountPoolTable
        snapshot={multiProviderSnapshot}
        isLoading={false}
        isFetching={false}
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )

    expect(screen.getByRole('tablist')).toHaveTextContent('Codex')
    expect(screen.getByText('Codex #1')).toBeInTheDocument()
    expect(screen.queryByText('Claude #1')).not.toBeInTheDocument()

    await user.click(screen.getByRole('tab', { name: /^Claude/ }))

    expect(screen.getByText('Claude #1')).toBeInTheDocument()
    expect(screen.queryByText('Codex #1')).not.toBeInTheDocument()
  })

  test('stacks antigravity window groups with their group labels', () => {
    const antigravityAccount: AccountPoolAccount = {
      ...account(),
      public_id: 'ag-1',
      display_name: 'Antigravity #1',
      provider: 'antigravity',
      plan: 'ultra',
      primary_window: null,
      secondary_window: null,
      window_groups: [
        {
          label: 'Gemini Models',
          primary_window: {
            used_percent: 20,
            remaining_percent: 80,
            reset_at: '2026-08-29T13:00:00Z',
            limit_window_seconds: 18000,
          },
          secondary_window: {
            used_percent: 40,
            remaining_percent: 60,
            reset_at: '2026-09-05T12:00:00Z',
            limit_window_seconds: 604800,
          },
        },
        {
          label: 'Claude and GPT models',
          primary_window: {
            used_percent: 10,
            remaining_percent: 90,
            reset_at: '2026-08-29T13:00:00Z',
            limit_window_seconds: 18000,
          },
          secondary_window: null,
        },
      ],
    }

    const antigravitySnapshot = snapshot(antigravityAccount)
    antigravitySnapshot.provider_summaries = {
      antigravity: { total: 1, available: 1, limited: 0, error: 0 },
    }

    render(
      <AccountPoolTable
        snapshot={antigravitySnapshot}
        isLoading={false}
        isFetching={false}
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )

    expect(screen.getByText('Gemini models')).toBeInTheDocument()
    expect(screen.getByText('Claude and GPT models')).toBeInTheDocument()
    expect(screen.getAllByRole('progressbar')).toHaveLength(3)
  })

  test('renders an administrator email only when the server includes it', () => {
    const { rerender } = render(
      <AccountPoolTable
        snapshot={snapshot(account('admin@example.com'))}
        isLoading={false}
        isFetching={false}
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )
    expect(screen.getByText('admin@example.com')).toBeInTheDocument()

    rerender(
      <AccountPoolTable
        snapshot={snapshot(account())}
        isLoading={false}
        isFetching={false}
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )
    expect(screen.queryByText('admin@example.com')).not.toBeInTheDocument()
    expect(screen.getByText('Codex #1')).toBeInTheDocument()
  })

  test('does not restore the obsolete seven-column sizing cache', () => {
    window.localStorage.setItem(
      'account-pool-table:column-sizing',
      JSON.stringify({
        account: 190,
        status: 100,
        plan: 90,
        'primary-window': 220,
        'secondary-window': 220,
        subscription_active_until: 150,
        updated_at: 150,
      })
    )

    render(
      <AccountPoolTable
        snapshot={snapshot(account())}
        isLoading={false}
        isFetching={false}
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )

    expect(screen.getByTestId('column-sizing')).toHaveTextContent('{}')
    expect(screen.getByTestId('fixed-height')).toHaveTextContent('false')
  })

  test('renders a weekly-only Pro account without a 5-hour quota section', () => {
    const weeklyOnly = account()
    weeklyOnly.plan = 'Pro 5x'
    weeklyOnly.primary_window = null

    render(
      <AccountPoolTable
        snapshot={snapshot(weeklyOnly)}
        isLoading={false}
        isFetching={false}
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )

    expect(screen.getByText('Pro 5x')).toBeInTheDocument()
    expect(screen.queryByText('5-hour quota')).not.toBeInTheDocument()
    expect(screen.getByText('Weekly quota')).toBeInTheDocument()
  })

  test('renders loading skeletons and the empty-pool state', () => {
    const { container, rerender } = render(
      <AccountPoolTable
        isLoading
        isFetching
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )
    expect(
      container.querySelectorAll('[data-slot="skeleton"]').length
    ).toBeGreaterThan(0)

    const emptySnapshot = snapshot(account())
    emptySnapshot.summary = { total: 0, available: 0, limited: 0, error: 0 }
    emptySnapshot.provider_summaries = {}
    emptySnapshot.accounts = []
    rerender(
      <AccountPoolTable
        snapshot={emptySnapshot}
        isLoading={false}
        isFetching={false}
        isRefreshing={false}
        refreshDisabled={false}
        refreshLabel='Refresh all'
        now={Date.parse('2026-08-29T12:00:00Z')}
        onRefresh={vi.fn()}
      />
    )
    expect(screen.getByText('No accounts in the pool')).toBeInTheDocument()
    expect(
      screen.getByText('Accounts will appear after the first successful sync.')
    ).toBeInTheDocument()
  })
})
