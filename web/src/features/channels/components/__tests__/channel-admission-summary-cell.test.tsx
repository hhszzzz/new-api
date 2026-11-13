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
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it, vi } from 'vitest'

import * as channelApi from '../../api'
import type { Channel } from '../../types'
import { ChannelAdmissionSummaryCell } from '../channel-admission-summary-cell'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

const channel = {
  id: 12,
  name: 'apikey.casa',
  concurrency_limit: 2,
  rpm_limit: null,
  priority: 1,
  settings: '{}',
  user_rate_limits: [],
} as unknown as Channel

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

async function renderCell() {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const channelsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'channels',
    component: () => <Outlet />,
  })
  const listRoute = createRoute({
    getParentRoute: () => channelsRoute,
    path: '/',
    component: () => <ChannelAdmissionSummaryCell channel={channel} />,
  })
  const admissionRoute = createRoute({
    getParentRoute: () => channelsRoute,
    path: 'admission',
    validateSearch: (search: Record<string, unknown>) => ({
      channel_id: Number(search.channel_id),
    }),
    component: () => <p>Detailed monitor</p>,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      channelsRoute.addChildren([listRoute, admissionRoute]),
    ]),
    history: createMemoryHistory({ initialEntries: ['/channels'] }),
  })
  await router.load()

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return { router, queryClient }
}

it('shows a compact summary and opens the selected channel detail', async () => {
  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockResolvedValue({
    success: true,
    data: {
      channel_id: 12,
      window_start_ms: 0,
      window_end_ms: 0,
      bucket_seconds: 60,
      active: 2,
      active_users: 2,
      active_user_list: [
        { user_id: 1, username: 'hhs', priority: 1, count: 2 },
      ],
      waiting: 1,
      oldest_wait_ms: 800,
      wait_p50_ms: 500,
      wait_p95_ms: 1_000,
      grants: 20,
      waited_grants: 4,
      soup_grants: 4,
      priority_grants: 0,
      queue_full: 1,
      timeouts: 0,
      waiting_users: [
        {
          user_id: 5,
          username: 'cbk',
          priority: 2,
          position: 1,
          count: 1,
          oldest_wait_ms: 800,
        },
      ],
      waiting_requests: [
        {
          id: 'req-1',
          user_id: 5,
          username: 'cbk',
          priority: 2,
          position: 1,
          wait_ms: 800,
        },
      ],
      minute_series: [
        {
          timestamp_ms: 0,
          grants: 2,
          waited: 1,
          soup_grants: 0,
          priority_grants: 0,
          queue_full: 0,
          timeouts: 0,
          wait_p50_ms: 500,
          wait_p95_ms: 800,
        },
      ],
    },
  })

  const { router, queryClient } = await renderCell()

  expect(await screen.findByText('Concurrency 2/2 · Queued 1')).toBeVisible()
  expect(screen.getByText('Oldest wait: 0.8s')).toBeVisible()
  expect(screen.getAllByTestId('admission-spark-bar')).toHaveLength(1)
  const user = userEvent.setup()
  await user.hover(screen.getByRole('button', { name: /Admission Monitor/ }))
  expect(await screen.findByText('Queued requests')).toBeVisible()
  await user.click(screen.getByRole('button', { name: /Admission Monitor/ }))
  expect(await screen.findByText('Detailed monitor')).toBeVisible()
  expect(router.state.location.pathname).toBe('/channels/admission')
  expect(router.state.location.search).toEqual({ channel_id: 12 })
  expect(channelApi.getChannelAdmissionStats).toHaveBeenCalledWith(
    12,
    expect.objectContaining({
      start_timestamp: expect.any(Number),
      end_timestamp: expect.any(Number),
    })
  )
  queryClient.clear()
})

it('shows unknown values instead of idle zeros when statistics fail', async () => {
  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockRejectedValue(
    new Error('redis is unavailable')
  )

  const { queryClient } = await renderCell()

  expect(await screen.findByText('Concurrency —/2 · Queued —')).toBeVisible()
  expect(
    screen.queryByText('Concurrency 0/2 · Queued 0')
  ).not.toBeInTheDocument()
  queryClient.clear()
})
