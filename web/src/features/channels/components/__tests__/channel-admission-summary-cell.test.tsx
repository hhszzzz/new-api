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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
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

async function renderCell(target: Channel = channel) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const channelsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'channels',
    component: () => <Outlet />,
  })
  const listRoute = createRoute({
    getParentRoute: () => channelsRoute,
    path: '/',
    component: () => <ChannelAdmissionSummaryCell channel={target} />,
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
      rpm: 12,
      rpm_users: [{ user_id: 1, username: 'hhs', count: 12 }],
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

  const summary = await screen.findByTestId('admission-summary')
  await waitFor(() => {
    expect(summary).toHaveTextContent('Concurrency')
    expect(summary).toHaveTextContent('2/2')
    expect(summary).toHaveTextContent('Queued 1')
    expect(summary).toHaveTextContent(/RPM\s*—/)
  })
  const slotGauge = summary.querySelector('[data-slot="admission-slot-gauge"]')
  expect(slotGauge).not.toBeNull()
  expect(slotGauge?.querySelectorAll('[data-occupied="true"]')).toHaveLength(2)
  expect(slotGauge?.querySelectorAll('[data-occupied="false"]')).toHaveLength(0)

  const user = userEvent.setup()
  await user.hover(screen.getByRole('button', { name: /Admission Monitor/ }))
  expect(await screen.findByText(/Wait timeout/)).toBeVisible()
  expect(screen.getByText('Active users')).toBeVisible()
  expect(screen.getAllByText('hhs').length).toBeGreaterThanOrEqual(1)
  expect(screen.queryByText('Queued requests')).not.toBeInTheDocument()
  expect(screen.queryByText(/Concurrency limit:/)).not.toBeInTheDocument()
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

it('shows RPM usage and user breakdown for RPM-only channels', async () => {
  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockResolvedValue({
    success: true,
    data: {
      channel_id: 12,
      window_start_ms: 0,
      window_end_ms: 0,
      bucket_seconds: 60,
      active: 0,
      active_users: 0,
      active_user_list: [],
      rpm: 14,
      rpm_users: [
        { user_id: 1, username: 'hhs', count: 12 },
        { user_id: 2, username: 'cbk', count: 2 },
      ],
      waiting: 0,
      oldest_wait_ms: 0,
      wait_p50_ms: 0,
      wait_p95_ms: 0,
      grants: 14,
      waited_grants: 0,
      soup_grants: 0,
      priority_grants: 0,
      queue_full: 0,
      timeouts: 0,
      waiting_users: [],
      waiting_requests: [],
      minute_series: [],
    },
  })
  const rpmOnly = {
    ...channel,
    concurrency_limit: 0,
    rpm_limit: 60,
    user_rate_limits: [{ user_id: 1, rpm_limit: 10 }],
  } as unknown as Channel

  const { queryClient } = await renderCell(rpmOnly)

  const summary = await screen.findByTestId('admission-summary')
  await waitFor(() => {
    expect(summary).toHaveTextContent('RPM')
    expect(summary).toHaveTextContent('Limited')
    expect(summary).toHaveTextContent('14/60')
    expect(summary).toHaveTextContent('hhs')
    expect(summary).toHaveTextContent('+1')
    expect(summary).toHaveTextContent('12/10')
  })
  expect(
    summary.querySelectorAll('[data-slot="admission-usage-bar"]')
  ).toHaveLength(2)
  queryClient.clear()
})

it('keeps the two-track structure when an RPM-only channel is idle', async () => {
  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockResolvedValue({
    success: true,
    data: {
      channel_id: 12,
      window_start_ms: 0,
      window_end_ms: 0,
      bucket_seconds: 60,
      active: 0,
      active_users: 0,
      active_user_list: [],
      rpm: 0,
      rpm_users: [],
      waiting: 0,
      oldest_wait_ms: 0,
      wait_p50_ms: 0,
      wait_p95_ms: 0,
      grants: 0,
      waited_grants: 0,
      soup_grants: 0,
      priority_grants: 0,
      queue_full: 0,
      timeouts: 0,
      waiting_users: [],
      waiting_requests: [],
      minute_series: [],
    },
  })
  const rpmOnly = {
    ...channel,
    concurrency_limit: 0,
    rpm_limit: 60,
    user_rate_limits: [],
  } as unknown as Channel

  const { queryClient } = await renderCell(rpmOnly)

  const summary = await screen.findByTestId('admission-summary')
  await waitFor(() => {
    expect(summary).toHaveTextContent(/RPM\s*0\/60/)
    expect(summary).toHaveTextContent(/No RPM usage this minute\s*0/)
  })
  expect(
    summary.querySelectorAll('[data-slot="admission-usage-bar"]')
  ).toHaveLength(2)
  queryClient.clear()
})

it('shows unknown values instead of idle zeros when statistics fail', async () => {
  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockRejectedValue(
    new Error('redis is unavailable')
  )

  const { queryClient } = await renderCell()

  const summary = await screen.findByTestId('admission-summary')
  await waitFor(() => {
    expect(summary).toHaveTextContent(/Concurrency\s*—\/2/)
  })
  expect(summary).not.toHaveTextContent(/Concurrency\s*0\/2/)
  queryClient.clear()
})

it('shows unknown RPM instead of zero when an RPM-only statistics request fails', async () => {
  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockRejectedValue(
    new Error('redis is unavailable')
  )
  const rpmOnly = {
    ...channel,
    concurrency_limit: 0,
    rpm_limit: 60,
    user_rate_limits: [],
  } as unknown as Channel

  const { queryClient } = await renderCell(rpmOnly)

  const summary = await screen.findByTestId('admission-summary')
  await waitFor(() => {
    expect(summary).toHaveTextContent(/RPM\s*—\/60/)
  })
  expect(summary).not.toHaveTextContent(/RPM\s*0\/60/)
  queryClient.clear()
})
