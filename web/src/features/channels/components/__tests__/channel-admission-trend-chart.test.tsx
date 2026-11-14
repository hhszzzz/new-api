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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import type { ChannelAdmissionMetricPoint } from '../../types'
import { ChannelAdmissionTrendChart } from '../channel-admission-trend-chart'

interface ChartSpec {
  type: string
  data: Array<{ id: string; values: Record<string, unknown>[] }>
  axes: Array<{
    orient: string
    label?: { formatMethod?: (value: number | string) => string }
    tick?: { noDecimals?: boolean }
  }>
}

const chart = vi.hoisted(() => vi.fn((_props: { spec: unknown }) => null))
vi.mock('@visactor/react-vchart', () => ({ VChart: chart }))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: vi.fn() },
}))

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

type TrendSeries = Array<{
  key: keyof ChannelAdmissionMetricPoint
  label: string
  color: string
}>

const countSeries: TrendSeries = [
  { key: 'grants', label: 'Grants', color: '#0ea5e9' },
]

function point(
  overrides: Partial<ChannelAdmissionMetricPoint> = {}
): ChannelAdmissionMetricPoint {
  return {
    timestamp_ms: new Date(2026, 10, 12, 23, 5).getTime(),
    grants: 0,
    waited: 0,
    soup_grants: 0,
    priority_grants: 0,
    queue_full: 0,
    timeouts: 0,
    wait_p50_ms: 0,
    wait_p95_ms: 0,
    ...overrides,
  }
}

function renderChart(props: {
  title: string
  summary?: string
  points: ChannelAdmissionMetricPoint[]
  series: typeof countSeries
  unit?: string
}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ChannelAdmissionTrendChart
        title={props.title}
        summary={props.summary ? <span>{props.summary}</span> : undefined}
        points={props.points}
        bucketSeconds={300}
        series={props.series}
        unit={props.unit}
      />
    </I18nextProvider>
  )
}

beforeEach(() => {
  chart.mockClear()
})

afterEach(() => {
  cleanup()
})

it('shows the empty history message when every bucket is zero', async () => {
  renderChart({
    title: 'Admissions',
    points: [point(), point()],
    series: countSeries,
  })

  expect(await screen.findByText('No data available')).toBeVisible()
  expect(chart).not.toHaveBeenCalled()
})

it('renders area buckets with integer count ticks', async () => {
  renderChart({
    title: 'Admissions',
    points: [point({ grants: 2 })],
    series: countSeries,
  })

  await waitFor(() => expect(chart).toHaveBeenCalledTimes(1))
  const spec = chart.mock.calls[0][0].spec as ChartSpec
  expect(spec.type).toBe('area')
  expect(spec.data[0].values).toEqual([
    expect.objectContaining({ series: 'Grants', value: 2 }),
  ])
  expect(spec.axes[1].tick?.noDecimals).toBe(true)
})

it('renders optional header summary alongside the bucket interval', () => {
  renderChart({
    title: 'Admissions',
    summary: 'Grants 12',
    points: [],
    series: countSeries,
  })

  expect(screen.getByText('Grants 12')).toBeVisible()
})

it('formats wait-time values in seconds', async () => {
  renderChart({
    title: 'Queue wait time',
    points: [point({ wait_p50_ms: 5_000 })],
    series: [{ key: 'wait_p50_ms', label: 'Wait p50', color: '#10b981' }],
    unit: 'ms',
  })

  await waitFor(() => expect(chart).toHaveBeenCalledTimes(1))
  const spec = chart.mock.calls[0][0].spec as ChartSpec
  expect(spec.data[0].values).toEqual([
    expect.objectContaining({ series: 'Wait p50', value: 5_000 }),
  ])
  expect(spec.axes[1].label?.formatMethod?.(5_000)).toBe('5.0s')
})
