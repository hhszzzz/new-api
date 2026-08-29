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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { AccountPool } from '..'
import type { AccountPoolSnapshot } from '../types'

const apiMocks = vi.hoisted(() => ({
  getAccountPool: vi.fn(),
  refreshAccountPool: vi.fn(),
  getAccountPoolApiError: vi.fn(),
}))

vi.mock('../api', () => apiMocks)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ auth: { user: { role: 1 } } }),
}))

vi.mock('@/components/layout', () => {
  const SectionPageLayout = Object.assign(
    ({ children }: { children: ReactNode }) => <div>{children}</div>,
    {
      Title: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
      Content: ({ children }: { children: ReactNode }) => <div>{children}</div>,
    }
  )
  return { SectionPageLayout }
})

vi.mock('../components/account-pool-table', () => ({
  AccountPoolTable: (props: {
    isLoading: boolean
    snapshot?: AccountPoolSnapshot
  }) => (
    <div>
      {props.isLoading
        ? 'account pool loading'
        : `${props.snapshot?.accounts.length ?? 0} accounts rendered`}
    </div>
  ),
}))

function snapshot(overrides: Partial<AccountPoolSnapshot> = {}) {
  return {
    updated_at: '2026-08-29T12:00:00Z',
    next_refresh_at: '2026-08-29T12:05:00Z',
    manual_refresh_available_at: '2026-08-29T12:00:00Z',
    stale: false,
    partial: false,
    summary: { total: 0, available: 0, error: 0 },
    accounts: [],
    ...overrides,
  } satisfies AccountPoolSnapshot
}

function renderAccountPool() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <AccountPool />
    </QueryClientProvider>
  )
}

describe('account pool page states', () => {
  beforeEach(() => {
    apiMocks.getAccountPool.mockReset()
    apiMocks.refreshAccountPool.mockReset()
    apiMocks.getAccountPoolApiError.mockReset()
    apiMocks.getAccountPoolApiError.mockImplementation((error) => ({
      code: error?.code ?? '',
      message: error?.message ?? '',
    }))
  })

  test('keeps the loading state visible until the first snapshot arrives', () => {
    apiMocks.getAccountPool.mockReturnValue(new Promise(() => {}))
    renderAccountPool()
    expect(screen.getByText('account pool loading')).toBeInTheDocument()
  })

  test('prioritizes the partial-refresh warning over whole-snapshot staleness', async () => {
    apiMocks.getAccountPool.mockResolvedValue({
      success: true,
      message: '',
      data: snapshot({ partial: true, stale: true }),
    })
    renderAccountPool()

    expect(
      await screen.findByText('Some accounts could not be refreshed')
    ).toBeInTheDocument()
    expect(
      screen.queryByText('Showing stale quota data')
    ).not.toBeInTheDocument()
  })

  test('renders the stale snapshot warning after a failed full refresh', async () => {
    apiMocks.getAccountPool.mockResolvedValue({
      success: true,
      message: '',
      data: snapshot({ stale: true }),
    })
    renderAccountPool()

    expect(
      await screen.findByText('Showing stale quota data')
    ).toBeInTheDocument()
  })

  test.each([
    ['account_pool_not_configured', 'Account pool is not configured'],
    ['account_pool_unavailable', 'Account pool is temporarily unavailable'],
  ])('renders the fatal %s state', async (code, title) => {
    apiMocks.getAccountPool.mockRejectedValue({ code })
    renderAccountPool()

    expect(await screen.findByText(title)).toBeInTheDocument()
  })
})
