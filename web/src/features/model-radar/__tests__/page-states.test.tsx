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
import type { ModelRadarFourTaskCell, ModelRadarResponse } from '../types'

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
    dateTime: (value: number | null) => `time:${value}`,
    decimal: (value: number | null) => String(value),
    integer: (value: number | null) => String(value),
    usd: (value: number | null) => String(value),
  }),
}))
vi.mock('@/hooks', () => ({ useMediaQuery: () => false }))
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (iconName: string) => <svg data-icon-key={iconName} />,
}))

function cell(effort: string): ModelRadarFourTaskCell {
  return {
    model: 'gpt-test',
    effort,
    score: 100,
    graded_tasks: 4,
    required_tasks: 4,
    rank: 1,
    components: {
      combined: {
        score: 100,
        weight: 60,
        contribution: 60,
        cost_usd: 1,
        duration_minutes: 10,
        state: 'graded',
      },
      pompeii: {
        score: 100,
        weight: 45,
        contribution: 45,
        cost_usd: 1,
        duration_minutes: 10,
        state: 'graded',
      },
      pelican: {
        score: 100,
        weight: 22.5,
        contribution: 22.5,
        cost_usd: 1,
        duration_minutes: 10,
        state: 'graded',
      },
      candy: {
        score: 100,
        weight: 22.5,
        contribution: 22.5,
        cost_usd: 1,
        duration_minutes: 10,
        state: 'graded',
      },
    },
    cost_usd: 4,
    duration_minutes: 40,
  }
}

function response(
  stale: boolean,
  cells: ModelRadarFourTaskCell[] = [cell('low'), cell('high')]
): ModelRadarResponse {
  return {
    success: true,
    message: '',
    data: {
      schema_version: 3,
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
      four_task:
        cells.length > 0
          ? {
              protocol: 'codex-four-weighted-150-v2',
              round_id: 'round-test',
              score_max: 150,
              source_sha256: 'a'.repeat(64),
              publication_status: 'authorized_partial',
              verified_at: 1_799_999_000,
              test_started_at: 1_799_000_000,
              test_completed_at: 1_800_000_000,
              graded: cells.length * 4,
              expected: cells.length * 4,
              ungraded: 0,
              valid_totals: cells.length,
              task_weights: {
                combined: 60,
                pompeii: 45,
                pelican: 22.5,
                candy: 22.5,
              },
              model_names: { 'gpt-test': 'GPT Test' },
              model_order: ['gpt-test'],
              read_at: 1_800_001_000,
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

  test('shows an empty state when the snapshot has no four-task configurations', () => {
    queryMocks.useQuery.mockReturnValue(queryResult(response(false, [])))
    renderPage()

    expect(
      screen.getByRole('status', { name: 'No model radar data' })
    ).toBeVisible()
  })

  test('renders four-task rows, counts and the source link', () => {
    queryMocks.useQuery.mockReturnValue(queryResult(response(false)))
    renderPage()

    expect(
      screen.getByRole('region', { name: 'Four-task score' })
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
