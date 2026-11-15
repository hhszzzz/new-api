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

import { FourTaskPanel } from '../components/four-task-panel'
import type {
  ModelRadarFourTask,
  ModelRadarFourTaskCell,
  ModelRadarFourTaskComponent,
} from '../types'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, string | number>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      ),
  }),
}))
vi.mock('../hooks/use-radar-formatters', () => ({
  useRadarFormatters: () => ({
    dateTime: (value: number | null) => `time:${value}`,
    decimal: (value: number | null) => String(value),
    integer: (value: number | null) => String(value),
    usd: (value: number | null) => `$${value}`,
  }),
}))
vi.mock('@/hooks', () => ({ useMediaQuery: () => true }))
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (iconName: string) => <svg data-icon-key={iconName} />,
}))

function componentFixture(
  score: number | null,
  weight: number,
  overrides: Partial<ModelRadarFourTaskComponent> = {}
): ModelRadarFourTaskComponent {
  return {
    score,
    weight,
    contribution: score === null ? null : (score * weight) / 100,
    cost_usd: score === null ? null : 1,
    duration_minutes: score === null ? null : 10,
    state: score === null ? 'needs_review' : 'graded',
    ...overrides,
  }
}

function cellFixture(
  effort: string,
  overrides: Partial<ModelRadarFourTaskCell> = {}
): ModelRadarFourTaskCell {
  return {
    model: 'gpt-test-astra',
    effort,
    score: 121.1,
    graded_tasks: 4,
    required_tasks: 4,
    rank: 2,
    components: {
      combined: componentFixture(80, 60),
      pompeii: componentFixture(90, 45),
      pelican: componentFixture(70, 22.5),
      candy: componentFixture(100, 22.5),
    },
    cost_usd: 4,
    duration_minutes: 40,
    ...overrides,
  }
}

function boardFixture(
  overrides: Partial<ModelRadarFourTask> = {}
): ModelRadarFourTask {
  return {
    protocol: 'codex-four-weighted-150-v2',
    round_id: 'round-test',
    score_max: 150,
    source_sha256: 'a'.repeat(64),
    publication_status: 'authorized_partial',
    verified_at: 1_800_000_000,
    test_started_at: 1_799_000_000,
    test_completed_at: 1_800_000_000,
    graded: 7,
    expected: 8,
    ungraded: 1,
    valid_totals: 1,
    task_weights: {
      combined: 60,
      pompeii: 45,
      pelican: 22.5,
      candy: 22.5,
    },
    model_names: { 'gpt-test-astra': 'GPT Test Astra' },
    model_order: ['gpt-test-astra'],
    read_at: 1_800_000_000,
    cells: [cellFixture('xhigh'), cellFixture('low', { score: 95.2 })],
    ...overrides,
  }
}

describe('model radar four-task panel', () => {
  test('renders each tier with its score, graded tasks, status, cost and runtime', () => {
    render(<FourTaskPanel board={boardFixture()} />)

    const row = screen.getByRole('region', { name: 'GPT Test Astra' })
    expect(within(row).getByText('121.1')).toBeVisible()
    expect(within(row).getAllByText('4/4')).toHaveLength(2)
    expect(within(row).getAllByText('Complete')).toHaveLength(2)
    expect(within(row).getAllByText('$4')).toHaveLength(2)
    expect(within(row).getAllByText('40 min')).toHaveLength(2)
  })

  test('switches the score dimension to the four components', async () => {
    const user = userEvent.setup()
    render(<FourTaskPanel board={boardFixture()} />)

    const total = screen.getByRole('button', { name: 'Total score' })
    const qin = screen.getByRole('button', { name: 'Qin Shi Huang' })
    expect(total).toHaveAttribute('aria-pressed', 'true')

    await user.click(qin)

    expect(qin).toHaveAttribute('aria-pressed', 'true')
    expect(total).toHaveAttribute('aria-pressed', 'false')
    const row = screen.getByRole('region', { name: 'GPT Test Astra' })
    expect(within(row).getAllByText('80')).toHaveLength(2)
    expect(within(row).getAllByText('1/1')).toHaveLength(2)
    expect(within(row).queryByText('$4')).toBeNull()
  })

  test('reports the published round counts and test time', () => {
    render(<FourTaskPanel board={boardFixture()} />)

    expect(screen.getByText('7/8 graded · 1 totals · 1 missing')).toBeVisible()
    expect(screen.getByText('Tested time:1800000000')).toBeVisible()
  })

  test('opens the tier detail dialog with components, observed metrics and round metadata', async () => {
    const user = userEvent.setup()
    render(<FourTaskPanel board={boardFixture()} />)

    await user.click(
      screen.getByRole('button', {
        name: 'View details for gpt-test-astra low',
      })
    )

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Components')).toBeVisible()
    expect(within(dialog).getByText('Qin Shi Huang')).toBeVisible()
    expect(within(dialog).getByText(/Weight: 60/)).toBeVisible()
    expect(within(dialog).getByText('Cost (USD)')).toBeVisible()
    expect(within(dialog).getByText('Runtime (min)')).toBeVisible()
    expect(within(dialog).getByText('round-test')).toBeVisible()
    expect(within(dialog).getByText('codex-four-weighted-150-v2')).toBeVisible()
    expect(
      within(dialog).getByText(
        'Weights: Qin Shi Huang 60, Pompeii 45, Pelican 22.5, Candy 22.5.'
      )
    ).toBeVisible()
  })

  test('marks a configuration with a missing component', async () => {
    const user = userEvent.setup()
    const missing = cellFixture('low', {
      score: null,
      graded_tasks: 3,
      rank: null,
      cost_usd: null,
      duration_minutes: null,
      components: {
        combined: componentFixture(null, 60, {
          missing_reason: 'required_svg_not_delivered',
        }),
        pompeii: componentFixture(50, 45),
        pelican: componentFixture(60, 22.5),
        candy: componentFixture(0, 22.5),
      },
    })
    render(
      <FourTaskPanel
        board={boardFixture({ cells: [cellFixture('xhigh'), missing] })}
      />
    )

    const row = screen.getByRole('region', { name: 'GPT Test Astra' })
    expect(within(row).getAllByText('Missing').length).toBeGreaterThan(0)
    expect(within(row).getByTitle(/required_svg_not_delivered/)).toBeVisible()

    await user.click(
      screen.getByRole('button', {
        name: 'View details for gpt-test-astra low',
      })
    )

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getAllByText('—').length).toBeGreaterThan(0)
    expect(
      within(dialog).getAllByText('Not published by this source').length
    ).toBeGreaterThan(0)
  })

  test('renders the per-model auto-effort switch and previews the selected tier', async () => {
    const user = userEvent.setup()
    const onToggle = vi.fn()
    render(
      <FourTaskPanel
        board={boardFixture()}
        settings={{
          auto_effort_enabled: true,
          models: { 'gpt-test-astra': { auto_effort: true } },
        }}
        autoEffort={{
          setting: {
            policy: 'highest_iq',
            min_iq_delta: 5,
            models: { 'gpt-test-astra': { enabled: true } },
          },
          isSaving: false,
          userModels: ['gpt-test-astra'],
          onToggle,
        }}
      />
    )

    const toggle = screen.getByRole('switch', {
      name: 'Automatically choose the reasoning tier for GPT Test Astra',
    })
    expect(toggle).toBeChecked()
    await user.click(toggle)
    expect(onToggle).toHaveBeenCalledWith('gpt-test-astra', false)

    const row = screen.getByRole('region', { name: 'GPT Test Astra' })
    expect(within(row).getByText('Current')).toBeVisible()
  })
})
