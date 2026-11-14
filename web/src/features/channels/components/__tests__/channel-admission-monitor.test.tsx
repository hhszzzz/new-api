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
import { createInstance } from 'i18next'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it, vi } from 'vitest'

import * as channelApi from '../../api'
import type { Channel } from '../../types'
import { ChannelAdmissionMonitor } from '../channel-admission-monitor'

vi.mock('../channel-admission-trend-chart', () => ({
  ChannelAdmissionTrendChart: (props: {
    title: string
    summary?: ReactNode
  }) => (
    <div data-testid='trend-chart' data-title={props.title}>
      <span>{props.title}</span>
      {props.summary}
    </div>
  ),
}))

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

async function renderMonitor(
  channels: Channel[],
  initialChannelId: number,
  detailedChannel?: Channel
) {
  vi.spyOn(channelApi, 'getAllChannels').mockResolvedValue(channels)
  const fallbackChannel =
    channels.find((item) => item.id === initialChannelId) ?? channels[0]
  vi.spyOn(channelApi, 'getChannel').mockResolvedValue({
    success: true,
    message: '',
    data: detailedChannel ?? fallbackChannel,
  })

  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const channelsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'channels',
    component: () => <Outlet />,
  })
  const listRoute = createRoute({
    getParentRoute: () => channelsRoute,
    path: '/',
    component: () => <p>Channels list</p>,
  })
  const admissionRoute = createRoute({
    getParentRoute: () => channelsRoute,
    path: 'admission',
    component: () => (
      <ChannelAdmissionMonitor initialChannelId={initialChannelId} />
    ),
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      channelsRoute.addChildren([listRoute, admissionRoute]),
    ]),
    history: createMemoryHistory({
      initialEntries: [`/channels/admission?channel_id=${initialChannelId}`],
    }),
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
  return { queryClient }
}

it('shows concurrency hero, per-request active user pills, queue, and RPM users for queued channels', async () => {
  const queuedChannel = {
    id: 1,
    name: 'ctx-e2e-channel',
    concurrency_limit: 2,
    rpm_limit: 5,
    priority: 0,
    settings: '{}',
    user_rate_limits: [{ user_id: 1, username: 'root', rpm_limit: 10 }],
  } as unknown as Channel

  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockResolvedValue({
    success: true,
    data: {
      channel_id: 1,
      window_start_ms: 0,
      window_end_ms: 0,
      bucket_seconds: 60,
      active: 2,
      active_users: 2,
      active_user_list: [
        { user_id: 1, username: 'root', priority: 0, count: 2 },
      ],
      rpm: 3,
      rpm_users: [{ user_id: 1, username: 'root', count: 3 }],
      waiting: 1,
      oldest_wait_ms: 12_500,
      wait_p50_ms: 0,
      wait_p95_ms: 0,
      grants: 10,
      waited_grants: 0,
      soup_grants: 0,
      priority_grants: 0,
      queue_full: 0,
      timeouts: 0,
      waiting_users: [],
      waiting_requests: [
        {
          id: 'req-1',
          user_id: 2,
          username: 'ct225521',
          priority: 2,
          position: 1,
          wait_ms: 12_500,
        },
      ],
      minute_series: [],
    },
  })

  const { queryClient } = await renderMonitor([queuedChannel], 1)

  expect(await screen.findByText('Queued requests')).toBeVisible()
  expect(screen.getByText('RPM users')).toBeVisible()
  expect(screen.getByText('Selected range')).toBeVisible()
  expect(screen.getByText('Admissions')).toBeVisible()
  expect(screen.getByText('Queue wait time')).toBeVisible()
  expect(screen.getByText('Queue pressure')).toBeVisible()
  expect(screen.getAllByText('root').length).toBeGreaterThanOrEqual(2)
  expect(screen.getByText('ct225521')).toBeVisible()
  expect(screen.queryByText('×2')).not.toBeInTheDocument()
  queryClient.clear()
})

it('focuses the live panel and admissions chart on RPM for RPM-only channels', async () => {
  const rpmOnlyChannel = {
    id: 4,
    name: 'admission-verify',
    concurrency_limit: 0,
    rpm_limit: 5,
    priority: 0,
    settings: '{}',
    user_rate_limits: [{ user_id: 1, username: 'root', rpm_limit: 10 }],
  } as unknown as Channel

  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockResolvedValue({
    success: true,
    data: {
      channel_id: 4,
      window_start_ms: 0,
      window_end_ms: 0,
      bucket_seconds: 60,
      active: 0,
      active_users: 0,
      active_user_list: [],
      rpm: 3,
      rpm_users: [
        { user_id: 1, username: 'root', count: 2 },
        { user_id: 2, username: 'ct225521', count: 1 },
      ],
      waiting: 0,
      oldest_wait_ms: 0,
      wait_p50_ms: 0,
      wait_p95_ms: 0,
      grants: 12,
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

  const { queryClient } = await renderMonitor([rpmOnlyChannel], 4)

  expect(await screen.findByText('60%')).toBeVisible()
  expect(screen.getAllByText('RPM users')).toHaveLength(2)
  expect(screen.getByText('root')).toBeVisible()
  expect(screen.getByText('ct225521')).toBeVisible()
  expect(screen.getByText('20%')).toBeVisible()
  expect(screen.getByText('Healthy')).toBeVisible()
  expect(screen.getByText('Admissions')).toBeVisible()
  expect(screen.getByText('Grants 12')).toBeVisible()
  expect(screen.queryByText('Queued requests')).not.toBeInTheDocument()
  expect(screen.queryByText('No users are waiting')).not.toBeInTheDocument()
  expect(screen.queryByText('Selected range')).not.toBeInTheDocument()
  expect(screen.queryByText('Queue wait time')).not.toBeInTheDocument()
  expect(screen.queryByText('Queue pressure')).not.toBeInTheDocument()
  queryClient.clear()
})

it('prefers hydrated usernames from getChannel when the channel list omits them', async () => {
  const listChannel = {
    id: 4,
    name: 'admission-verify',
    concurrency_limit: 0,
    rpm_limit: 5,
    priority: 0,
    settings: '{}',
    user_rate_limits: [{ user_id: 3, rpm_limit: 2 }],
  } as unknown as Channel
  const hydratedChannel = {
    ...listChannel,
    user_rate_limits: [{ user_id: 3, username: 'alice', rpm_limit: 2 }],
  } as unknown as Channel

  vi.spyOn(channelApi, 'getChannelAdmissionStats').mockResolvedValue({
    success: true,
    data: {
      channel_id: 4,
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

  const { queryClient } = await renderMonitor([listChannel], 4, hydratedChannel)

  await waitFor(() => {
    expect(screen.getByText('alice')).toBeVisible()
  })
  expect(screen.queryByText('User #3')).not.toBeInTheDocument()
  queryClient.clear()
})
