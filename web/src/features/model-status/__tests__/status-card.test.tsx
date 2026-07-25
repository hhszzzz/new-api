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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { ModelStatusCard } from '../components/model-status-card'
import type { ModelStatusModel } from '../types'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (icon: string) => <span data-testid='model-icon'>{icon}</span>,
}))

const GENERATED_AT = 1_800_000_000
const model: ModelStatusModel = {
  model_name: 'a-very-long-model-name-that-must-wrap-on-small-screens',
  vendor: 'A very long provider name that must wrap on small screens',
  icon: 'Claude.Color',
  request_count: 12,
  success_count: 11,
  success_rate: 91.67,
  avg_ttft_ms: 240,
  avg_latency_ms: 820,
  avg_tps: 24.5,
  status: 'degraded',
  timeline: [
    {
      ts: GENERATED_AT,
      status: 'degraded',
      request_count: 12,
      success_count: 11,
      success_rate: 91.67,
      avg_ttft_ms: 240,
      avg_latency_ms: 820,
      avg_tps: 24.5,
    },
  ],
}
const hourFormatter = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  timeZone: 'UTC',
})
const numberFormatter = new Intl.NumberFormat('en-US')

describe('model status card', () => {
  test('renders the model icon beside its name and keeps status screen-reader only', () => {
    render(
      <ModelStatusCard
        model={{ ...model, status: 'failed' }}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
        numberFormatter={numberFormatter}
      />
    )

    const article = screen.getByRole('article', { name: model.model_name })
    expect(screen.getByTestId('model-icon')).toHaveTextContent('Claude.Color')
    expect(article).toHaveAccessibleDescription('Unavailable')
    expect(screen.queryByRole('img', { name: 'Unavailable' })).toBeNull()
    expect(screen.queryByText(model.vendor)).toBeNull()
    expect(screen.queryByText('Last 24 hours')).toBeNull()
  })

  test('shows six aggregate metrics and exactly 24 detailed hourly entries', () => {
    render(
      <ModelStatusCard
        model={model}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
        numberFormatter={numberFormatter}
      />
    )

    const article = screen.getByRole('article', { name: model.model_name })
    expect(article).toBeVisible()
    expect(within(article).getAllByRole('term')).toHaveLength(6)
    expect(within(article).getByText('Requests')).toBeVisible()
    expect(within(article).getByText('Successful requests')).toBeVisible()
    expect(within(article).getByText('Success rate')).toBeVisible()
    expect(within(article).getByText('Average TTFT')).toBeVisible()
    expect(within(article).getByText('Average latency')).toBeVisible()
    expect(within(article).getByText('Throughput')).toBeVisible()
    expect(within(article).getByText('12')).toBeVisible()
    expect(within(article).getByText('11')).toBeVisible()
    expect(within(article).getByText('91.67%')).toBeVisible()
    expect(within(article).getByText('240ms')).toBeVisible()
    expect(within(article).getByText('820ms')).toBeVisible()
    expect(within(article).getByText('24.5 t/s')).toBeVisible()

    const timeline = screen.getByRole('list', {
      name: 'Status over the last 24 hours',
    })
    const hourlyEntries = within(timeline).getAllByRole('listitem')
    expect(hourlyEntries).toHaveLength(24)
    const lastHour = hourlyEntries.at(-1)
    if (!lastHour) throw new Error('Expected a final hourly status entry')
    const expectedTimeRange = hourFormatter.formatRange(
      new Date(GENERATED_AT * 1000),
      new Date((GENERATED_AT + 60 * 60) * 1000)
    )
    expect(lastHour).toHaveAccessibleName(
      expect.stringContaining(expectedTimeRange)
    )
    expect(lastHour).toHaveAccessibleName(
      expect.stringContaining('Requests: 12')
    )
    expect(lastHour).toHaveAccessibleName(
      expect.stringContaining('Average TTFT: 240ms')
    )
    expect(within(lastHour).getByRole('button')).toHaveAttribute(
      'type',
      'button'
    )
  })

  test('shows complete localized hourly metrics when an hour is hovered', async () => {
    const user = userEvent.setup()
    render(
      <ModelStatusCard
        model={model}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
        numberFormatter={numberFormatter}
      />
    )

    const timeline = screen.getByRole('list', {
      name: 'Status over the last 24 hours',
    })
    const lastHour = within(timeline).getAllByRole('listitem').at(-1)
    if (!lastHour) throw new Error('Expected a final hourly status entry')
    await user.hover(within(lastHour).getByRole('button'))

    const tooltip = await screen.findByRole('tooltip')
    const expectedTimeRange = hourFormatter.formatRange(
      new Date(GENERATED_AT * 1000),
      new Date((GENERATED_AT + 60 * 60) * 1000)
    )
    const timeLabel = within(tooltip).getByText('Time')
    expect(timeLabel).toBeVisible()
    expect(timeLabel.nextElementSibling?.textContent).toBe(expectedTimeRange)
    expect(within(tooltip).getByText('Requests')).toBeVisible()
    expect(within(tooltip).getByText('Successful requests')).toBeVisible()
    expect(within(tooltip).getByText('Success rate')).toBeVisible()
    expect(within(tooltip).getByText('Average TTFT')).toBeVisible()
    expect(within(tooltip).getByText('Average latency')).toBeVisible()
    expect(within(tooltip).getByText('Throughput')).toBeVisible()
    expect(within(tooltip).getByText('12')).toBeVisible()
    expect(within(tooltip).getByText('11')).toBeVisible()
    expect(within(tooltip).getByText('91.67%')).toBeVisible()
    expect(within(tooltip).getByText('240ms')).toBeVisible()
    expect(within(tooltip).getByText('820ms')).toBeVisible()
    expect(within(tooltip).getByText('24.5 t/s')).toBeVisible()
  })

  test('shows zero counts and unavailable metrics on keyboard focus without data', async () => {
    const user = userEvent.setup()
    render(
      <ModelStatusCard
        model={{
          ...model,
          icon: '',
          request_count: 0,
          success_count: 0,
          success_rate: null,
          avg_ttft_ms: null,
          avg_latency_ms: null,
          avg_tps: null,
          status: 'no_data',
          timeline: [],
        }}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
        numberFormatter={numberFormatter}
      />
    )

    const article = screen.getByRole('article', { name: model.model_name })
    expect(within(article).getAllByText('0')).toHaveLength(2)
    expect(within(article).getAllByText('—')).toHaveLength(4)

    const timeline = within(article).getByRole('list', {
      name: 'Status over the last 24 hours',
    })
    const firstHour = within(timeline).getAllByRole('listitem')[0]
    if (!firstHour) throw new Error('Expected an initial hourly status entry')
    const firstHourButton = within(firstHour).getByRole('button')
    await user.tab()
    expect(firstHourButton).toHaveFocus()

    const tooltip = await screen.findByRole('tooltip')
    expect(within(tooltip).getAllByText('0')).toHaveLength(2)
    expect(within(tooltip).getAllByText('—')).toHaveLength(4)
  })

  test('uses shrinkable columns and breakable labels for narrow layouts', () => {
    render(
      <ModelStatusCard
        model={model}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
        numberFormatter={numberFormatter}
      />
    )

    const article = screen.getByRole('article', { name: model.model_name })
    const timeline = screen.getByRole('list', {
      name: 'Status over the last 24 hours',
    })

    expect(article).toHaveClass('min-w-0')
    expect(screen.getByRole('heading', { name: model.model_name })).toHaveClass(
      'break-all'
    )
    const successfulRequestsLabel = screen.getByText('Successful requests')
    expect(successfulRequestsLabel).toHaveClass('truncate')
    expect(successfulRequestsLabel.closest('dt')).toHaveClass('min-w-0')
    expect(successfulRequestsLabel.closest('dt')?.parentElement).toHaveClass(
      'min-w-0'
    )
    expect(successfulRequestsLabel.closest('dl')).toHaveClass('grid-cols-6')
    expect(timeline).toHaveClass('grid-cols-[repeat(24,minmax(0,1fr))]')
    for (const item of within(timeline).getAllByRole('listitem')) {
      expect(item).toHaveClass('min-w-0')
      expect(within(item).getByRole('button')).toHaveClass(
        'min-w-0',
        'overflow-hidden'
      )
    }
  })

  test('uses the model initial when no icon is configured', () => {
    render(
      <ModelStatusCard
        model={{ ...model, icon: '' }}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
        numberFormatter={numberFormatter}
      />
    )

    expect(screen.queryByTestId('model-icon')).toBeNull()
    expect(screen.getByText('A')).toBeVisible()
  })
})
