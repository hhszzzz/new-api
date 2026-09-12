/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import type {
  RadarAutoEffortPolicy,
  RadarAutoEffortSetting,
} from '@/features/profile/types'

import { AutoEffortControls } from '../components/auto-effort-controls'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, string | number>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      ),
  }),
}))

function setting(
  overrides: Partial<RadarAutoEffortSetting> = {}
): RadarAutoEffortSetting {
  return { policy: 'highest_iq', min_iq_delta: 5, models: {}, ...overrides }
}

function renderControls(props: {
  setting?: RadarAutoEffortSetting
  isSaving?: boolean
  onPolicyChange?: (policy: RadarAutoEffortPolicy) => void
  onMinIQDeltaChange?: (value: number) => void
}) {
  const onPolicyChange = props.onPolicyChange ?? vi.fn()
  const onMinIQDeltaChange = props.onMinIQDeltaChange ?? vi.fn()
  render(
    <AutoEffortControls
      setting={props.setting ?? setting()}
      isSaving={props.isSaving ?? false}
      onPolicyChange={onPolicyChange}
      onMinIQDeltaChange={onMinIQDeltaChange}
    />
  )
  return { onPolicyChange, onMinIQDeltaChange }
}

describe('radar automatic reasoning tier controls', () => {
  test('reports the selected strategy and hides the threshold for the IQ strategies', async () => {
    const user = userEvent.setup()
    const { onPolicyChange } = renderControls({})

    expect(screen.queryByRole('spinbutton')).toBeNull()
    await user.click(
      screen.getByRole('combobox', { name: 'Selection strategy' })
    )
    await user.click(
      screen.getByRole('option', { name: /Best value for the price/ })
    )

    expect(onPolicyChange).toHaveBeenCalledWith('iq_per_cost')
  })

  test('shows each strategy explanation in a side tooltip while its option is hovered', async () => {
    const user = userEvent.setup()
    renderControls({})

    await user.click(
      screen.getByRole('combobox', { name: 'Selection strategy' })
    )

    const option = screen.getByRole('option', {
      name: /Best value for the price/,
    })
    expect(
      screen.queryByText(
        'Balance IQ against price and pick the best value tier'
      )
    ).toBeNull()

    await user.hover(option)
    expect(
      await screen.findByText(
        'Balance IQ against price and pick the best value tier'
      )
    ).toBeVisible()
    expect(option).toHaveClass('hover:bg-accent')
    expect(
      screen.getByText(
        'Automatic reasoning tiers adjust to a suitable reasoning effort based on the strategy.'
      )
    ).toBeVisible()
  })

  test('shows the threshold input for the minimum-delta strategy and commits valid edits', async () => {
    const user = userEvent.setup()
    const { onMinIQDeltaChange } = renderControls({
      setting: setting({ policy: 'min_iq_delta', min_iq_delta: 5 }),
    })

    const input = screen.getByRole('spinbutton', { name: 'IQ gap' })
    expect(input).toHaveValue(5)

    await user.clear(input)
    await user.type(input, '30')
    await user.tab()

    expect(onMinIQDeltaChange).toHaveBeenCalledWith(30)
  })

  test.each(['0', '900'])(
    'restores the stored threshold when the draft %s is out of range',
    async (draft) => {
      const user = userEvent.setup()
      const { onMinIQDeltaChange } = renderControls({
        setting: setting({ policy: 'min_iq_delta', min_iq_delta: 5 }),
      })

      const input = screen.getByRole('spinbutton', { name: 'IQ gap' })
      await user.clear(input)
      await user.type(input, draft)
      await user.tab()

      expect(onMinIQDeltaChange).not.toHaveBeenCalled()
      expect(input).toHaveValue(5)
    }
  )

  test('ignores edits that arrive while a save is in flight instead of shifting the row', async () => {
    const user = userEvent.setup()
    const { onPolicyChange, onMinIQDeltaChange } = renderControls({
      setting: setting({ policy: 'min_iq_delta', min_iq_delta: 5 }),
      isSaving: true,
    })

    expect(screen.queryByText('Saving...')).toBeNull()
    await user.click(
      screen.getByRole('combobox', { name: 'Selection strategy' })
    )
    await user.click(
      screen.getByRole('option', { name: /Best value for the price/ })
    )
    expect(onPolicyChange).not.toHaveBeenCalled()

    const input = screen.getByRole('spinbutton', { name: 'IQ gap' })
    await user.clear(input)
    await user.type(input, '30')
    await user.tab()
    expect(onMinIQDeltaChange).not.toHaveBeenCalled()
  })
})
