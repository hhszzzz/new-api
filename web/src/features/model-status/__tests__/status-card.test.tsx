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
import { describe, expect, test, vi } from 'vitest'

import { ModelStatusCard } from '../components/model-status-card'
import type { ModelStatusModel } from '../types'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

const GENERATED_AT = 1_800_000_000
const model: ModelStatusModel = {
  model_name: 'a-very-long-model-name-that-must-wrap-on-small-screens',
  vendor: 'A very long provider name that must wrap on small screens',
  success_rate: 98.75,
  avg_latency_ms: 820,
  avg_tps: 24.5,
  status: 'degraded',
  timeline: [
    {
      ts: GENERATED_AT,
      status: 'degraded',
      success_rate: 98.75,
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

describe('model status card', () => {
  test('uses a color-only status indicator with an accessible label', () => {
    render(
      <ModelStatusCard
        model={{ ...model, status: 'failed' }}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
      />
    )

    expect(screen.getByRole('img', { name: 'Unavailable' })).toBeVisible()
    expect(screen.queryByText('Unavailable')).not.toBeInTheDocument()
  })

  test('labels the card and exposes exactly 24 hourly status entries', () => {
    render(
      <ModelStatusCard
        model={model}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
      />
    )

    expect(
      screen.getByRole('article', { name: model.model_name })
    ).toBeVisible()
    const timeline = screen.getByRole('list', {
      name: 'Status over the last 24 hours',
    })
    const hourlyEntries = within(timeline).getAllByRole('listitem')
    expect(hourlyEntries).toHaveLength(24)
    expect(hourlyEntries.at(-1)).toHaveAccessibleName(
      expect.stringContaining('Degraded')
    )
    expect(hourlyEntries.at(-1)).toBeEmptyDOMElement()
  })

  test('uses shrinkable columns and breakable labels for narrow layouts', () => {
    render(
      <ModelStatusCard
        model={model}
        generatedAt={GENERATED_AT}
        hourFormatter={hourFormatter}
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
    expect(screen.getByText(model.vendor)).toHaveClass('break-all')
    const successRateLabel = screen.getByText('Success rate')
    expect(successRateLabel).toHaveClass('truncate')
    expect(successRateLabel.closest('dt')).toHaveClass('min-w-0')
    expect(successRateLabel.closest('dt')?.parentElement).toHaveClass('min-w-0')
    expect(timeline).toHaveClass('grid-cols-[repeat(24,minmax(0,1fr))]')
    for (const item of within(timeline).getAllByRole('listitem')) {
      expect(item).toHaveClass('min-w-0', 'overflow-hidden')
    }
  })
})
