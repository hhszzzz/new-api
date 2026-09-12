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
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { ModelRadar } from '../index'
import { resolveRadarSettings } from '../lib/model-radar'
import type { ModelRadarResponse } from '../types'

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
vi.mock('@/lib/use-chart-theme', () => ({
  useChartTheme: () => ({ resolvedTheme: 'light', themeReady: false }),
}))
vi.mock('../hooks/use-radar-formatters', () => ({
  useRadarFormatters: () => ({
    compact: (value: number | null) => String(value),
    dateTime: (value: number | null) => `time:${value}`,
    decimal: (value: number | null) => String(value),
    historyTime: (value: number) => String(value),
    integer: (value: number | null) => String(value),
    percent: (value: number | null) => String(value),
    usd: (value: number | null) => String(value),
  }),
}))
vi.mock('@/hooks', () => ({ useMediaQuery: () => false }))
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (iconName: string) => <svg data-icon-key={iconName} />,
}))
vi.mock('@visactor/react-vchart', () => ({
  VChart: () => <div data-testid='radar-chart' />,
}))

function response(stale: boolean): ModelRadarResponse {
  return {
    success: true,
    message: '',
    data: {
      schema_version: 1,
      fetched_at: 1_800_001_000,
      source_updated_at: 1_799_999_000,
      alerts_updated_at: 1_800_000_100,
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
          model: 'gpt-a',
          effort: 'low',
          harness: 'codex',
          runs_24h: null,
          runs_48h: null,
          average_price_usd_by_band: null,
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
  test.each([
    { default_vendor: 'anthropic', selected: 'Anthropic 1' },
    { default_vendor: 'missing', selected: 'All 2' },
  ])(
    'honours configured default $default_vendor with an All fallback',
    ({ default_vendor, selected }) => {
      const data = response(false)
      data.data.configurations.push({
        ...data.data.configurations[0],
        model: 'claude-a',
      })
      data.data.settings = resolveRadarSettings({ default_vendor })
      queryMocks.useQuery.mockReturnValue({ data, isFetched: true })
      renderPage()
      expect(screen.getByRole('tab', { name: selected })).toHaveAttribute(
        'aria-selected',
        'true'
      )
    }
  )

  test('hides configured models from counts, cards and alerts, counting each model once across tiers', () => {
    const data = response(false)
    data.data.configurations.push(
      { ...data.data.configurations[0], effort: 'high' },
      { ...data.data.configurations[0], model: 'claude-a' },
      { ...data.data.configurations[0], model: 'k3' }
    )
    data.data.settings = resolveRadarSettings({
      default_vendor: 'all',
      models: { k3: { hidden: true } },
    })
    data.data.degradation_alerts = [
      {
        model: 'k3',
        effort: 'low',
        iq: 70,
        degradation_12h_iq: 1,
        degradation_24h_iq: 2,
        degradation_48h_iq: 3,
      },
    ]
    queryMocks.useQuery.mockReturnValue({ data, isFetched: true })
    renderPage()
    expect(screen.getByRole('tab', { name: 'All 2' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.getByRole('tab', { name: 'OpenAI 1' })).toBeVisible()
    expect(screen.queryByRole('tab', { name: /Moonshot/ })).toBeNull()
    expect(screen.getByText(/2 models, 3 configurations/)).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'kimi-k3' })).toBeNull()
    expect(
      screen.queryByRole('heading', { name: 'Degradation alerts' })
    ).toBeNull()
  })

  test('the alert switch hides nonempty alerts and hiding every model shows an empty state', () => {
    const data = response(false)
    data.data.settings = resolveRadarSettings({
      show_degradation_alerts: false,
    })
    data.data.degradation_alerts = [
      {
        model: 'gpt-a',
        effort: 'low',
        iq: 70,
        degradation_12h_iq: 1,
        degradation_24h_iq: 2,
        degradation_48h_iq: 3,
      },
    ]
    queryMocks.useQuery.mockReturnValue({ data, isFetched: true })
    const view = renderPage()
    expect(
      screen.queryByRole('heading', { name: 'Degradation alerts' })
    ).toBeNull()
    data.data.settings = resolveRadarSettings({
      models: { 'gpt-a': { hidden: true } },
    })
    view.rerender(<ModelRadar />)
    expect(screen.getByText(/0 models, 0 configurations/)).toBeVisible()
    expect(screen.getByText('No models for this vendor yet.')).toBeVisible()
  })
  test('defaults to OpenAI and switching vendors filters both model cards and degradation alerts', async () => {
    const user = userEvent.setup()
    const data = response(false)
    data.data.configurations.push({
      ...data.data.configurations[0],
      model: 'claude-a',
      harness: 'dsh',
    })
    data.data.degradation_alerts = data.data.configurations.map((item) => ({
      model: item.model,
      effort: item.effort,
      iq: item.iq,
      degradation_12h_iq: 1,
      degradation_24h_iq: 2,
      degradation_48h_iq: 3,
    }))
    queryMocks.useQuery.mockReturnValue({ data, isFetched: true })
    renderPage()
    expect(screen.getByRole('tab', { name: 'OpenAI 1' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.queryByRole('heading', { name: 'claude-a' })).toBeNull()
    await user.click(screen.getByRole('tab', { name: 'Anthropic 1' }))
    expect(screen.getByRole('tab', { name: 'Anthropic 1' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    const panel = screen.getByRole('tabpanel')
    expect(
      within(panel).getByRole('heading', { name: 'claude-a' })
    ).toBeVisible()
    expect(within(panel).queryByRole('heading', { name: 'gpt-a' })).toBeNull()
    expect(
      within(panel).getByRole('article', { name: 'claude-a low' })
    ).toBeVisible()
    expect(
      within(panel).queryByRole('article', { name: 'gpt-a low' })
    ).toBeNull()
  })

  test('refreshing away the selected vendor falls back to the default and does not reselect it when it returns', async () => {
    const user = userEvent.setup()
    const data = response(false)
    data.data.configurations.push({
      ...data.data.configurations[0],
      model: 'claude-a',
      harness: 'dsh',
    })
    queryMocks.useQuery.mockReturnValue({ data, isFetched: true })
    const view = renderPage()
    await user.click(screen.getByRole('tab', { name: 'Anthropic 1' }))
    queryMocks.useQuery.mockReturnValue({
      data: response(false),
      isFetched: true,
    })
    view.rerender(<ModelRadar />)
    expect(screen.queryByRole('tablist')).toBeNull()
    expect(screen.getByRole('heading', { name: 'gpt-a' })).toBeVisible()
    queryMocks.useQuery.mockReturnValue({ data, isFetched: true })
    view.rerender(<ModelRadar />)
    expect(screen.getByRole('tab', { name: 'OpenAI 1' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
  })

  test('shows an empty snapshot state when every vendor loses its configurations', async () => {
    const user = userEvent.setup()
    const data = response(false)
    data.data.configurations.push({
      ...data.data.configurations[0],
      model: 'claude-a',
      harness: 'dsh',
    })
    queryMocks.useQuery.mockReturnValue({ data, isFetched: true })
    const view = renderPage()
    await user.click(screen.getByRole('tab', { name: 'Anthropic 1' }))
    queryMocks.useQuery.mockReturnValue({
      data: { ...data, data: { ...data.data, configurations: [] } },
      isFetched: true,
    })
    view.rerender(<ModelRadar />)
    expect(
      screen.getByRole('status', { name: 'No model radar data' })
    ).toBeVisible()
    expect(screen.queryByRole('tablist')).toBeNull()
    expect(screen.queryByRole('button', { name: /View details/ })).toBeNull()
  })

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

  test('shows stale metadata without an empty alert section or recommendation content', () => {
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
    expect(
      screen.getByText(
        'The last source update was time:1800000100. Showing the latest valid data.'
      )
    ).toBeVisible()
    expect(
      screen.queryByRole('heading', { name: 'Degradation alerts' })
    ).toBeNull()
    expect(screen.getByText('Stale data')).toBeVisible()
    expect(screen.getByText(/Updated time:1799999000/)).toBeVisible()
    expect(screen.queryByText(/Updated time:1800001000/)).toBeNull()
    expect(
      screen.getByRole('link', {
        name: 'Data from Codex Radar codexradar.com',
      })
    ).toHaveAttribute('href', 'https://codexradar.com')
    expect(screen.queryByText(/recommend/i)).toBeNull()
  })

  test('renders the model icon variant configured for the matching pricing vendor', () => {
    const radarResponse = response(false)
    radarResponse.data.configurations[0].model = 'deepseek-v3.2'
    queryMocks.useQuery.mockImplementation(
      (query?: { queryKey: readonly string[] }) => {
        if (query?.queryKey[0] === 'pricing') {
          return {
            data: {
              success: true,
              data: [
                {
                  id: 1,
                  model_name: 'deepseek-v3.2',
                  icon: 'DeepSeek',
                  vendor_id: 1,
                  quota_type: 0,
                  model_ratio: 1,
                  completion_ratio: 1,
                  enable_groups: ['default'],
                },
              ],
              vendors: [{ id: 1, name: 'DeepSeek', icon: 'DeepSeek.Color' }],
              group_ratio: {},
              usable_group: {},
              supported_endpoint: {},
              auto_groups: [],
            },
            error: null,
            isError: false,
            isFetched: true,
            isFetching: false,
            isLoading: false,
            refetch: vi.fn(),
          }
        }
        return {
          data: radarResponse,
          error: null,
          isError: false,
          isFetched: true,
          isFetching: false,
          isLoading: false,
          refetch: vi.fn(),
        }
      }
    )

    const view = renderPage()

    expect(
      view.container.querySelector('[data-icon-key="DeepSeek.Color"]')
    ).not.toBeNull()
  })
})
