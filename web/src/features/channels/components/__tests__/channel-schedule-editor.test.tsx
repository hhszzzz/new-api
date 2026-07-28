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
import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { createEmptyChannelSchedule } from '../../lib/channel-schedule'
import type { ChannelSchedule } from '../../types'
import { ChannelScheduleEditor } from '../channel-schedule-editor'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, value),
        key
      ),
    i18n: { language: 'en', resolvedLanguage: 'en' },
  }),
}))

function ScheduleHarness() {
  const [schedule, setSchedule] = useState<ChannelSchedule>(
    createEmptyChannelSchedule()
  )

  return (
    <>
      <ChannelScheduleEditor value={schedule} onChange={setSchedule} />
      <output data-testid='schedule-value'>{JSON.stringify(schedule)}</output>
    </>
  )
}

describe('channel schedule editor', () => {
  test('reveals a localized date rule on demand while preserving the schedule clock', async () => {
    const user = userEvent.setup()
    render(<ScheduleHarness />)

    expect(screen.queryByLabelText('Enable from date and time')).toBeNull()
    expect(screen.queryByText('Beijing time (UTC+8)')).toBeNull()

    await user.click(screen.getByRole('switch', { name: 'Enable from' }))
    fireEvent.change(screen.getByLabelText('Enable from date and time'), {
      target: { value: '2026-07-27T09:30' },
    })

    const value = JSON.parse(
      screen.getByTestId('schedule-value').textContent || '{}'
    ) as ChannelSchedule
    expect(value.starts_at).toBe(Date.UTC(2026, 6, 27, 1, 30) / 1000)
  })

  test('clears and hides a configured date rule when it is disabled', async () => {
    const user = userEvent.setup()
    render(<ScheduleHarness />)

    const startRule = screen.getByRole('switch', { name: 'Enable from' })
    await user.click(startRule)
    expect(screen.getByLabelText('Enable from date and time')).toBeVisible()

    await user.click(startRule)

    expect(screen.queryByLabelText('Enable from date and time')).toBeNull()
    const value = JSON.parse(
      screen.getByTestId('schedule-value').textContent || '{}'
    ) as ChannelSchedule
    expect(value.starts_at).toBeNull()
  })

  test('uses one weekly label and lets each weekday be enabled independently', async () => {
    const user = userEvent.setup()
    render(<ScheduleHarness />)

    expect(screen.queryByText('Use weekly time windows')).toBeNull()
    expect(screen.getAllByText('Weekly availability')).toHaveLength(1)

    await user.click(
      screen.getByRole('switch', { name: 'Weekly availability' })
    )

    expect(screen.getByTestId('weekly-schedule-days')).toHaveClass(
      'ml-3',
      'border-l-2'
    )
    expect(screen.getByRole('switch', { name: 'Monday' })).not.toBeChecked()
    expect(screen.getByRole('switch', { name: 'Saturday' })).not.toBeChecked()
    expect(screen.queryByLabelText('Monday start time')).toBeNull()
    expect(screen.queryByLabelText('Saturday start time')).toBeNull()

    await user.click(screen.getByRole('switch', { name: 'Saturday' }))
    expect(screen.getByLabelText('Saturday start time')).toHaveValue('09:00')

    const value = JSON.parse(
      screen.getByTestId('schedule-value').textContent || '{}'
    ) as ChannelSchedule
    expect(value.weekly_windows.monday).toBeUndefined()
    expect(value.weekly_windows.saturday).toEqual([
      { start: '09:00', end: '18:00' },
    ])
  })

  test('supports all-day windows for an enabled weekday', async () => {
    const user = userEvent.setup()
    render(<ScheduleHarness />)

    await user.click(
      screen.getByRole('switch', { name: 'Weekly availability' })
    )
    await user.click(screen.getByRole('switch', { name: 'Monday' }))

    const monday = screen.getByRole('group', { name: 'Monday' })
    const mondayAllDay = within(monday).getByRole('checkbox', {
      name: 'All day',
    })
    await user.click(mondayAllDay)

    const value = JSON.parse(
      screen.getByTestId('schedule-value').textContent || '{}'
    ) as ChannelSchedule
    expect(value.weekly_enabled).toBe(true)
    expect(value.weekly_windows.monday).toEqual([{ all_day: true }])
  })

  test('adds multiple windows without replacing an existing window', async () => {
    const user = userEvent.setup()
    render(<ScheduleHarness />)

    await user.click(
      screen.getByRole('switch', { name: 'Weekly availability' })
    )
    await user.click(screen.getByRole('switch', { name: 'Monday' }))
    const monday = screen.getByRole('group', { name: 'Monday' })
    const mondayAddWindow = within(monday).getByRole('button', {
      name: 'Add time window',
    })
    await user.click(mondayAddWindow)

    const value = JSON.parse(
      screen.getByTestId('schedule-value').textContent || '{}'
    ) as ChannelSchedule
    expect(value.weekly_windows.monday).toHaveLength(2)
  })
})
