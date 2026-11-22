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
import { cleanup, render, screen, within } from '@testing-library/react'
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
  scope_kind: 'token',
  scope_id: 24,
  token_ids: [24],
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
    await screen.findByRole('button', { name: 'Details #42' })
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
    expect(
      await screen.findByRole('button', { name: 'Details #43' })
    ).toBeVisible()
  })

  it('clears stale results after a failed refresh and offers a retry', async () => {
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
    await screen.findByRole('button', { name: 'Details #42' })
    await user.click(
      within(screen.getByRole('main')).getByRole('button', {
        name: 'Refresh',
      })
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'risk listing unavailable'
    )
    expect(
      screen.queryByRole('button', { name: 'Details #42' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText('Pending: 2')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
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

    expect(await screen.findByText('Risk event #42')).toBeVisible()
    expect(
      await screen.findByText('Detailed evidence has expired after 30 days.')
    ).toBeVisible()
    expect(screen.getByText('Reviewer ID').parentElement).toHaveTextContent('3')
    expect(screen.getByText('Reviewed at').parentElement).toHaveTextContent(
      '2025'
    )
  })

  it('offers copy actions for investigation identifiers', async () => {
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
    expect(screen.getByText('Risk event #42')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Copy Risk event ID' })
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Copy Scope ID' })).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Copy Contributing tokens' })
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Copy Reviewer ID' })
    ).toBeVisible()
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
    expect(await screen.findByText('Risk event #42')).toBeVisible()
    expect(getDistributionRiskEvent).toHaveBeenCalledTimes(2)
  })
})
