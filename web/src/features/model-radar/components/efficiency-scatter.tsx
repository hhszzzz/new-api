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
import { ChartScatterIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { VChart } from '@visactor/react-vchart'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import { createModelColorMap } from '../lib/model-radar'
import type { ModelRadarConfiguration, ScatterMetric } from '../types'

const SCATTER_METRICS: Array<{ value: ScatterMetric; label: string }> = [
  { value: 'combined_cost_index', label: 'Combined cost' },
  { value: 'average_price_usd', label: 'Cost' },
  { value: 'average_minutes', label: 'Duration' },
]

export function EfficiencyScatter(props: {
  configurations: ModelRadarConfiguration[]
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const { resolvedTheme, themeReady } = useChartTheme()
  const [metric, setMetric] = useState<ScatterMetric>('combined_cost_index')
  const modelColors = useMemo(
    () => createModelColorMap(props.configurations),
    [props.configurations]
  )
  const chartTextColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const chartGridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'
  const metricLabel = t(
    SCATTER_METRICS.find((item) => item.value === metric)?.label ??
      'Combined cost'
  )

  const data = useMemo(
    () =>
      props.configurations.flatMap((configuration) => {
        const value = configuration[metric]
        if (value === null) return []
        return [
          {
            model: configuration.model,
            effort: configuration.effort,
            configuration: `${configuration.model} / ${configuration.effort}`,
            iq: configuration.iq,
            value,
            color: modelColors.get(configuration.model) ?? '#64748b',
          },
        ]
      }),
    [metric, modelColors, props.configurations]
  )

  const spec = useMemo(() => {
    if (data.length === 0) return null
    return {
      type: 'scatter' as const,
      data: [{ id: 'model-radar-scatter', values: data }],
      xField: 'value',
      yField: 'iq',
      seriesField: 'model',
      shapeField: 'effort',
      legends: { visible: false },
      point: {
        style: {
          size: 10,
          fill: (datum: { color: string }) => datum.color,
          fillOpacity: 0.82,
          stroke: resolvedTheme === 'dark' ? '#0f172a' : '#ffffff',
          lineWidth: 1.5,
        },
      },
      axes: [
        {
          orient: 'bottom',
          title: { visible: true, text: metricLabel },
          label: {
            formatMethod: (value: number | string) => {
              const numericValue = Number(value)
              if (metric === 'average_price_usd') {
                return format.usd(numericValue) ?? String(value)
              }
              return format.decimal(numericValue) ?? String(value)
            },
            style: { fill: chartTextColor, fontSize: 10 },
          },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: chartGridColor },
          },
        },
        {
          orient: 'left',
          title: { visible: true, text: 'IQ' },
          label: { style: { fill: chartTextColor, fontSize: 10 } },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: chartGridColor },
          },
        },
      ],
      tooltip: {
        mark: {
          title: {
            value: (datum: { configuration: string }) => datum.configuration,
          },
          content: [
            {
              key: 'IQ',
              value: (datum: { iq: number }) => format.decimal(datum.iq),
            },
            {
              key: metricLabel,
              value: (datum: { value: number }) => {
                if (metric === 'average_price_usd') {
                  return format.usd(datum.value)
                }
                return format.decimal(datum.value)
              },
            },
          ],
        },
      },
      animationAppear: { duration: 350 },
    }
  }, [
    chartGridColor,
    chartTextColor,
    data,
    format,
    metric,
    metricLabel,
    resolvedTheme,
  ])

  return (
    <section
      aria-labelledby='efficiency-scatter-title'
      className='border-border/70 border-t py-6'
    >
      <header className='mb-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div>
          <div className='flex items-center gap-2'>
            <HugeiconsIcon
              icon={ChartScatterIcon}
              className='text-primary size-4'
              strokeWidth={2}
              aria-hidden='true'
            />
            <h2
              id='efficiency-scatter-title'
              className='text-base font-semibold'
            >
              {t('Efficiency scatter plot')}
            </h2>
          </div>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Compare IQ against cost and response-time efficiency.')}
          </p>
        </div>
        <ToggleGroup
          value={[metric]}
          onValueChange={(values) => {
            const nextMetric = values.find((value) => value !== metric)
            if (nextMetric) setMetric(nextMetric as ScatterMetric)
          }}
          variant='outline'
          size='sm'
          aria-label={t('Scatter plot horizontal metric')}
          className='max-w-full self-start overflow-x-auto'
        >
          {SCATTER_METRICS.map((item) => (
            <ToggleGroupItem key={item.value} value={item.value}>
              {t(item.label)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </header>

      <div className='bg-card h-80 overflow-hidden rounded-lg border p-2 sm:h-96'>
        {themeReady && spec ? (
          <VChart
            key={`radar-scatter-${resolvedTheme}-${metric}`}
            spec={{
              ...spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        ) : (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No comparable data available')}
          </div>
        )}
      </div>

      <div
        className='mt-3 flex flex-wrap gap-x-4 gap-y-2'
        aria-label={t('Models')}
      >
        {Array.from(modelColors, ([model, color]) => (
          <span
            key={model}
            className='inline-flex min-w-0 items-center gap-1.5 text-xs'
          >
            <span
              className='size-2 shrink-0 rounded-full'
              style={{ backgroundColor: color }}
              aria-hidden='true'
            />
            <span className='max-w-48 truncate'>{model}</span>
          </span>
        ))}
      </div>
    </section>
  )
}
