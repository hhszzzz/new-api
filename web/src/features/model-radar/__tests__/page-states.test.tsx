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
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { ModelRadar } from '../index'
import type {
  ModelRadarBenchCell,
  ModelRadarBenchDetailMetrics,
  ModelRadarResponse,
} from '../types'

const queryMocks = vi.hoisted(() => ({
  useQuery: vi.fn(),
  useMutation: vi.fn(() => ({ isPending: false, mutateAsync: vi.fn() })),
  useQueryClient: vi.fn(() => ({ invalidateQueries: vi.fn() })),
}))

vi.mock('@tanstack/react-query', () => ({
  useQuery: queryMocks.useQuery,
  useMutation: queryMocks.useMutation,
  useQueryClient: queryMocks.useQueryClient,
}))
vi.mock('axios', async (importOriginal) => {
  const actual = await importOriginal<typeof import('axios')>()
  return {
    ...actual,
    isAxiosError: (error: { response?: unknown }) => Boolean(error.response),
  }
})
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, string | number | null>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      ),
  }),
}))
vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: ReactNode }) => props.children,
}))
vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: ReactNode }) => props.children,
}))
vi.mock('../hooks/use-radar-formatters', () => ({
  useRadarFormatters: () => ({
    compact: (value: number | null) => String(value),
    dateTime: (value: number | null) => `time:${value}`,
    decimal: (value: number | null) => String(value),
    integer: (value: number | null) => String(value),
    percent: (value: number | null) => String(value),
    usd: (value: number | null) => String(value),
    utcDay: (value: number | null) => `day:${value}`,
  }),
}))
vi.mock('@/hooks', () => ({ useMediaQuery: () => false }))
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (iconName: string) => <svg data-icon-key={iconName} />,
}))

const detail: ModelRadarBenchDetailMetrics = {
  valid_grade_events: 8,
  latest_graded_at: 1_800_000_000,
  as_of: 1_800_001_000,
  graded_24h: 1,
  graded_48h: 2,
  metrics_state: 'available',
  metrics: {},
}

function cell(effort: string): ModelRadarBenchCell {
  return {
    model: 'gpt-test',
    effort,
    score: 100,
    coverage: 64,
    required_tasks: 64,
    score_status: 'complete',
    graded_runs_count: 64,
    source_scores: {
      deepswe: {
        score: 100,
        coverage: 15,
        required_tasks: 15,
        graded_runs_count: 15,
      },
    },
    price: { mean: 1, known_samples: 64, total_samples: 64 },
    duration: { mean: 5, known_samples: 64, total_samples: 64 },
    statistics_state: 'available',
    scoring_policy_version: 'radar-bench-v2-last3-per-task-equal-weight-150',
    detail_metrics: detail,
    history_state: 'available',
    history: [],
    last_graded_day: 1_799_000_000,
    read_at: 1_800_001_000,
  }
}

function response(
  stale: boolean,
  cells: ModelRadarBenchCell[] = [cell('low'), cell('high')]
): ModelRadarResponse {
  return {
    success: true,
    message: '',
    data: {
      schema_version: 2,
      fetched_at: 1_800_001_000,
      source_updated_at: 1_799_999_000,
      stale,
      source: {
        name: 'Codex Radar',
        url: 'https://codexradar.com',
        attribution: 'Data from Codex Radar',
      },
      model_count: cells.length > 0 ? 1 : 0,
      configuration_count: cells.length,
      bench:
        cells.length > 0
          ? {
              benchmark: 'radar-bench-test',
              catalog_version: 'catalog-test',
              task_set_sha256: 'a'.repeat(64),
              score_version: 'radar-bench-v2-150',
              score_max: 150,
              source_counts: {
                deepswe: 15,
                pompeii: 16,
                tb4: 14,
                science: 19,
              },
              read_at: 1_800_001_000,
              loaded_count: cells.length,
              total_count: cells.length,
              failed_cells: [],
              cells,
            }
          : null,
    },
  }
}

function renderPage() {
  return render(<ModelRadar />)
}

function queryResult(data: ModelRadarResponse | undefined) {
  return {
    data,
    error: null,
    isError: false,
    isFetched: true,
    isFetching: false,
    isLoading: false,
    refetch: vi.fn(),
  }
}

beforeEach(() => {
  queryMocks.useQuery.mockReset()
  queryMocks.useMutation.mockReset()
  queryMocks.useMutation.mockReturnValue({
    isPending: false,
    mutateAsync: vi.fn(),
  })
  queryMocks.useQueryClient.mockReset()
  queryMocks.useQueryClient.mockReturnValue({ invalidateQueries: vi.fn() })
})

describe('model radar page states', () => {
  test('shows the loading skeleton during the initial fetch', () => {
    queryMocks.useQuery.mockReturnValue({
      data: undefined,
      error: null,
      isError: false,
      isFetched: false,
      isFetching: true,
      isLoading: true,
      refetch: vi.fn(),
    })
    renderPage()

    expect(
      screen.getByRole('status', { name: 'Loading model radar' })
    ).toBeVisible()
    expect(screen.queryByRole('link')).toBeNull()
  })

  test('distinguishes the first-sync 503 state from a general load failure', () => {
    queryMocks.useQuery.mockReturnValue({
      data: undefined,
      error: Object.assign(new Error('unavailable'), {
        response: { status: 503 },
      }),
      isError: true,
      isFetched: true,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    })
    renderPage()

    expect(
      screen.getByRole('alert', {
        name: 'Model radar data is not available yet',
      })
    ).toBeVisible()
    expect(
      screen.getByText('The first upstream synchronization has not completed.')
    ).toBeVisible()
  })

  test('shows an empty state when the snapshot has no bench cells', () => {
    queryMocks.useQuery.mockReturnValue(queryResult(response(false, [])))
    renderPage()

    expect(
      screen.getByRole('status', { name: 'No model radar data' })
    ).toBeVisible()
  })

  test('renders bench rows, counts and the source link', () => {
    queryMocks.useQuery.mockReturnValue(queryResult(response(false)))
    renderPage()

    expect(
      screen.getByRole('region', { name: 'RadarBench score' })
    ).toBeVisible()
    expect(screen.getByRole('region', { name: 'GPT Test' })).toBeVisible()
    expect(screen.getByText(/1 models, 2 configurations/)).toBeVisible()
    expect(screen.getByText(/Updated time:1799999000/)).toBeVisible()
    for (const link of screen.getAllByRole('link', {
      name: 'Data from Codex Radar codexradar.com',
    })) {
      expect(link).toHaveAttribute('href', 'https://codexradar.com')
    }
    expect(screen.queryByText(/recommend/i)).toBeNull()
  })

  test('shows stale metadata without legacy alert content', () => {
    queryMocks.useQuery.mockReturnValue(queryResult(response(true)))
    renderPage()

    expect(screen.getByText('This data is outdated')).toBeVisible()
    expect(
      screen.getByText(
        'The last source update was time:1799999000. Showing the latest valid data.'
      )
    ).toBeVisible()
    expect(screen.getByText('Stale data')).toBeVisible()
    expect(screen.queryByText(/Degradation alerts/)).toBeNull()
    expect(screen.queryByText(/recommend/i)).toBeNull()
  })

  test('renders the model icon variant configured for the matching pricing vendor', () => {
    const radarResponse = response(false)
    queryMocks.useQuery.mockImplementation(
      (query?: { queryKey: readonly string[] }) => {
        if (query?.queryKey[0] === 'pricing') {
          return queryResult({
            success: true,
            data: [
              {
                id: 1,
                model_name: 'gpt-test',
                icon: 'OpenAI',
                vendor_id: 1,
                quota_type: 0,
                model_ratio: 1,
                completion_ratio: 1,
                enable_groups: ['default'],
              },
            ],
            vendors: [{ id: 1, name: 'OpenAI', icon: 'OpenAI.Color' }],
            group_ratio: {},
            usable_group: {},
            supported_endpoint: {},
            auto_groups: [],
          } as never)
        }
        return queryResult(radarResponse)
      }
    )

    const view = renderPage()

    expect(
      view.container.querySelector('[data-icon-key="OpenAI.Color"]')
    ).not.toBeNull()
  })
})
