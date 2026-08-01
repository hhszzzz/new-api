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

import { CapabilityMatrix, ModelBadge } from '../components/capability-matrix'
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
vi.mock('@/hooks', () => ({ useMediaQuery: () => true }))
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (iconName: string) => <svg data-icon-key={iconName} />,
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
  test('renders a complete vendor badge without clipping it', () => {
    const { container } = render(<ModelBadge color='#2563eb' model='gpt-5.4' />)
    const wrapper = container.firstElementChild

    expect(wrapper).toHaveClass('size-6')
    expect(wrapper).not.toHaveClass('overflow-hidden')
    expect(wrapper?.querySelector('svg')).not.toBeNull()
  })

  test('renders the vendor configured icon variant instead of the radar fallback', () => {
    const { container } = render(
      <CapabilityMatrix
        configurations={[{ ...fixture, model: 'deepseek-v3.2' }]}
        iconRegistry={{
          modelIcons: new Map<string, string>(),
          providerIcons: new Map([['deepseek', 'DeepSeek.Color']]),
        }}
      />
    )

    expect(
      container.querySelector('[data-icon-key="DeepSeek.Color"]')
    ).not.toBeNull()
  })

  test('lays out the matrix as a table with one row per model', () => {
    render(<CapabilityMatrix configurations={[fixture]} />)

    const table = screen.getByRole('table')
    expect(table).toBeVisible()
    expect(screen.getByRole('rowheader', { name: /gpt-radar/ })).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'View details for gpt-radar medium' })
    ).toBeVisible()
  })

  test('keeps the pinned model column opaque and above scrolling score cells', () => {
    render(<CapabilityMatrix configurations={[fixture]} />)

    expect(screen.getByRole('columnheader', { name: 'Model' })).toHaveClass(
      'sticky',
      'left-0',
      'z-20',
      'bg-card'
    )
    expect(screen.getByRole('rowheader', { name: /gpt-radar/ })).toHaveClass(
      'sticky',
      'left-0',
      'z-10',
      'bg-card'
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
    expect(
      screen.getByText(
        (_, element) => element?.textContent === 'Cost samples: 10'
      )
    ).toBeVisible()
    expect(
      screen.getByText(
        'IQ is the latest valid pass rate per task multiplied by 150. The combined cost index is provided by the source after normalizing weighted price and duration to 100.'
      )
    ).toBeVisible()

    await user.keyboard('{Escape}')

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(detailsButton).toHaveFocus()
  })

  test('keeps an open detail dialog synchronized with refreshed configuration data', async () => {
    const user = userEvent.setup()
    const view = render(<CapabilityMatrix configurations={[fixture]} />)

    await user.click(
      screen.getByRole('button', {
        name: 'View details for gpt-radar medium',
      })
    )
    expect(screen.getByRole('dialog')).toBeVisible()

    view.rerender(
      <CapabilityMatrix
        configurations={[{ ...fixture, iq: 81.25, combined_cost_index: 12 }]}
      />
    )

    expect(screen.getByRole('dialog')).toHaveTextContent('81.25')
    expect(screen.getByRole('dialog')).toHaveTextContent('12')
    expect(screen.getByRole('dialog')).not.toHaveTextContent('93.75')
  })

  test('closes an open detail dialog when its configuration disappears', async () => {
    const user = userEvent.setup()
    const view = render(<CapabilityMatrix configurations={[fixture]} />)

    await user.click(
      screen.getByRole('button', {
        name: 'View details for gpt-radar medium',
      })
    )
    expect(screen.getByRole('dialog')).toBeVisible()

    view.rerender(<CapabilityMatrix configurations={[]} />)

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  test('adds a column when the source introduces a new reasoning effort', () => {
    render(
      <CapabilityMatrix
        configurations={[fixture, { ...fixture, effort: 'turbo', iq: 95 }]}
      />
    )

    expect(screen.getByRole('columnheader', { name: 'turbo' })).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'View details for gpt-radar turbo' })
    ).toBeVisible()
  })
})
