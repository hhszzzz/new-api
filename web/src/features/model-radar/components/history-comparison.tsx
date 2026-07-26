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
import { Analytics01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { VChart } from '@visactor/react-vchart'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Checkbox } from '@/components/ui/checkbox'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import {
  configurationKey,
  createModelColorMap,
  flattenHistory,
  getEffortBorderStyle,
  getEffortLineDash,
  getMetricValue,
  groupConfigurations,
} from '../lib/model-radar'
import type {
  ModelRadarConfiguration,
  ModelRadarHistoryFrame,
  RadarMetric,
} from '../types'

const HISTORY_METRICS: Array<{ value: RadarMetric; label: string }> = [
  { value: 'iq', label: 'IQ' },
  { value: 'average_price_usd', label: 'Cost' },
  { value: 'average_minutes', label: 'Duration' },
  { value: 'average_agent_steps', label: 'Agent steps' },
  { value: 'cache_hit_rate', label: 'Cache hit rate' },
  { value: 'average_total_tokens', label: 'Tokens' },
]

export function HistoryComparison(props: {
  configurations: ModelRadarConfiguration[]
  history: ModelRadarHistoryFrame[]
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const { resolvedTheme, themeReady } = useChartTheme()
  const [metric, setMetric] = useState<RadarMetric>('iq')
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(() => new Set())
  const groups = useMemo(
    () => groupConfigurations(props.configurations),
    [props.configurations]
  )
  const modelColors = useMemo(
    () => createModelColorMap(props.configurations),
    [props.configurations]
  )
  const flattenedHistory = useMemo(
    () => flattenHistory(props.history),
    [props.history]
  )
  const selectedConfigurations = useMemo(
    () =>
      props.configurations.filter((configuration) =>
        selectedKeys.has(
          configurationKey(configuration.model, configuration.effort)
        )
      ),
    [props.configurations, selectedKeys]
  )
  const chartData = useMemo(
    () =>
      flattenedHistory.flatMap((point) => {
        if (!selectedKeys.has(point.configuration)) return []
        const value = getMetricValue(point, metric)
        if (value === null) return []
        return [
          {
            configuration: `${point.model} / ${point.effort}`,
            effort: point.effort,
            model: point.model,
            time: format.historyTime(point.ts),
            value,
            color: modelColors.get(point.model) ?? '#64748b',
          },
        ]
      }),
    [flattenedHistory, format, metric, modelColors, selectedKeys]
  )
  const metricLabel = t(
    HISTORY_METRICS.find((item) => item.value === metric)?.label ?? 'IQ'
  )
  const chartTextColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const chartGridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'

  const spec = useMemo(() => {
    if (chartData.length === 0) return null
    return {
      type: 'line' as const,
      data: [{ id: 'model-radar-comparison', values: chartData }],
      xField: 'time',
      yField: 'value',
      seriesField: 'configuration',
      legends: { visible: false },
      line: {
        style: {
          stroke: (datum: { color: string }) => datum.color,
          lineWidth: 2,
          lineDash: (datum: { effort: string }) =>
            getEffortLineDash(datum.effort),
        },
      },
      point: {
        visible: true,
        style: {
          size: 4,
          fill: (datum: { color: string }) => datum.color,
        },
      },
      axes: [
        {
          orient: 'bottom',
          label: {
            autoHide: true,
            autoLimit: true,
            style: { fill: chartTextColor, fontSize: 10 },
          },
          tick: { visible: false },
        },
        {
          orient: 'left',
          title: { visible: true, text: metricLabel },
          label: {
            formatMethod: (value: number | string) => {
              const numericValue = Number(value)
              if (metric === 'average_price_usd') {
                return format.usd(numericValue) ?? String(value)
              }
              if (metric === 'cache_hit_rate') {
                return format.percent(numericValue) ?? String(value)
              }
              if (metric === 'average_total_tokens') {
                return format.compact(numericValue) ?? String(value)
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
      ],
      tooltip: {
        dimension: {
          title: { value: (datum: { time: string }) => datum.time },
          content: [
            {
              key: (datum: { configuration: string }) => datum.configuration,
              value: (datum: { value: number }) => {
                if (metric === 'average_price_usd') {
                  return format.usd(datum.value)
                }
                if (metric === 'cache_hit_rate') {
                  return format.percent(datum.value)
                }
                if (metric === 'average_total_tokens') {
                  return format.integer(datum.value)
                }
                return format.decimal(datum.value)
              },
            },
          ],
        },
      },
      animationAppear: { duration: 300 },
    }
  }, [chartData, chartGridColor, chartTextColor, format, metric, metricLabel])

  const selectItems = HISTORY_METRICS.map((item) => ({
    value: item.value,
    label: t(item.label),
  }))
  let comparisonContent: React.ReactNode
  if (selectedKeys.size === 0) {
    comparisonContent = (
      <div
        role='status'
        aria-label={t('Select at least one configuration to compare history.')}
        className='text-muted-foreground flex h-64 items-center justify-center rounded-lg border border-dashed px-5 text-center text-sm sm:h-80'
      >
        {t('Select at least one configuration to compare history.')}
      </div>
    )
  } else if (chartData.length === 0) {
    comparisonContent = (
      <div
        role='status'
        aria-label={t(
          'The selected configurations have no data for this metric.'
        )}
        className='text-muted-foreground flex h-64 items-center justify-center rounded-lg border border-dashed px-5 text-center text-sm sm:h-80'
      >
        {t('The selected configurations have no data for this metric.')}
      </div>
    )
  } else {
    comparisonContent = (
      <div className='bg-card h-80 overflow-hidden rounded-lg border p-2 sm:h-96'>
        {themeReady && spec ? (
          <VChart
            key={`radar-comparison-${resolvedTheme}-${metric}`}
            spec={{
              ...spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        ) : null}
      </div>
    )
  }

  return (
    <section
      aria-labelledby='history-comparison-title'
      className='border-border/70 border-t py-6'
    >
      <header className='mb-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div>
          <div className='flex items-center gap-2'>
            <HugeiconsIcon
              icon={Analytics01Icon}
              className='text-primary size-4'
              strokeWidth={2}
              aria-hidden='true'
            />
            <h2
              id='history-comparison-title'
              className='text-base font-semibold'
            >
              {t('Historical comparison')}
            </h2>
          </div>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Select any configurations and metric to compare over time.')}
          </p>
        </div>
        <Select
          items={selectItems}
          value={metric}
          onValueChange={(value) => {
            if (value) setMetric(value as RadarMetric)
          }}
        >
          <SelectTrigger
            size='sm'
            className='w-full sm:w-44'
            aria-label={t('Comparison metric')}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {HISTORY_METRICS.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {t(item.label)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </header>

      <fieldset className='bg-muted/20 mb-4 max-h-44 overflow-y-auto rounded-lg border p-3'>
        <legend className='sr-only'>{t('Configurations to compare')}</legend>
        <div className='grid grid-cols-1 gap-x-4 gap-y-2 sm:grid-cols-2 lg:grid-cols-3'>
          {groups.flatMap((group) =>
            group.configurations.map((configuration) => {
              const key = configurationKey(
                configuration.model,
                configuration.effort
              )
              const id = `radar-compare-${key.replaceAll(/[^a-zA-Z0-9_-]/g, '-')}`
              return (
                <label
                  key={key}
                  htmlFor={id}
                  className='hover:bg-accent/50 flex min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-xs'
                >
                  <Checkbox
                    id={id}
                    aria-label={`${configuration.model} ${configuration.effort}`}
                    checked={selectedKeys.has(key)}
                    onCheckedChange={(checked) => {
                      setSelectedKeys((current) => {
                        const next = new Set(current)
                        if (checked) next.add(key)
                        else next.delete(key)
                        return next
                      })
                    }}
                  />
                  <span
                    className='size-2 shrink-0 rounded-full'
                    style={{ backgroundColor: group.color }}
                    aria-hidden='true'
                  />
                  <span className='min-w-0 truncate'>
                    {configuration.model}
                  </span>
                  <span className='text-muted-foreground shrink-0 uppercase'>
                    {configuration.effort}
                  </span>
                </label>
              )
            })
          )}
        </div>
      </fieldset>

      {comparisonContent}

      {selectedConfigurations.length > 0 ? (
        <div className='mt-3 flex flex-wrap gap-x-4 gap-y-2'>
          {selectedConfigurations.map((configuration) => (
            <span
              key={configurationKey(configuration.model, configuration.effort)}
              className='inline-flex min-w-0 items-center gap-1.5 text-xs'
            >
              <span
                className='w-4 border-t-2'
                style={{
                  borderTopColor:
                    modelColors.get(configuration.model) ?? '#64748b',
                  borderTopStyle: getEffortBorderStyle(configuration.effort),
                }}
                aria-hidden='true'
              />
              <span className='max-w-52 truncate'>
                {configuration.model} / {configuration.effort}
              </span>
            </span>
          ))}
        </div>
      ) : null}
    </section>
  )
}
