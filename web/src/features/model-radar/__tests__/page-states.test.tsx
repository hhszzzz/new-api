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
import type { ModelRadarResponse } from '../types'

const queryMocks = vi.hoisted(() => ({ useQuery: vi.fn() }))

vi.mock('@tanstack/react-query', () => ({
  useQuery: queryMocks.useQuery,
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
vi.mock('@/lib/use-chart-theme', () => ({
  useChartTheme: () => ({ resolvedTheme: 'light', themeReady: false }),
}))
vi.mock('@/hooks', () => ({ useMediaQuery: () => false }))
vi.mock('@visactor/react-vchart', () => ({
  VChart: () => <div data-testid='radar-chart' />,
}))

function response(stale: boolean): ModelRadarResponse {
  return {
    success: true,
    message: '',
    data: {
      schema_version: 1,
      fetched_at: 1_800_000_000,
      source_updated_at: 1_799_999_000,
      alerts_updated_at: 1_799_999_000,
      stale,
      source: {
        name: 'Codex Radar',
        url: 'https://codexradar.com',
        attribution: 'Data from Codex Radar',
      },
      model_count: 1,
      configuration_count: 1,
      configurations: [
        {
          model: 'model-a',
          effort: 'low',
          iq: 75,
          passed: 1,
          valid_tasks: 2,
          average_price_usd: null,
          price_samples: null,
          average_minutes: null,
          duration_samples: null,
          incomplete_cost_samples: null,
          total_runs: null,
          latest_graded_at: null,
          average_agent_steps: null,
          agent_steps_samples: null,
          average_total_tokens: null,
          token_samples: null,
          cache_hit_rate: null,
          cache_token_samples: null,
          combined_cost_index: null,
        },
      ],
      history: [],
      degradation_alerts: [],
    },
  }
}

function renderPage() {
  return render(<ModelRadar />)
}

beforeEach(() => queryMocks.useQuery.mockReset())

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

  test('shows stale metadata, an empty-alert state, and no recommendation content', () => {
    queryMocks.useQuery.mockReturnValue({
      data: response(true),
      error: null,
      isError: false,
      isFetched: true,
      isFetching: false,
      isLoading: false,
      refetch: vi.fn(),
    })
    renderPage()

    expect(screen.getByText('This data is outdated')).toBeVisible()
    expect(screen.getByText('No degradation alerts')).toBeVisible()
    expect(screen.getByText('Stale data')).toBeVisible()
    expect(
      screen.getByRole('link', {
        name: 'Data from Codex Radar codexradar.com',
      })
    ).toHaveAttribute('href', 'https://codexradar.com')
    expect(screen.queryByText(/recommend/i)).toBeNull()
  })
})
