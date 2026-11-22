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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { DistributionRiskPage } from '..'
import { getDistributionRiskEvent, listDistributionRiskEvents } from '../api'
import { DistributionRiskEventDialog } from '../components/distribution-risk-event-dialog'
import type { DistributionRiskEvent } from '../types'

vi.mock('../api', () => ({
  getDistributionRiskEvent: vi.fn(),
  listDistributionRiskEvents: vi.fn(),
  reviewDistributionRiskEvent: vi.fn(),
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  useAuthStore.getState().auth.reset()
  localStorage.clear()
})

const event = (id: number): DistributionRiskEvent => ({
  id,
  user_id: 17,
  username: 'alice',
  tokens: [{ id: 24, name: 'production' }],
  score: 88,
  level: 'high',
  category_scores: { network: 41, session: 47 },
  status: 'confirmed',
  created_at: 1_760_000_000,
  reviewed_at: 1_760_000_100,
  reviewer_id: 3,
  review_reason: '=HYPERLINK("https://example.invalid")',
  evidence: {
    ips: ['198.51.100.25'],
    user_agents: ['synthetic-browser'],
    session_hashes: ['session-hash-value'],
  },
})

describe('risk log listing', () => {
  function renderListing() {
    useAuthStore.getState().auth.setUser({
      id: 7,
      username: 'auditor',
      role: ROLE.ADMIN,
      permissions: {
        admin_permissions: { distribution_risk: { read: true } },
      },
    })
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <DistributionRiskPage />
      </QueryClientProvider>
    )
    return queryClient
  }

  it('searching with unchanged filters refreshes the results', async () => {
    vi.mocked(listDistributionRiskEvents).mockResolvedValue({
      items: [event(42)],
      total: 1,
      pending_count: 0,
      page: 1,
      page_size: 20,
    })
    const user = userEvent.setup()
    renderListing()
    await screen.findByRole('button', { name: 'Details' })
    const main = within(screen.getByRole('main'))
    expect(main.getByRole('columnheader', { name: 'User' })).toBeVisible()
    expect(main.getByRole('columnheader', { name: 'Token' })).toBeVisible()
    expect(main.getByRole('columnheader', { name: 'Risk' })).toBeVisible()
    expect(main.getByText('Confirmed')).toBeVisible()
    expect(main.queryByRole('columnheader', { name: 'Status' })).toBeNull()
    expect(
      main.queryByRole('columnheader', { name: 'Risk event ID' })
    ).toBeNull()
    expect(main.queryByRole('columnheader', { name: 'Scope' })).toBeNull()
    expect(
      main.queryByRole('columnheader', { name: 'Review reason' })
    ).toBeNull()
    expect(main.getByText('alice')).toBeVisible()
    expect(main.getByText('production')).toBeVisible()
    expect(main.queryByText('#17')).not.toBeInTheDocument()
    expect(
      main.getByRole('button', { name: /\d{4}-\d{2}-\d{2} 00:00:00 ~/ })
    ).toBeVisible()
    expect(listDistributionRiskEvents).toHaveBeenCalledWith(
      expect.objectContaining({
        start_timestamp: expect.any(Number),
        end_timestamp: expect.any(Number),
      }),
      1,
      20,
      expect.any(AbortSignal)
    )
    expect(main.queryByText('SYNTHETIC DEMO DATA')).not.toBeInTheDocument()
    vi.mocked(listDistributionRiskEvents).mockResolvedValue({
      items: [event(43)],
      total: 1,
      pending_count: 0,
      page: 1,
      page_size: 20,
    })
    await user.click(
      within(screen.getByRole('main')).getByRole('button', {
        name: 'Search',
      })
    )
    expect(await screen.findByRole('button', { name: 'Details' })).toBeVisible()
  })

  it('keeps the last result and pending count after a failed refresh', async () => {
    vi.mocked(listDistributionRiskEvents)
      .mockResolvedValueOnce({
        items: [event(42)],
        total: 1,
        pending_count: 2,
        page: 1,
        page_size: 20,
      })
      .mockRejectedValue(new Error('risk listing unavailable'))
    const user = userEvent.setup()
    renderListing()
    await screen.findByRole('button', { name: 'Details' })
    await user.click(
      within(screen.getByRole('main')).getByRole('button', {
        name: 'Refresh',
      })
    )
    await waitFor(() =>
      expect(listDistributionRiskEvents).toHaveBeenCalledTimes(2)
    )
    expect(screen.getByRole('button', { name: 'Details' })).toBeVisible()
    expect(screen.getByText('Pending: 2')).toBeVisible()
    expect(
      screen.queryByText(/distribution risk events are waiting for review/)
    ).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(
      screen.queryByText('risk listing unavailable')
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Retry' })
    ).not.toBeInTheDocument()
  })
})

describe('risk event details', () => {
  it('shows review attribution and handles an event whose detailed evidence expired', async () => {
    vi.mocked(getDistributionRiskEvent).mockResolvedValueOnce({
      ...event(42),
      category_scores: null,
      evidence: { ips: null, user_agents: null, session_hashes: null },
    })
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <DistributionRiskEventDialog
          eventID={42}
          canManage={false}
          onOpenChange={vi.fn()}
          onReviewed={vi.fn()}
        />
      </QueryClientProvider>
    )

    expect(
      await screen.findByRole('heading', { name: 'Risk event' })
    ).toBeVisible()
    expect(
      await screen.findByText('Detailed evidence has expired after 30 days.')
    ).toBeVisible()
    expect(screen.getByText('Reviewer ID').parentElement).toHaveTextContent('3')
    expect(screen.getByText('Reviewed at').parentElement).toHaveTextContent(
      '2025'
    )
  })

  it('keeps the event identifier hidden and offers copy actions for useful IDs', async () => {
    vi.mocked(getDistributionRiskEvent).mockResolvedValueOnce(event(42))
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <DistributionRiskEventDialog
          eventID={42}
          canManage={false}
          onOpenChange={vi.fn()}
          onReviewed={vi.fn()}
        />
      </QueryClientProvider>
    )

    expect(
      await screen.findByRole('button', { name: 'Copy User ID' })
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Copy Token' })).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Copy Reviewer ID' })
    ).toBeVisible()
    expect(screen.getByText('Username').parentElement).toHaveTextContent(
      'alice'
    )
    expect(screen.getByText('User ID').parentElement).toHaveTextContent('17')
    expect(screen.getByText('production (#24)')).toBeVisible()
    expect(screen.queryByText('Risk event #42')).not.toBeInTheDocument()
  })

  it('lets an administrator retry when event details fail to load', async () => {
    vi.mocked(getDistributionRiskEvent)
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce(event(42))
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const user = userEvent.setup()

    render(
      <QueryClientProvider client={queryClient}>
        <DistributionRiskEventDialog
          eventID={42}
          canManage={false}
          onOpenChange={vi.fn()}
          onReviewed={vi.fn()}
        />
      </QueryClientProvider>
    )

    await user.click(await screen.findByRole('button', { name: 'Retry' }))
    expect(
      await screen.findByRole('heading', { name: 'Risk event' })
    ).toBeVisible()
    expect(getDistributionRiskEvent).toHaveBeenCalledTimes(2)
  })
})
