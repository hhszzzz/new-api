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
import { ChartLineData01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { VChart } from '@visactor/react-vchart'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import {
  getEffortBorderStyle,
  getEffortLineDash,
  groupConfigurations,
} from '../lib/model-radar'
import type { ModelRadarConfiguration, ModelRadarHistoryFrame } from '../types'

export function IQTrends(props: {
  configurations: ModelRadarConfiguration[]
  history: ModelRadarHistoryFrame[]
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const { resolvedTheme, themeReady } = useChartTheme()
  const chartTextColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const chartGridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'

  const groups = useMemo(() => {
    const configurationGroups = groupConfigurations(props.configurations)
    return configurationGroups.map((group) => ({
      ...group,
      points: props.history.flatMap((frame) =>
        frame.points
          .filter((point) => point.model === group.model)
          .map((point) => ({
            effort: point.effort,
            iq: point.iq,
            time: format.historyTime(frame.ts),
          }))
      ),
    }))
  }, [format, props.configurations, props.history])

  return (
    <section
      aria-labelledby='iq-trends-title'
      className='border-border/70 border-t py-6'
    >
      <header className='mb-4'>
        <div className='flex items-center gap-2'>
          <HugeiconsIcon
            icon={ChartLineData01Icon}
            className='text-primary size-4'
            strokeWidth={2}
            aria-hidden='true'
          />
          <h2 id='iq-trends-title' className='text-base font-semibold'>
            {t('48-hour IQ trends')}
          </h2>
        </div>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('Each chart compares all reasoning efforts for one model.')}
        </p>
      </header>

      <div className='grid gap-3 lg:grid-cols-2'>
        {groups.map((group) => (
          <article
            key={group.model}
            className='bg-card min-w-0 overflow-hidden rounded-lg border'
            aria-label={t('IQ trend for {{model}}', { model: group.model })}
          >
            <div className='flex min-w-0 flex-wrap items-center justify-between gap-2 border-b px-3 py-2.5'>
              <h3 className='min-w-0 truncate text-sm font-semibold'>
                {group.model}
              </h3>
              <div className='flex flex-wrap justify-end gap-x-2.5 gap-y-1'>
                {group.configurations.map((configuration) => (
                  <span
                    key={configuration.effort}
                    className='text-muted-foreground inline-flex items-center gap-1 text-[10px] uppercase'
                  >
                    <span
                      className='w-4 border-t-2'
                      style={{
                        borderTopColor: group.color,
                        borderTopStyle: getEffortBorderStyle(
                          configuration.effort
                        ),
                      }}
                      aria-hidden='true'
                    />
                    {configuration.effort}
                  </span>
                ))}
              </div>
            </div>
            <TrendChart
              points={group.points}
              color={group.color}
              themeReady={themeReady}
              resolvedTheme={resolvedTheme}
              chartTextColor={chartTextColor}
              chartGridColor={chartGridColor}
            />
          </article>
        ))}
      </div>
    </section>
  )
}

function TrendChart(props: {
  points: Array<{ effort: string; iq: number; time: string }>
  color: string
  themeReady: boolean
  resolvedTheme: string
  chartTextColor: string
  chartGridColor: string
}) {
  const { t } = useTranslation()
  const spec = useMemo(() => {
    if (props.points.length === 0) return null
    return {
      type: 'line' as const,
      data: [{ id: 'model-iq-trend', values: props.points }],
      xField: 'time',
      yField: 'iq',
      seriesField: 'effort',
      legends: { visible: false },
      line: {
        style: {
          stroke: props.color,
          lineWidth: 2,
          lineDash: (datum: { effort: string }) =>
            getEffortLineDash(datum.effort),
        },
      },
      point: {
        visible: true,
        style: {
          size: 4,
          fill: props.color,
          fillOpacity: 0.82,
        },
      },
      axes: [
        {
          orient: 'bottom',
          label: {
            autoHide: true,
            autoLimit: true,
            style: { fill: props.chartTextColor, fontSize: 9 },
          },
          tick: { visible: false },
        },
        {
          orient: 'left',
          label: { style: { fill: props.chartTextColor, fontSize: 9 } },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: props.chartGridColor },
          },
        },
      ],
      tooltip: {
        dimension: {
          title: { value: (datum: { time: string }) => datum.time },
          content: [
            {
              key: (datum: { effort: string }) => datum.effort,
              value: (datum: { iq: number }) => datum.iq.toFixed(1),
            },
          ],
        },
      },
      animationAppear: { duration: 300 },
    }
  }, [props.chartGridColor, props.chartTextColor, props.color, props.points])

  return (
    <div className='h-56 p-2'>
      {props.themeReady && spec ? (
        <VChart
          key={`iq-trend-${props.resolvedTheme}`}
          spec={{
            ...spec,
            theme: props.resolvedTheme === 'dark' ? 'dark' : 'light',
            background: 'transparent',
          }}
          option={VCHART_OPTION}
        />
      ) : (
        <div className='text-muted-foreground flex h-full items-center justify-center text-xs'>
          {t('No history data available')}
        </div>
      )}
    </div>
  )
}
