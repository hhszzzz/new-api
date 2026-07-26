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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { HEADER_NAV_DEFAULT, serializeHeaderNavModules } from '../config'
import { HeaderNavigationSection } from '../header-navigation-section'

const { mutateAsyncMock } = vi.hoisted(() => ({
  mutateAsyncMock: vi.fn(),
}))

vi.mock('@/features/system-settings/hooks/use-update-option', () => ({
  useUpdateOption: () => ({
    isPending: false,
    mutateAsync: mutateAsyncMock,
  }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('header navigation settings form', () => {
  test('saves model status login access independently from model square', async () => {
    mutateAsyncMock.mockResolvedValue({ success: true })
    const user = userEvent.setup()
    const actionsContainer = document.createElement('div')
    document.body.appendChild(actionsContainer)

    render(
      <SettingsPageProvider
        actionsContainer={actionsContainer}
        suppressSectionHeader={false}
      >
        <HeaderNavigationSection
          config={HEADER_NAV_DEFAULT}
          initialSerialized={serializeHeaderNavModules(HEADER_NAV_DEFAULT)}
        />
      </SettingsPageProvider>
    )

    const requireAuthSwitch = screen.getByRole('switch', {
      name: 'Require login to view model status',
    })
    expect(requireAuthSwitch).toBeEnabled()

    await user.click(requireAuthSwitch)
    expect(requireAuthSwitch).toBeChecked()
    await user.click(screen.getByRole('button', { name: 'Save navigation' }))

    await waitFor(() => expect(mutateAsyncMock).toHaveBeenCalledOnce())
    const request = mutateAsyncMock.mock.calls[0]?.[0] as {
      key: string
      value: string
    }
    const saved = JSON.parse(request.value) as typeof HEADER_NAV_DEFAULT

    expect(request.key).toBe('HeaderNavModules')
    expect(saved.modelStatus).toEqual({
      enabled: true,
      requireAuth: true,
    })
    expect(saved.pricing).toEqual(HEADER_NAV_DEFAULT.pricing)

    expect(saved.modelRadar).toEqual(HEADER_NAV_DEFAULT.modelRadar)

    actionsContainer.remove()
  })

  test('saves model radar login access independently', async () => {
    mutateAsyncMock.mockResolvedValue({ success: true })
    const user = userEvent.setup()
    const actionsContainer = document.createElement('div')
    document.body.appendChild(actionsContainer)

    render(
      <SettingsPageProvider
        actionsContainer={actionsContainer}
        suppressSectionHeader={false}
      >
        <HeaderNavigationSection
          config={HEADER_NAV_DEFAULT}
          initialSerialized={serializeHeaderNavModules(HEADER_NAV_DEFAULT)}
        />
      </SettingsPageProvider>
    )

    await user.click(
      screen.getByRole('switch', {
        name: 'Require login to view model radar',
      })
    )
    await user.click(screen.getByRole('button', { name: 'Save navigation' }))

    await waitFor(() => expect(mutateAsyncMock).toHaveBeenCalled())
    const request = mutateAsyncMock.mock.calls.at(-1)?.[0] as {
      value: string
    }
    const saved = JSON.parse(request.value) as typeof HEADER_NAV_DEFAULT
    expect(saved.modelRadar).toEqual({ enabled: true, requireAuth: true })
    expect(saved.modelStatus).toEqual(HEADER_NAV_DEFAULT.modelStatus)

    actionsContainer.remove()
  })
})
