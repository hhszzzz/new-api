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
import { describe, expect, test, vi } from 'vitest'

import { EfficiencyScatter } from '../components/efficiency-scatter'
import { HistoryComparison } from '../components/history-comparison'
import type { ModelRadarConfiguration, ModelRadarHistoryFrame } from '../types'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string) => key,
  }),
}))

vi.mock('@/lib/use-chart-theme', () => ({
  useChartTheme: () => ({ resolvedTheme: 'light', themeReady: true }),
}))

vi.mock('@/lib/vchart', () => ({ VCHART_OPTION: {} }))
vi.mock('@/hooks', () => ({ useMediaQuery: () => false }))
vi.mock('@visactor/react-vchart', () => ({
  VChart: (props: {
    spec: {
      data?: Array<{
        id: string
        values: Array<Record<string, unknown>>
      }>
    }
  }) => (
    <div
      data-testid={props.spec.data?.[0]?.id}
      data-values={JSON.stringify(props.spec.data?.[0]?.values ?? [])}
    />
  ),
}))

const configurations: ModelRadarConfiguration[] = [
  {
    model: 'model-a',
    effort: 'low',
    iq: 80,
    passed: 8,
    valid_tasks: 15,
    average_price_usd: 1,
    price_samples: 15,
    average_minutes: 5,
    duration_samples: 15,
    incomplete_cost_samples: 0,
    total_runs: 20,
    latest_graded_at: 1_800_000_000,
    average_agent_steps: 10,
    agent_steps_samples: 15,
    average_total_tokens: 1_000,
    token_samples: 15,
    cache_hit_rate: 0.5,
    cache_token_samples: 15,
    combined_cost_index: 20,
  },
  {
    model: 'model-b',
    effort: 'high',
    iq: 100,
    passed: 10,
    valid_tasks: 15,
    average_price_usd: null,
    price_samples: null,
    average_minutes: 8,
    duration_samples: 15,
    incomplete_cost_samples: null,
    total_runs: 20,
    latest_graded_at: null,
    average_agent_steps: null,
    agent_steps_samples: null,
    average_total_tokens: null,
    token_samples: null,
    cache_hit_rate: null,
    cache_token_samples: null,
    combined_cost_index: 40,
  },
]

const history: ModelRadarHistoryFrame[] = [
  {
    ts: 1_799_996_400,
    points: configurations.map((configuration) => ({
      model: configuration.model,
      effort: configuration.effort,
      iq: configuration.iq,
      passed: configuration.passed,
      valid_tasks: configuration.valid_tasks,
      average_price_usd: configuration.average_price_usd,
      average_minutes: configuration.average_minutes,
      average_agent_steps: configuration.average_agent_steps,
      average_total_tokens: configuration.average_total_tokens,
      cache_hit_rate: configuration.cache_hit_rate,
    })),
  },
]

describe('model radar chart controls', () => {
  test('switches scatter metrics and omits configurations with missing values', async () => {
    const user = userEvent.setup()
    render(<EfficiencyScatter configurations={configurations} />)

    expect(screen.getByTestId('model-radar-scatter').dataset.values).toContain(
      'model-b'
    )
    await user.click(screen.getByRole('button', { name: 'Cost' }))

    const values = screen.getByTestId('model-radar-scatter').dataset.values
    expect(values).toContain('model-a')
    expect(values).not.toContain('model-b')
  })

  test('renders an empty comparison until checkboxes select configurations', async () => {
    const user = userEvent.setup()
    const view = render(
      <HistoryComparison configurations={configurations} history={history} />
    )

    expect(
      screen.getByRole('status', {
        name: 'Select at least one configuration to compare history.',
      })
    ).toBeVisible()
    const fieldset = view.container.querySelector('fieldset')
    expect(fieldset).toHaveClass('max-h-44', 'overflow-y-auto')

    await user.click(screen.getByRole('checkbox', { name: /model-a low/ }))

    expect(screen.getByTestId('model-radar-comparison')).toBeVisible()
    expect(
      screen.queryByText(
        'Select at least one configuration to compare history.'
      )
    ).toBeNull()
  })
})
