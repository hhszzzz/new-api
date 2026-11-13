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
import { afterEach, expect, it } from 'vitest'

import type { Channel } from '../../types'
import { ChannelAdmissionSettingsPanel } from '../channel-admission-settings-panel'

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
  settings: JSON.stringify({
    admission_control: {
      wait_timeout_ms: 5_000,
      max_waiting_total: 30,
      soup_mode: 'off',
    },
  }),
  user_rate_limits: [{ user_id: 5, username: 'cbk', concurrency_limit: 1 }],
} as unknown as Channel

afterEach(() => {
  cleanup()
})

it('shows effective admission settings and links to the channel editor', async () => {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const channelsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'channels',
    component: () => <Outlet />,
  })
  const listRoute = createRoute({
    getParentRoute: () => channelsRoute,
    path: '/',
    component: () => <ChannelAdmissionSettingsPanel channel={channel} />,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([channelsRoute.addChildren([listRoute])]),
    history: createMemoryHistory({ initialEntries: ['/channels'] }),
  })
  await router.load()

  render(
    <I18nextProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nextProvider>
  )

  expect(await screen.findByText('Admission settings')).toBeVisible()
  expect(screen.getByText('Customized')).toBeVisible()
  expect(screen.queryByText('5.0s')).not.toBeInTheDocument()

  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: /Admission settings/ }))

  expect(await screen.findByText('5.0s')).toBeVisible()
  expect(screen.getByText('Off')).toBeVisible()
  expect(screen.getByText('cbk · ≤ 1')).toBeVisible()

  const editLink = screen.getByRole('button', {
    name: 'Edit channel settings',
  })
  expect(editLink.getAttribute('href')).toContain('edit=12')
})
