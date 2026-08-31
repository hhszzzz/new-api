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

const authMocks = vi.hoisted(() => ({
  setUser: vi.fn(),
  user: {
    id: 1,
    username: 'allowed-user',
    role: 1,
    permissions: { account_pool: true },
  },
}))

vi.mock('../api', () => apiMocks)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ auth: { user: authMocks.user, setUser: authMocks.setUser } }),
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
    server_time: '2026-08-29T12:00:00Z',
    updated_at: '2026-08-29T12:00:00Z',
    next_refresh_at: '2026-08-29T12:05:00Z',
    manual_refresh_available_at: '2026-08-29T12:00:00Z',
    stale: false,
    partial: false,
    summary: { total: 0, available: 0, limited: 0, error: 0 },
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
    authMocks.setUser.mockReset()
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
      await screen.findByText('Some account quotas could not be refreshed')
    ).toBeInTheDocument()
    expect(
      screen.queryByText('Quota data could not be refreshed')
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
      await screen.findByText('Quota data could not be refreshed')
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

  test('turns an authoritative forbidden response into an access-denied state', async () => {
    apiMocks.getAccountPool.mockRejectedValue({
      code: 'account_pool_forbidden',
    })
    renderAccountPool()

    expect(await screen.findByText('Access Forbidden')).toBeInTheDocument()
    expect(authMocks.setUser).toHaveBeenCalledWith({
      ...authMocks.user,
      permissions: { account_pool: false },
    })
    expect(
      screen.queryByText('Account pool is temporarily unavailable')
    ).not.toBeInTheDocument()
  })
})
