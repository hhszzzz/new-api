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
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { USD_PRICING_CURRENCY } from '@/features/model-pricing/currency'

import { TimeBasedPricingEditor } from '../time-based-pricing-editor'

const { translateMock, i18nState } = vi.hoisted(() => ({
  translateMock: (key: string, values?: Record<string, string | number>) =>
    Object.entries(values || {}).reduce(
      (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
      key
    ),
  i18nState: {
    language: 'en' as string,
    resolvedLanguage: undefined as string | undefined,
  },
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translateMock, i18n: i18nState }),
}))

const ENABLED =
  'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? tier("peak", p * 3 + c * 9) : tier("off_peak", p * 1.5 + c * 4.5)'
const FLAT = 'tier("base", p * 1.5 + c * 4.5)'

function lastExpression(onChange: ReturnType<typeof vi.fn>): string {
  const value = onChange.mock.lastCall?.[0]
  expect(typeof value).toBe('string')
  return value as string
}

function renderEditor(billingExpr: string) {
  const onChange = vi.fn()
  render(
    <TimeBasedPricingEditor
      currency={USD_PRICING_CURRENCY}
      billingExpr={billingExpr}
      onBillingExprChange={onChange}
    />
  )
  return onChange
}

function ControlledHarness({ initial }: { initial: string }) {
  const [expr, setExpr] = useState(initial)
  return (
    <TimeBasedPricingEditor
      currency={USD_PRICING_CURRENCY}
      billingExpr={expr}
      onBillingExprChange={setExpr}
    />
  )
}

describe('time based pricing editor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    i18nState.language = 'en'
    i18nState.resolvedLanguage = undefined
  })

  test('renders weekday labels for the project zhCN/zhTW language codes', () => {
    // The project's i18next codes (`zhCN` / `zhTW`) are not valid BCP-47 tags;
    // passing them raw into Intl.DateTimeFormat throws RangeError. Both must
    // survive by being mapped through toIntlLocale.
    for (const language of ['zhCN', 'zhTW']) {
      i18nState.language = language
      const { container, unmount } = render(
        <TimeBasedPricingEditor
          currency={USD_PRICING_CURRENCY}
          billingExpr={ENABLED}
          onBillingExprChange={vi.fn()}
        />
      )
      expect(container.querySelectorAll('button[aria-pressed]')).toHaveLength(7)
      unmount()
    }
  })

  test('keeps the standard price as the flat baseline while the toggle is off', () => {
    renderEditor(FLAT)

    expect(
      screen.getByRole('switch', { name: 'Peak hours pricing' })
    ).not.toBeChecked()
    expect(screen.getByText('Standard price')).toBeVisible()
    expect(screen.queryByText('Peak price')).toBeNull()
    expect(
      screen.getAllByRole('textbox', { name: 'Input price' })[0]
    ).toHaveValue('1.5')
  })

  test('turning the toggle on emits a peak and off-peak branch', async () => {
    const user = userEvent.setup()
    const onChange = renderEditor(FLAT)

    await user.click(screen.getByRole('switch', { name: 'Peak hours pricing' }))

    const expression = lastExpression(onChange)
    expect(expression).toContain('? tier("peak"')
    expect(expression).toContain('tier("off_peak", p * 1.5 + c * 4.5)')
    // New schedules adopt the interface language's timezone (en -> UTC).
    expect(expression).toContain('weekday("UTC") >= 1')
    expect(expression).toContain('hour("UTC") >= 9')
  })

  test('new schedules use the timezone implied by the interface language', async () => {
    const user = userEvent.setup()
    i18nState.language = 'zhCN'
    const onChange = renderEditor(FLAT)

    await user.click(screen.getByRole('switch', { name: 'Peak hours pricing' }))

    const expression = lastExpression(onChange)
    expect(expression).toContain('weekday("Asia/Shanghai") >= 1')
    expect(expression).toContain('hour("Asia/Shanghai") >= 9')
  })

  test('existing schedules keep the timezone saved in the expression', async () => {
    const user = userEvent.setup()
    i18nState.language = 'zhCN'
    const onChange = renderEditor(ENABLED)

    const monday = screen.getByRole('button', { name: '周一' })
    await user.click(monday)

    expect(lastExpression(onChange)).toContain(
      'weekday("Asia/Shanghai") >= 2 && weekday("Asia/Shanghai") <= 5'
    )
  })

  test('turning the toggle off drops the schedule and bills off-peak flat', async () => {
    const user = userEvent.setup()
    const onChange = renderEditor(ENABLED)

    await user.click(screen.getByRole('switch', { name: 'Peak hours pricing' }))

    expect(lastExpression(onChange)).toBe('tier("base", p * 1.5 + c * 4.5)')
  })

  test('peak section exposes multipliers with the absolute prices advanced section collapsed', () => {
    renderEditor(ENABLED)

    // DOM order: the off-peak (standard) absolute prices render first, then
    // the peak multiplier fields derived from them (3 / 1.5 = 2x).
    const inputs = screen.getAllByRole('textbox', { name: 'Input price' })
    expect(inputs.length).toBeGreaterThanOrEqual(2)
    expect(inputs[0]).toHaveValue('1.5')
    expect(inputs[1]).toHaveValue('2')
  })

  test('editing a peak multiplier rebuilds the peak prices', () => {
    const onChange = renderEditor(ENABLED)

    const inputs = screen.getAllByRole('textbox', { name: 'Input price' })
    fireEvent.change(inputs[1], { target: { value: '3' } })

    const expression = lastExpression(onChange)
    expect(expression).toContain('tier("peak", p * 4.5 + c * 9)')
    expect(expression).toContain('tier("off_peak", p * 1.5 + c * 4.5)')
  })

  test('editing an off-peak price keeps peak in sync via the multipliers', () => {
    const onChange = renderEditor(ENABLED)

    const inputs = screen.getAllByRole('textbox', { name: 'Input price' })
    // Raise the peak multiplier first: 1.5 * 4 = 6.
    fireEvent.change(inputs[1], { target: { value: '4' } })
    expect(lastExpression(onChange)).toContain('tier("peak", p * 6 + c * 9)')

    // Changing the standard price rebuilds peak from the recorded multipliers.
    fireEvent.change(inputs[0], { target: { value: '2' } })
    const expression = lastExpression(onChange)
    expect(expression).toContain('tier("off_peak", p * 2 + c * 4.5)')
    expect(expression).toContain('tier("peak", p * 8 + c * 9)')
  })

  test('advanced absolute prices open to expose the absolute peak fields', async () => {
    const user = userEvent.setup()
    renderEditor(ENABLED)

    // Collapsed: the off-peak section and the peak multipliers each own one
    // cache-read field; the advanced absolute section adds a third.
    expect(
      screen.queryAllByRole('textbox', { name: 'Cache read price' })
    ).toHaveLength(2)
    await user.click(
      screen.getByRole('button', { name: 'Advanced absolute prices' })
    )
    expect(
      screen.queryAllByRole('textbox', { name: 'Cache read price' })
    ).toHaveLength(3)

    const absolute = screen.getAllByRole('textbox', { name: 'Input price' })
    expect(absolute.length).toBeGreaterThanOrEqual(3)
    expect(absolute[2]).toHaveValue('3')
  })

  test('toggling a weekday rewrites the weekday bounds', async () => {
    const user = userEvent.setup()
    const onChange = renderEditor(ENABLED)

    const monday = screen.getByRole('button', { name: 'Mon' })
    expect(monday).toHaveAttribute('aria-pressed', 'true')
    await user.click(monday)

    expect(monday).toHaveAttribute('aria-pressed', 'false')
    expect(lastExpression(onChange)).toContain(
      'weekday("Asia/Shanghai") >= 2 && weekday("Asia/Shanghai") <= 5'
    )
  })

  test('adds and removes time windows', async () => {
    const user = userEvent.setup()
    const onChange = renderEditor(ENABLED)

    expect(
      screen.getAllByRole('button', { name: 'Remove time window' })
    ).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: 'Add time window' }))
    expect(
      screen.getAllByRole('button', { name: 'Remove time window' })
    ).toHaveLength(2)
    expect(lastExpression(onChange)).toMatch(
      /\(\(hour\("Asia\/Shanghai"\) >= 9.*\) \|\| \(hour\("Asia\/Shanghai"\) >= 9.*\)\)/
    )

    await user.click(
      screen.getAllByRole('button', { name: 'Remove time window' })[1]
    )
    expect(
      screen.getAllByRole('button', { name: 'Remove time window' })
    ).toHaveLength(1)
  })

  test('preserves a condition the schedule editor cannot represent', () => {
    const onChange = renderEditor(
      'month("UTC") == 12 ? tier("peak", p * 2) : tier("off_peak", p * 1)'
    )

    expect(
      screen.getByText(
        'This condition is too complex for the schedule editor. It is preserved as-is; edit it in expression mode.'
      )
    ).toBeVisible()
    expect(screen.getByText('month("UTC") == 12')).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Add time window' })).toBeNull()

    // Index 0 is the off-peak absolute input; raising it rebuilds the peak
    // branch from the recorded 2x multiplier (1 * 2 = 2 → 3 * 2 = 6).
    fireEvent.change(
      screen.getAllByRole('textbox', { name: 'Input price' })[0],
      { target: { value: '3' } }
    )
    expect(lastExpression(onChange)).toBe(
      'month("UTC") == 12 ? tier("peak", p * 6) : tier("off_peak", p * 3)'
    )
  })

  test('stays stable when the parent echoes the emitted expression back', async () => {
    const user = userEvent.setup()
    render(<ControlledHarness initial={FLAT} />)

    await user.click(screen.getByRole('switch', { name: 'Peak hours pricing' }))

    // The echo must not reset the editor: the peak branch stays visible.
    expect(screen.getByText('Peak price')).toBeVisible()
    expect(screen.getByText('Standard price')).toBeVisible()
    expect(
      screen.getByRole('switch', { name: 'Peak hours pricing' })
    ).toBeChecked()
  })
})
