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
import type { DateRange } from 'react-day-picker'
import { describe, expect, test, vi } from 'vitest'

import { MAX_RANKING_CUSTOM_DAYS } from '../../lib/range'
import { RankingsHero } from '../rankings-hero'

const testI18n = vi.hoisted(() => ({
  language: 'en',
  resolvedLanguage: 'en',
}))

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
  Popover: (props: { children?: ReactNode }) => <div>{props.children}</div>,
  PopoverContent: (props: { children?: ReactNode }) => (
    <div>{props.children}</div>
  ),
  PopoverTrigger: (props: { children?: ReactNode; render?: ReactNode }) => (
    <div>
      {props.render}
      {props.children}
    </div>
  ),
}))

vi.mock('@/components/ui/calendar', () => ({
  Calendar: (props: {
    max?: number
    onSelect?: (range: DateRange) => void
  }) => (
    <button
      type='button'
      aria-label='Pick dates'
      data-max-days={props.max}
      onClick={() =>
        props.onSelect?.({
          from: new Date(2026, 0, 2),
          to: new Date(2026, 0, 4),
        })
      }
    >
      Pick dates
    </button>
  ),
}))

describe('rankings date picker', () => {
  test('emits a closed date range and enforces the inclusive day limit', async () => {
    testI18n.language = 'en'
    testI18n.resolvedLanguage = 'en'
    const user = userEvent.setup()
    const onCustomRangeChange = vi.fn()

    render(
      <RankingsHero
        period='custom'
        onPeriodChange={vi.fn()}
        onCustomRangeChange={onCustomRangeChange}
      />
    )

    expect(screen.getByRole('tab', { name: 'Custom' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.getByRole('button', { name: 'Pick dates' })).toHaveAttribute(
      'data-max-days',
      String(MAX_RANKING_CUSTOM_DAYS - 1)
    )

    await user.click(screen.getByRole('button', { name: 'Pick dates' }))

    expect(onCustomRangeChange).toHaveBeenCalledWith({
      from: new Date(2026, 0, 2),
      to: new Date(2026, 0, 4),
    })
  })

  test('formats a custom range for the project zhCN locale', () => {
    testI18n.language = 'zhCN'
    testI18n.resolvedLanguage = 'zhCN'

    render(
      <RankingsHero
        period='custom'
        customRange={{
          from: new Date(2026, 0, 2),
          to: new Date(2026, 0, 4),
        }}
        onPeriodChange={vi.fn()}
        onCustomRangeChange={vi.fn()}
      />
    )

    expect(screen.getByText('2026年1月2日 – 2026年1月4日')).toBeVisible()
  })
})
