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

import { CapabilityMatrix } from '../components/capability-matrix'
import type { ModelRadarConfiguration } from '../types'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, string | number>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      ),
  }),
}))

const fixture: ModelRadarConfiguration = {
  model: 'gpt-radar',
  effort: 'medium',
  iq: 93.75,
  passed: 7,
  valid_tasks: 10,
  average_price_usd: 1.25,
  price_samples: 10,
  average_minutes: 4.5,
  duration_samples: 9,
  incomplete_cost_samples: 1,
  total_runs: 12,
  latest_graded_at: 1_800_000_000,
  average_agent_steps: 22,
  agent_steps_samples: 8,
  average_total_tokens: 12_345,
  token_samples: 7,
  cache_hit_rate: 0.75,
  cache_token_samples: 6,
  combined_cost_index: 45,
}

describe('model radar capability matrix', () => {
  test('uses two, three, and six responsive columns for the matrix', () => {
    render(<CapabilityMatrix configurations={[fixture]} />)

    const detailsButton = screen.getByRole('button', {
      name: 'View details for gpt-radar medium',
    })
    expect(detailsButton.parentElement).toHaveClass(
      'grid-cols-2',
      'sm:grid-cols-3',
      'lg:grid-cols-6'
    )
  })

  test('opens complete metrics from the keyboard and restores focus after Escape', async () => {
    const user = userEvent.setup()
    render(<CapabilityMatrix configurations={[fixture]} />)
    const detailsButton = screen.getByRole('button', {
      name: 'View details for gpt-radar medium',
    })

    detailsButton.focus()
    await user.keyboard('{Enter}')

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('gpt-radar medium')
    expect(screen.getByText('Combined cost index')).toBeVisible()
    expect(screen.getByText('45')).toBeVisible()
    expect(screen.getByText('Cost samples')).toBeVisible()
    expect(
      screen.getByText(
        'IQ is the latest valid pass rate per task multiplied by 150. The combined cost index is provided by the source after normalizing weighted price and duration to 100.'
      )
    ).toBeVisible()

    await user.keyboard('{Escape}')

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(detailsButton).toHaveFocus()
  })
})
