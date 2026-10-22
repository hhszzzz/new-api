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

import { BenchPanel } from '../components/bench-panel'
import type { ModelRadarBench, ModelRadarBenchCell } from '../types'

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
    compact: (value: number | null) => String(value),
    dateTime: (value: number | null) => `time:${value}`,
    decimal: (value: number | null) => String(value),
    historyTime: (value: number) => String(value),
    integer: (value: number | null) => String(value),
    percent: (value: number | null) => String(value),
    usd: (value: number | null) => `$${value}`,
    utcDay: (value: number | null) => `day:${value}`,
  }),
}))
vi.mock('@/hooks', () => ({ useMediaQuery: () => true }))
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (iconName: string) => <svg data-icon-key={iconName} />,
}))

function cellFixture(
  effort: string,
  overrides: Partial<ModelRadarBenchCell> = {}
): ModelRadarBenchCell {
  return {
    model: 'gpt-test-astra',
    effort,
    score: 121.1,
    coverage: 62,
    required_tasks: 64,
    score_status: 'provisional',
    graded_runs_count: 62,
    source_scores: {
      deepswe: {
        score: 116.6,
        coverage: 15,
        required_tasks: 15,
        graded_runs_count: 15,
      },
      pompeii: {
        score: 120.4,
        coverage: 12,
        required_tasks: 16,
        graded_runs_count: 12,
      },
      tb4: {
        score: null,
        coverage: 0,
        required_tasks: 14,
        graded_runs_count: null,
      },
      science: {
        score: 75,
        coverage: 16,
        required_tasks: 19,
        graded_runs_count: 16,
      },
    },
    price: { mean: 3.347, known_samples: 62, total_samples: 62 },
    duration: { mean: 13.2, known_samples: 62, total_samples: 62 },
    statistics_state: 'available',
    scoring_policy_version: 'radar-bench-v2-last3-per-task-equal-weight-150',
    detail_metrics: {
      valid_grade_events: 64,
      latest_graded_at: 1_791_849_600,
      as_of: 1_800_000_000,
      graded_24h: 2,
      graded_48h: 5,
      metrics_state: 'available',
      metrics: {
        total_tokens: { mean: 1000, known_samples: 64, total_samples: 64 },
        input_tokens: { mean: 800, known_samples: 64, total_samples: 64 },
        output_tokens: { mean: 200, known_samples: 64, total_samples: 64 },
        cache_tokens: { mean: 600, known_samples: 64, total_samples: 64 },
        reasoning_tokens: { mean: 50, known_samples: 64, total_samples: 64 },
        cache_write_tokens: { mean: 0, known_samples: 64, total_samples: 64 },
        turn_count: { mean: 3, known_samples: 64, total_samples: 64 },
      },
    },
    history_state: 'available',
    history: [
      {
        ts: 1_791_849_600,
        score: 121.1,
        coverage: 62,
        score_status: 'provisional',
      },
    ],
    last_graded_day: 1_791_849_600,
    read_at: 1_800_000_000,
    ...overrides,
  }
}

function benchFixture(
  overrides: Partial<ModelRadarBench> = {}
): ModelRadarBench {
  return {
    benchmark: 'radar-bench-test',
    catalog_version: 'catalog-test',
    task_set_sha256: 'a'.repeat(64),
    score_version: 'radar-bench-v2-150',
    score_max: 150,
    source_counts: { deepswe: 15, pompeii: 16, tb4: 14, science: 19 },
    read_at: 1_800_000_000,
    loaded_count: 2,
    total_count: 2,
    failed_cells: [],
    cells: [cellFixture('xhigh'), cellFixture('low', { score: 95.2 })],
    ...overrides,
  }
}

describe('model radar bench panel', () => {
  test('renders each tier with its score, coverage, status and observed cost', () => {
    render(<BenchPanel bench={benchFixture()} />)

    const row = screen.getByRole('region', { name: 'GPT Test Astra' })
    expect(within(row).getByText('121.1')).toBeVisible()
    expect(within(row).getAllByText('62/64')).toHaveLength(2)
    expect(within(row).getAllByText('Provisional')).toHaveLength(2)
    expect(within(row).getAllByText('$3.347')).toHaveLength(2)
    expect(screen.queryByText(/Read at/)).toBeNull()
  })

  test('switches the score dimension to the per-library scores', async () => {
    const user = userEvent.setup()
    render(<BenchPanel bench={benchFixture()} />)

    const total = screen.getByRole('button', { name: 'Total' })
    const software = screen.getByRole('button', {
      name: 'Software engineering',
    })
    expect(total).toHaveAttribute('aria-pressed', 'true')

    await user.click(software)

    expect(software).toHaveAttribute('aria-pressed', 'true')
    expect(total).toHaveAttribute('aria-pressed', 'false')
    const row = screen.getByRole('region', { name: 'GPT Test Astra' })
    expect(within(row).getAllByText('116.6')).toHaveLength(2)
    expect(within(row).getAllByText('15/15')).toHaveLength(2)
    expect(within(row).queryByText('$3.35')).toBeNull()
  })

  test('reports a partial round and marks a tier that kept its previous read', () => {
    const carried = cellFixture('low', { read_at: 1_700_000_000, score: 95.2 })
    render(
      <BenchPanel
        bench={benchFixture({
          read_at: 1_700_000_000,
          loaded_count: 1,
          total_count: 2,
          failed_cells: ['gpt-test-astra|low'],
          cells: [cellFixture('xhigh'), carried],
        })}
      />
    )

    expect(
      screen.getByText(
        'This round read 1 of 2 configurations; failed tiers keep their previous values.'
      )
    ).toBeVisible()
    expect(
      screen.getByTitle(/Keeping the previous read from time:1700000000/)
    ).toBeVisible()
  })

  test('opens the tier detail dialog with grading activity and token usage', async () => {
    const user = userEvent.setup()
    render(<BenchPanel bench={benchFixture()} />)

    await user.click(
      screen.getByRole('button', {
        name: 'View details for gpt-test-astra low',
      })
    )

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Grading activity')).toBeVisible()
    expect(within(dialog).getByText('Valid grading events')).toBeVisible()
    expect(within(dialog).getByText('Formal grades in 24h')).toBeVisible()
    expect(within(dialog).getByText('Formal grades in 48h')).toBeVisible()
    expect(within(dialog).getByText('Mean total tokens')).toBeVisible()
    expect(within(dialog).getByText('Mean cached input tokens')).toBeVisible()
    expect(within(dialog).getByText('Cache hit rate')).toBeVisible()
    expect(
      within(dialog).getByText(/radar-bench-v2-last3-per-task-equal-weight-150/)
    ).toBeVisible()
  })

  test('explains when a snapshot predates the grading detail', async () => {
    const user = userEvent.setup()
    render(
      <BenchPanel
        bench={benchFixture({
          cells: [
            cellFixture('xhigh', { detail_metrics: null }),
            cellFixture('low', { score: 95.2 }),
          ],
        })}
      />
    )

    await user.click(
      screen.getByRole('button', {
        name: 'View details for gpt-test-astra xhigh',
      })
    )

    const dialog = await screen.findByRole('dialog')
    expect(
      within(dialog).getByText(
        'This snapshot was published before grading details were added.'
      )
    ).toBeVisible()
  })

  test('renders the per-model auto-effort switch and previews the selected tier', async () => {
    const user = userEvent.setup()
    const onToggle = vi.fn()
    render(
      <BenchPanel
        bench={benchFixture()}
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
