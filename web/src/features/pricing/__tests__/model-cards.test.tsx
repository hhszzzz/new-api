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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createJSONStorage } from 'zustand/middleware'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { CachedPriceCell } from '../components/cached-price-cell'
import { ModelCard } from '../components/model-card'
import { ModelCardGrid } from '../components/model-card-grid'
import { ModelPerfBadge } from '../components/model-perf-badge'
import { PricingTable } from '../components/pricing-table'
import type { PricingModel } from '../types'

function pricingModel(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    id: 1,
    model_name: 'example-model',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 3,
    enable_groups: ['default', 'premium'],
    group_ratio: { default: 1, premium: 3 },
    ...overrides,
  }
}

let queryClient: QueryClient
const originalStorage = useSystemConfigStore.persist.getOptions().storage
beforeEach(() => {
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
    key: (index: number) => [...storage.keys()][index] ?? null,
    get length() {
      return storage.size
    },
  })
  useSystemConfigStore.persist.setOptions({
    storage: createJSONStorage(() => localStorage),
  })
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
  })
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
})
afterEach(() => {
  queryClient.clear()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  vi.useRealTimers()
  vi.unstubAllGlobals()
  useSystemConfigStore.persist.setOptions({ storage: originalStorage })
})

describe('model cards', () => {
  it('reserves readable metric widths in table view', () => {
    render(
      <QueryClientProvider client={queryClient}>
        <PricingTable models={[pricingModel()]} />
      </QueryClientProvider>
    )
    expect(
      screen.getByLabelText('Performance metrics for the last 24 hours')
    ).toHaveClass('min-w-[295px]')
  })
  it('reserves the same columns for missing and populated performance values', () => {
    const { rerender } = render(<ModelPerfBadge perf={undefined} />)
    const metrics = screen.getByLabelText(
      'Performance metrics for the last 24 hours'
    )
    expect(metrics.querySelector('dl')).toHaveClass(
      'flex',
      'items-start',
      'gap-5'
    )
    const latencyColumn =
      within(metrics).getByText('Latency short').parentElement
    const throughputColumn =
      within(metrics).getByText('Throughput short').parentElement
    const cacheColumn = within(metrics).getByText('Cache short').parentElement
    expect(latencyColumn).toHaveClass('shrink-0')
    expect(throughputColumn).toHaveClass('shrink-0')
    expect(cacheColumn).toHaveClass('shrink-0')
    // The cache hit rate sits to the right of throughput.
    expect(metrics.querySelector('dl')?.lastElementChild).toBe(cacheColumn)
    rerender(
      <ModelPerfBadge
        perf={{
          avg_latency_ms: 12000,
          avg_tps: 1420,
          success_rate: 98,
          cache_hit_rate: 62.5,
        }}
      />
    )
    expect(within(metrics).getByText('Latency short').parentElement).toBe(
      latencyColumn
    )
    expect(within(metrics).getByText('Throughput short').parentElement).toBe(
      throughputColumn
    )
    expect(within(cacheColumn as HTMLElement).getByText('62.5%')).toBeDefined()
  })

  it('shows separate generic and image cache prices including a free image cache', () => {
    render(
      <CachedPriceCell
        model={pricingModel({
          billing_mode: 'tiered_expr',
          billing_expr:
            'tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 0 + c * 30)',
        })}
        options={{ tokenUnit: 'M' }}
      />
    )
    expect(screen.getByText('Cache Read').parentElement).toHaveTextContent(
      '$1.25'
    )
    expect(screen.getByText('Image Cache').parentElement).toHaveTextContent(
      '$0'
    )
  })
  it('shows fixed prices per request in both token display units', () => {
    const model = pricingModel({
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("request", fixed(0.01))',
    })
    const { rerender } = render(
      <ModelCard model={model} onClick={vi.fn()} tokenUnit='K' />
    )
    expect(screen.getByText('$0.01')).toBeVisible()
    expect(screen.getByText('/ request')).toBeVisible()
    rerender(<ModelCard model={model} onClick={vi.fn()} tokenUnit='M' />)
    expect(screen.getByText('$0.01')).toBeVisible()
    expect(screen.queryByText('/ 1M')).not.toBeInTheDocument()
  })
  it('updates the current time tier at a minute boundary and after returning to the page', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-07T08:59:59+08:00'))
    const model = pricingModel({
      billing_mode: 'tiered_expr',
      billing_expr:
        'hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? tier("peak", p * 3 + c * 9) : tier("off_peak", p * 1.5 + c * 4.5)',
    })
    render(<ModelCard model={model} onClick={vi.fn()} tokenUnit='M' />)
    expect(screen.getByText('Current period price')).toBeVisible()
    expect(screen.getByText('$1.5')).toBeVisible()
    act(() => vi.advanceTimersByTime(1000))
    expect(screen.getByText('$3')).toBeVisible()
    act(() => {
      vi.setSystemTime(new Date('2026-09-07T12:00:00+08:00'))
      fireEvent(document, new Event('visibilitychange'))
    })
    expect(screen.getByText('$1.5')).toBeVisible()
  })

  it('copies the complete long model name without opening details', async () => {
    const user = userEvent.setup()
    const onClick = vi.fn()
    const name = 'provider/model-with-a-long-name-and-a-version-suffix-20260906'
    render(
      <ModelCard model={pricingModel({ model_name: name })} onClick={onClick} />
    )
    expect(screen.getByRole('heading', { name })).toHaveAttribute('title', name)
    await user.click(screen.getByRole('button', { name: 'Copy model name' }))
    expect(await navigator.clipboard.readText()).toBe(name)
    expect(onClick).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Details' }))
    expect(onClick).toHaveBeenCalledOnce()
    expect(onClick).toHaveBeenCalledWith(name)
  })

  it('retains a neutral health strip and missing values when metrics are unavailable', () => {
    render(<ModelCard model={pricingModel()} onClick={vi.fn()} />)
    const metrics = screen.getByLabelText(
      'Performance metrics for the last 24 hours'
    )
    expect(within(metrics).getAllByText('—')[0]).toBeVisible()
    expect(within(metrics).getByText('—s')).toBeVisible()
    expect(within(metrics).getByText('—t/s')).toBeVisible()
    expect(within(metrics).queryByText(/100/)).not.toBeInTheDocument()
    expect(
      within(metrics).getByRole('img', {
        name: 'Recent success-rate samples; gray bars indicate missing data.',
      })
    ).toBeVisible()
    expect(
      screen.queryByText('No description available.')
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Details' })).toBeEnabled()
  })

  it('uses fixed spacing between hourly status bars', () => {
    render(<ModelCard model={pricingModel()} onClick={vi.fn()} />)
    const statusStrip = screen.getByRole('img', {
      name: 'Recent success-rate samples; gray bars indicate missing data.',
    })
    expect(statusStrip).toHaveClass('gap-px')
    expect(statusStrip).not.toHaveClass('justify-between')
  })

  it('keeps group, endpoint and tag overflow counts with their own metadata', () => {
    const groups = ['default-with-a-long-group-name', 'premium', 'internal']
    const endpoints = ['openai-response', 'openai', 'claude', 'gemini', 'jina']
    const tags = [
      'video-generation',
      'high-resolution',
      'fast',
      'batch',
      'hd',
      'pro',
    ]
    render(
      <ModelCard
        model={pricingModel({
          enable_groups: groups,
          supported_endpoint_types: endpoints,
          tags: tags.join(','),
        })}
        onClick={vi.fn()}
      />
    )

    expect(screen.getByText('Groups')).toBeVisible()
    expect(screen.getByText(groups[0])).toBeVisible()
    expect(screen.getByText('+2')).toBeVisible()
    expect(screen.getByText('Endpoints')).toBeVisible()
    expect(screen.getByText(endpoints.slice(0, 2).join(', '))).toBeVisible()
    expect(screen.getByText('+3')).toBeVisible()
    expect(screen.getByText('Tags')).toBeVisible()
    expect(screen.getByText(tags.slice(0, 2).join(', '))).toBeVisible()
    expect(screen.getByText('+4')).toBeVisible()
    expect(screen.getByText('Token-based')).toBeVisible()
  })

  it('hides groups the site does not offer and adjusts the overflow count', () => {
    render(
      <ModelCard
        model={pricingModel({
          enable_groups: ['default', 'removed-group', 'another-removed'],
        })}
        onClick={vi.fn()}
        availableGroups={['default']}
      />
    )
    expect(screen.getByText('default')).toBeVisible()
    expect(screen.queryByText('removed-group')).not.toBeInTheDocument()
    expect(screen.queryByText(/another-removed/)).not.toBeInTheDocument()
    expect(screen.queryByText('+2')).not.toBeInTheDocument()
  })

  it('hides the groups row entirely when none of the model groups exist on the site', () => {
    render(
      <ModelCard
        model={pricingModel({ enable_groups: ['ghost-group'] })}
        onClick={vi.fn()}
        availableGroups={['default']}
      />
    )
    expect(screen.queryByText('Groups')).not.toBeInTheDocument()
    expect(screen.queryByText('ghost-group')).not.toBeInTheDocument()
  })

  it('omits metadata fields when the model has no groups, endpoints or tags', () => {
    render(
      <ModelCard
        model={pricingModel({ enable_groups: [] })}
        onClick={vi.fn()}
      />
    )
    expect(screen.queryByText('Groups')).not.toBeInTheDocument()
    expect(screen.queryByText('Endpoints')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('group', { name: 'Tags' })
    ).not.toBeInTheDocument()
  })

  it.each([
    { success_rate: 0, expected: '0.00%' },
    { success_rate: 99.8, expected: '99.80%' },
    { success_rate: Number.NaN, expected: '—' },
  ])(
    'shows $expected for the reported request success rate $success_rate',
    ({ success_rate, expected }) => {
      render(
        <ModelCard
          model={pricingModel()}
          onClick={vi.fn()}
          perf={{ avg_latency_ms: 1200, avg_tps: 42, success_rate }}
        />
      )
      const metrics = screen.getByLabelText(
        'Performance metrics for the last 24 hours'
      )
      expect(within(metrics).getAllByText(expected)[0]).toBeVisible()
      expect(within(metrics).getByText('Status')).toBeVisible()
      expect(within(metrics).getByText('1.20s')).toBeVisible()
      expect(within(metrics).getByText('42.0t/s')).toBeVisible()
    }
  )

  it('keeps group and recharge pricing correct when changing the token unit, including a free cache price', () => {
    const props = {
      model: pricingModel({ cache_ratio: 0 }),
      onClick: vi.fn(),
      selectedGroup: 'premium',
      showRechargePrice: true,
      priceRate: 3,
      usdExchangeRate: 6,
    }
    const { rerender } = render(<ModelCard {...props} tokenUnit='M' />)
    expect(screen.getByText('Input').parentElement).toHaveTextContent(/\$3/)
    expect(screen.getByText('Output').parentElement).toHaveTextContent(/\$9/)
    expect(screen.getByText('Cached').parentElement).toHaveTextContent(/\$0/)
    expect(screen.getAllByText('/ 1M').length).toBeGreaterThan(0)
    rerender(<ModelCard {...props} tokenUnit='K' />)
    expect(screen.getByText('Input').parentElement).toHaveTextContent(/\$0.003/)
    expect(screen.getByText('Output').parentElement).toHaveTextContent(
      /\$0.009/
    )
    expect(screen.getByText('Cached').parentElement).toHaveTextContent(/\$0/)
    expect(screen.getAllByText('/ 1K').length).toBeGreaterThan(0)
  })

  it('shows a per-request price with the selected group and recharge multiplier without a token unit', () => {
    render(
      <ModelCard
        model={pricingModel({ quota_type: 1, model_price: 0.4 })}
        onClick={vi.fn()}
        selectedGroup='premium'
        showRechargePrice
        priceRate={3}
        usdExchangeRate={6}
        tokenUnit='K'
      />
    )
    expect(screen.getByText(/\$0.6/).parentElement).toHaveTextContent(
      /\$0.6\s*\/\s*request/
    )
    expect(screen.queryByText(/1K|1M/)).not.toBeInTheDocument()
    expect(screen.getAllByText('Per Request')).toHaveLength(1)
    expect(screen.queryByText('Per-request')).not.toBeInTheDocument()
  })

  it('preserves expression prices and makes the selected token unit explicit', () => {
    render(
      <ModelCard
        model={pricingModel({
          billing_mode: 'tiered_expr',
          billing_expr: 'tier("base", p * 3 + c * 15)',
        })}
        onClick={vi.fn()}
        tokenUnit='K'
        selectedGroup='premium'
        showRechargePrice
        priceRate={3}
        usdExchangeRate={6}
      />
    )
    expect(screen.getByText('Input').parentElement).toHaveTextContent(
      /Input\s*\$0.0045\s*\/\s*1K/
    )
    expect(screen.getByText('Output').parentElement).toHaveTextContent(
      /Output\s*\$0.0225\s*\/\s*1K/
    )
  })

  it('keeps task price ranges in their actual usage unit instead of the selected token unit', () => {
    render(
      <ModelCard
        model={pricingModel({
          billing_mode: 'tiered_expr',
          billing_expr:
            'u("mode") == "pro" ? tier("pro", u("seconds") * 0.8) : tier("std", u("seconds") * 0.4)',
          billing_usage_schema: {
            seconds: { type: 'number', unit: 'second' },
            mode: { enum: ['std', 'pro'] },
          },
        })}
        onClick={vi.fn()}
        tokenUnit='K'
      />
    )
    expect(screen.getByText(/0.4.*0.8/).parentElement).toHaveTextContent(
      /0.4 – \$0.8\s*\/\s*s/
    )
    expect(screen.queryByText(/1K|1M/)).not.toBeInTheDocument()
  })

  it('shows the unconfigured usage message without inventing a token price', () => {
    render(
      <ModelCard
        model={pricingModel({
          billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
        })}
        onClick={vi.fn()}
      />
    )
    expect(
      screen.getByText('Usage-based billing · price not configured')
    ).toBeVisible()
    expect(screen.queryByText('Input')).not.toBeInTheDocument()
  })

  it('shows a spaced task token range with its unit when an example price is present', () => {
    render(
      <ModelCard
        model={pricingModel({
          billing_mode: 'tiered_expr',
          billing_expr:
            'u("mode") == "pro" ? tier("pro", u("tokens") * 70 / 1000000) : tier("std", u("tokens") * 42 / 1000000)',
          billing_usage_schema: {
            tokens: { type: 'number', unit: 'token' },
            mode: { enum: ['std', 'pro'] },
          },
          billing_usage_examples: [
            { label: '480p · 5s', facts: { tokens: 48000, mode: 'std' } },
          ],
        })}
        onClick={vi.fn()}
        tokenUnit='K'
      />
    )
    expect(screen.getByText(/\$42 – \$70/).parentElement).toHaveTextContent(
      /\$42 – \$70\s*\/\s*1M token/
    )
    expect(screen.getByText(/480p · 5s ≈/)).toBeVisible()
  })

  it('keeps an unrecognized expression visible with the special billing message', () => {
    const expression =
      'u("seconds") > 30 ? tier("long", u("seconds") * 0.3) : tier("short", u("seconds") * 0.4)'
    render(
      <ModelCard
        model={pricingModel({
          billing_mode: 'tiered_expr',
          billing_expr: expression,
          billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
        })}
        onClick={vi.fn()}
      />
    )
    expect(screen.getByText('Special billing expression')).toBeVisible()
    expect(screen.getByText(expression)).toBeVisible()
  })

  it('keeps browsing and neutral health placeholders available without a status snapshot', async () => {
    const onModelClick = vi.fn()
    render(
      <QueryClientProvider client={queryClient}>
        <ModelCardGrid models={[pricingModel()]} onModelClick={onModelClick} />
      </QueryClientProvider>
    )
    expect(
      within(
        screen.getByLabelText('Performance metrics for the last 24 hours')
      ).getAllByText(/^—/)
    ).toHaveLength(4)
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Details' }))
    expect(onModelClick).toHaveBeenCalledWith('example-model')
  })

  it('paginates the model cards and disables navigation at both boundaries', async () => {
    const models = Array.from({ length: 21 }, (_, index) =>
      pricingModel({ id: index + 1, model_name: `model-${index + 1}` })
    )
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={queryClient}>
        <ModelCardGrid models={models} onModelClick={vi.fn()} />
      </QueryClientProvider>
    )
    expect(screen.getByRole('button', { name: 'Previous page' })).toBeDisabled()
    expect(screen.getAllByRole('heading', { level: 3 })).toHaveLength(20)
    await user.click(screen.getByRole('button', { name: 'Next page' }))
    expect(screen.getAllByRole('heading', { level: 3 })).toHaveLength(1)
    expect(screen.getByRole('heading', { name: 'model-21' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Previous page' }))
    expect(screen.getByRole('heading', { name: 'model-1' })).toBeVisible()
  })

  it('switches the card grid to three columns at the xl breakpoint instead of 2xl', () => {
    queryClient.setQueryData(['perf-metrics-summary', 24], {
      success: true,
      data: { models: [] },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <ModelCardGrid models={[pricingModel()]} onModelClick={vi.fn()} />
      </QueryClientProvider>
    )
    const grid = screen
      .getByRole('heading', { name: 'example-model' })
      .closest('.grid')
    expect(grid).toHaveClass('xl:grid-cols-3')
    expect(grid).not.toHaveClass('2xl:grid-cols-3')
    expect(grid).not.toHaveClass('min-[1440px]:grid-cols-3')
  })

  it('positions sparse hourly data accurately and opens performance from the strip', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.setSystemTime(new Date('2026-09-07T12:37:00.000Z'))
    const currentHourStart = Math.floor(Date.now() / 1000 / 3600) * 3600
    const onClick = vi.fn()
    const onOpenPerformance = vi.fn()
    const user = userEvent.setup()
    render(
      <ModelCard
        model={pricingModel()}
        onClick={onClick}
        onOpenPerformance={onOpenPerformance}
        perf={{
          avg_latency_ms: 1200,
          avg_tps: 42,
          success_rate: 80,
          window_start: currentHourStart - 23 * 3600,
          recent_success_series: [
            { ts: currentHourStart - 5 * 3600, success_rate: 80 },
          ],
        }}
      />
    )

    const strip = screen.getByRole('img', {
      name: 'Recent success-rate samples; gray bars indicate missing data.',
    })
    expect(strip).toHaveClass('flex', 'w-24', 'gap-px')
    expect(strip.children).toHaveLength(24)
    const spans = [...strip.children]
    spans.forEach((slot, index) => {
      if (index === 18) {
        expect(slot.classList.contains('bg-muted-foreground/15')).toBe(false)
        return
      }
      expect(slot.classList.contains('bg-muted-foreground/15')).toBe(true)
    })

    const performanceButton = screen.getByRole('button', {
      name: 'View performance',
    })
    performanceButton.focus()
    await user.keyboard('{Enter}')
    expect(onOpenPerformance).toHaveBeenCalledOnce()
    expect(onClick).not.toHaveBeenCalled()
    vi.useRealTimers()
  })

  it('lights slots 23 and 18 when series has the current hour and five hours earlier', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-07T12:00:00.000Z'))
    const currentHourStart = Math.floor(Date.now() / 1000 / 3600) * 3600

    render(
      <ModelCard
        model={pricingModel()}
        onClick={vi.fn()}
        perf={{
          avg_latency_ms: 1200,
          avg_tps: 42,
          success_rate: 100,
          window_start: currentHourStart - 23 * 3600,
          recent_success_series: [
            { ts: currentHourStart, success_rate: 100 },
            { ts: currentHourStart - 5 * 3600, success_rate: 80 },
          ],
        }}
      />
    )

    const spans = [
      ...screen.getByRole('img', {
        name: 'Recent success-rate samples; gray bars indicate missing data.',
      }).children,
    ]
    expect(spans).toHaveLength(24)
    spans.forEach((slot, index) => {
      if (index === 18 || index === 23) {
        expect(slot.classList.contains('bg-muted-foreground/15')).toBe(false)
        return
      }
      expect(slot.classList.contains('bg-muted-foreground/15')).toBe(true)
    })
    vi.useRealTimers()
  })

  it('keeps all 24 slots gray when recent_success_series is undefined', () => {
    render(
      <ModelCard
        model={pricingModel()}
        onClick={vi.fn()}
        perf={{ avg_latency_ms: 1200, avg_tps: 42, success_rate: 100 }}
      />
    )

    const spans = [
      ...screen.getByRole('img', {
        name: 'Recent success-rate samples; gray bars indicate missing data.',
      }).children,
    ]
    expect(spans).toHaveLength(24)
    spans.forEach((slot) => {
      expect(slot.classList.contains('bg-muted-foreground/15')).toBe(true)
    })
  })

  it('keeps all 24 slots gray when a series point is 24 hours before the current hour', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-07T12:00:00.000Z'))
    const currentHourStart = Math.floor(Date.now() / 1000 / 3600) * 3600

    render(
      <ModelCard
        model={pricingModel()}
        onClick={vi.fn()}
        perf={{
          avg_latency_ms: 1200,
          avg_tps: 42,
          success_rate: 100,
          window_start: currentHourStart - 23 * 3600,
          recent_success_series: [
            { ts: currentHourStart - 24 * 3600, success_rate: 100 },
          ],
        }}
      />
    )

    const spans = [
      ...screen.getByRole('img', {
        name: 'Recent success-rate samples; gray bars indicate missing data.',
      }).children,
    ]
    expect(spans).toHaveLength(24)
    spans.forEach((slot) => {
      expect(slot.classList.contains('bg-muted-foreground/15')).toBe(true)
    })
    vi.useRealTimers()
  })

  it('keeps all 24 slots gray when recent_success_series is undefined', () => {
    render(
      <ModelCard
        model={pricingModel()}
        onClick={vi.fn()}
        perf={{ avg_latency_ms: 1200, avg_tps: 42, success_rate: 100 }}
      />
    )

    const spans = [
      ...screen.getByRole('img', {
        name: 'Recent success-rate samples; gray bars indicate missing data.',
      }).children,
    ]
    expect(spans).toHaveLength(24)
    spans.forEach((slot) => {
      expect(slot.classList.contains('bg-muted-foreground/15')).toBe(true)
    })
  })

  it('uses the server window even when the browser clock is a day ahead', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-07T12:37:00.000Z'))
    const currentHourStart = Math.floor(Date.now() / 1000 / 3600) * 3600

    render(
      <ModelCard
        model={pricingModel()}
        onClick={vi.fn()}
        perf={{
          avg_latency_ms: 1200,
          avg_tps: 42,
          success_rate: 80,
          window_start: currentHourStart - 47 * 3600,
          recent_success_series: [
            { ts: currentHourStart - 29 * 3600, success_rate: 80 },
          ],
        }}
      />
    )

    const spans = [
      ...screen.getByRole('img', {
        name: 'Recent success-rate samples; gray bars indicate missing data.',
      }).children,
    ]
    expect(spans).toHaveLength(24)
    spans.forEach((slot, index) => {
      if (index === 18) {
        expect(slot.classList.contains('bg-muted-foreground/15')).toBe(false)
        return
      }
      expect(slot.classList.contains('bg-muted-foreground/15')).toBe(true)
    })
    vi.useRealTimers()
  })
})
