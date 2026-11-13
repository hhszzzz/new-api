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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it, vi } from 'vitest'

import { ChannelAdmissionLiveRefreshSelect } from '../channel-admission-live-refresh'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

afterEach(() => {
  cleanup()
})

it('changes the live refresh interval from the select', async () => {
  const onChange = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <ChannelAdmissionLiveRefreshSelect seconds={10} onChange={onChange} />
    </I18nextProvider>
  )

  const select = screen.getByRole('combobox', { name: 'Auto refresh' })
  expect(select).toHaveTextContent('10s')

  await userEvent.setup().click(select)
  await userEvent.setup().click(screen.getByRole('option', { name: '30s' }))

  expect(onChange).toHaveBeenCalledWith(30)
})

it('shows the paused state when refreshes are disabled', () => {
  render(
    <I18nextProvider i18n={i18n}>
      <ChannelAdmissionLiveRefreshSelect seconds={0} onChange={vi.fn()} />
    </I18nextProvider>
  )

  expect(
    screen.getByRole('combobox', { name: 'Auto refresh' })
  ).toHaveTextContent('Off')
})
