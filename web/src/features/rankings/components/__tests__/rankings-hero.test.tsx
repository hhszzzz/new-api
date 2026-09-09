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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { RankingsHero } from '../rankings-hero'

const testI18n = vi.hoisted(() => ({
  language: 'en',
  resolvedLanguage: 'en',
}))

type MockCalendarProps = {
  selected?: Date
  onSelect?: (date: Date | undefined) => void
  disabled?: (date: Date) => boolean
}

// Captured per render: [0] is the start picker, [1] the end picker.
const calendarRefs = vi.hoisted(() => [] as MockCalendarProps[])

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string | number>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      ),
    i18n: testI18n,
  }),
}))

vi.mock('@/components/ui/button', () => ({
  Button: (props: { children?: ReactNode } & Record<string, unknown>) => {
    const { children, ...rest } = props
    return (
      <button type='button' {...rest}>
        {children}
      </button>
    )
  },
}))

vi.mock('@/components/ui/popover', () => ({
  Popover: (props: {
    children?: ReactNode
    open?: boolean
    onOpenChange?: (open: boolean) => void
  }) => (
    <div data-open={String(props.open ?? false)}>
      <button
        type='button'
        aria-label='toggle-popover'
        onClick={() => props.onOpenChange?.(!props.open)}
      />
      {props.children}
    </div>
  ),
  PopoverContent: (props: { children?: ReactNode }) => <div>{props.children}</div>,
  PopoverTrigger: (props: { children?: ReactNode; render?: ReactNode }) => (
    <div>
      {props.render}
      {props.children}
    </div>
  ),
}))

vi.mock('@/components/ui/calendar', () => ({
  Calendar: (props: MockCalendarProps) => {
    calendarRefs.push(props)
    return (
      <button
        type='button'
        aria-label='Pick date'
        data-selected={props.selected?.getTime() ?? ''}
        onClick={() => props.onSelect?.(new Date(2026, 0, 3))}
      >
        Pick date
      </button>
    )
  },
}))

function renderHero(customRange?: { from: Date; to: Date }) {
  calendarRefs.length = 0
  const onCustomRangeChange = vi.fn()
  render(
    <RankingsHero
      period='custom'
      customRange={customRange}
      onPeriodChange={vi.fn()}
      onCustomRangeChange={onCustomRangeChange}
    />
  )
  expect(calendarRefs).toHaveLength(2)
  return { onCustomRangeChange }
}

describe('rankings custom date pickers', () => {
  test('renders separate start and end pickers with the current range', () => {
    testI18n.language = 'en'
    testI18n.resolvedLanguage = 'en'
    const { onCustomRangeChange } = renderHero({
      from: new Date(2026, 0, 2),
      to: new Date(2026, 0, 4),
    })

    expect(screen.getByRole('tab', { name: 'Custom' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.getByRole('button', { name: 'Start date' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'End date' })).toBeVisible()
    expect(screen.getByText('Jan 2, 2026')).toBeVisible()
    expect(screen.getByText('Jan 4, 2026')).toBeVisible()
    expect(calendarRefs[0].selected).toEqual(new Date(2026, 0, 2))
    expect(calendarRefs[1].selected).toEqual(new Date(2026, 0, 4))
    expect(onCustomRangeChange).not.toHaveBeenCalled()
  })

  test('selecting a start date keeps the current end and closes the picker', async () => {
    testI18n.language = 'en'
    testI18n.resolvedLanguage = 'en'
    const user = userEvent.setup()
    const { onCustomRangeChange } = renderHero({
      from: new Date(2026, 0, 2),
      to: new Date(2026, 0, 4),
    })

    const startPopover = screen
      .getByRole('button', { name: 'Start date' })
      .closest('[data-open]') as HTMLElement
    expect(startPopover.getAttribute('data-open')).toBe('false')

    await user.click(
      screen.getAllByRole('button', { name: 'toggle-popover' })[0]
    )
    expect(startPopover.getAttribute('data-open')).toBe('true')

    await user.click(screen.getAllByRole('button', { name: 'Pick date' })[0])

    expect(onCustomRangeChange).toHaveBeenCalledWith({
      from: new Date(2026, 0, 3),
      to: new Date(2026, 0, 4),
    })
    expect(startPopover.getAttribute('data-open')).toBe('false')
  })

  test('selecting an end date keeps the current start and closes the picker', async () => {
    testI18n.language = 'en'
    testI18n.resolvedLanguage = 'en'
    const user = userEvent.setup()
    const { onCustomRangeChange } = renderHero({
      from: new Date(2026, 0, 2),
      to: new Date(2026, 0, 4),
    })

    const endPopover = screen
      .getByRole('button', { name: 'End date' })
      .closest('[data-open]') as HTMLElement

    await user.click(
      screen.getAllByRole('button', { name: 'toggle-popover' })[1]
    )
    expect(endPopover.getAttribute('data-open')).toBe('true')

    await user.click(screen.getAllByRole('button', { name: 'Pick date' })[1])

    expect(onCustomRangeChange).toHaveBeenCalledWith({
      from: new Date(2026, 0, 2),
      to: new Date(2026, 0, 3),
    })
    expect(endPopover.getAttribute('data-open')).toBe('false')
  })

  test('start picker rejects dates after the end day or beyond the day cap', () => {
    testI18n.language = 'en'
    testI18n.resolvedLanguage = 'en'
    renderHero({ from: new Date(2020, 0, 2), to: new Date(2020, 0, 4) })

    const disabled = (date: Date) => calendarRefs[0].disabled?.(date) ?? false
    expect(disabled(new Date(2020, 0, 5))).toBe(true)
    expect(disabled(new Date(2020, 0, 4))).toBe(false)
    expect(disabled(new Date(2020, 0, 3))).toBe(false)
    expect(disabled(new Date(2019, 0, 4))).toBe(false)
    expect(disabled(new Date(2019, 0, 3))).toBe(true)
    expect(disabled(new Date(2100, 0, 1))).toBe(true)
  })

  test('end picker rejects dates before the start day or beyond the day cap', () => {
    testI18n.language = 'en'
    testI18n.resolvedLanguage = 'en'
    renderHero({ from: new Date(2020, 0, 2), to: new Date(2020, 0, 4) })

    const disabled = (date: Date) => calendarRefs[1].disabled?.(date) ?? false
    expect(disabled(new Date(2019, 11, 31))).toBe(true)
    expect(disabled(new Date(2020, 0, 2))).toBe(false)
    expect(disabled(new Date(2020, 0, 3))).toBe(false)
    // 2020 is a leap year: Jan 2 2020 + 365 days lands on Jan 1 2021, which
    // is exactly the 366-closed-day cap.
    expect(disabled(new Date(2021, 0, 1))).toBe(false)
    expect(disabled(new Date(2021, 0, 2))).toBe(true)
    expect(disabled(new Date(2100, 0, 1))).toBe(true)
  })

  test('formats both dates for the project zhCN locale', () => {
    testI18n.language = 'zhCN'
    testI18n.resolvedLanguage = 'zhCN'

    renderHero({ from: new Date(2026, 0, 2), to: new Date(2026, 0, 4) })

    expect(screen.getByText('2026年1月2日')).toBeVisible()
    expect(screen.getByText('2026年1月4日')).toBeVisible()
  })
})
