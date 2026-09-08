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
import { useQuery } from '@tanstack/react-query'
import { HeartPulse, Timer } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  staticDataTableClassNames as tableStyles,
} from '@/components/data-table'
import { GroupBadge } from '@/components/group-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { getPerfMetrics } from '@/features/performance-metrics/api'
import { ModelStatusDetails } from '@/features/performance-metrics/components/model-status-details'
import {
  formatLatency,
  formatThroughput,
} from '@/features/performance-metrics/lib/format'
import type { ModelStatusModel } from '@/features/performance-metrics/status-types'
import type { PerformanceGroup } from '@/features/performance-metrics/types'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import type { UptimeDayPoint } from '../lib/mock-stats'
import type { PricingModel } from '../types'
import { LatencyTrendChart } from './model-details-charts'
import { UptimeSparkline } from './model-details-uptime-sparkline'

type PerformanceRow = {
  group: string
  avg_ttft_ms: number
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
}

function toUptimePct(value: number): number {
  if (!Number.isFinite(value)) return 0
  const clamped = Math.min(100, Math.max(0, value))
  return Math.round(clamped * 100) / 100
}

function toGroupUptimeSeries(group: PerformanceGroup): UptimeDayPoint[] {
  return group.series.map((point) => {
    const successRate = toUptimePct(point.success_rate)
    return {
      date: new Date(point.ts * 1000).toISOString(),
      uptime_pct: successRate,
      incidents: successRate < 100 ? 1 : 0,
      outage_minutes: 0,
    }
  })
}

export function ModelDetailsPerformance(props: {
  model: PricingModel
  status?: ModelStatusModel
  generatedAt?: number
  selectedGroup?: string
  isLoading?: boolean
  isError?: boolean
}) {
  const { t, i18n } = useTranslation()
  const [initialTimestamp] = useState(() => Date.now() / 1000)
  const user = useAuthStore((state) => state.auth.user)
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const hourFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(locale, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      }),
    [locale]
  )
  const numberFormatter = useMemo(() => new Intl.NumberFormat(locale), [locale])
  const metricsQuery = useQuery({
    queryKey: [
      'pricing-group-performance',
      user?.id ?? null,
      user?.groups ?? user?.group ?? null,
      props.model.model_name,
    ],
    queryFn: () => getPerfMetrics(props.model.model_name, 24),
    enabled: !props.selectedGroup,
    staleTime: 60 * 1000,
    retry: false,
  })
  const groups = useMemo(
    () => metricsQuery.data?.data.groups ?? [],
    [metricsQuery.data]
  )
  const performances = useMemo<PerformanceRow[]>(
    () =>
      groups.map((group) => ({
        group: group.group,
        avg_ttft_ms: group.avg_ttft_ms,
        avg_latency_ms: group.avg_latency_ms,
        success_rate: group.success_rate,
        avg_tps: group.avg_tps,
      })),
    [groups]
  )
  const latencySeries = useMemo(
    () =>
      (props.status?.timeline ?? []).flatMap((point) =>
        point.avg_ttft_ms === null
          ? []
          : [
              {
                timestamp: new Date(point.ts * 1000).toISOString(),
                group: 'latency',
                ttft_ms: point.avg_ttft_ms,
              },
            ]
      ),
    [props.status]
  )
  const uptimeByGroup = useMemo<Record<string, UptimeDayPoint[]>>(() => {
    const map: Record<string, UptimeDayPoint[]> = {}
    for (const group of groups) {
      map[group.group] = toGroupUptimeSeries(group)
    }
    return map
  }, [groups])

  if (props.isLoading && !props.status) {
    return <Skeleton className='h-44 w-full' aria-label={t('Loading...')} />
  }
  if (!props.status) {
    return (
      <div className='text-muted-foreground rounded-lg border p-6 text-center text-sm'>
        {props.isError
          ? t('Performance data is unavailable.')
          : t('Performance data is not yet available for this model.')}
      </div>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
        {props.selectedGroup ? (
          <GroupBadge group={props.selectedGroup} size='sm' />
        ) : (
          <span>{t('All Groups')}</span>
        )}
        <span>{t('Request success rate sampled over the last 24 hours')}</span>
      </div>
      {props.isError && (
        <p role='status' className='text-muted-foreground text-xs'>
          {t('Performance update failed; showing the last available data.')}
        </p>
      )}
      <ModelStatusDetails
        model={props.status}
        generatedAt={props.generatedAt ?? initialTimestamp}
        hourFormatter={hourFormatter}
        numberFormatter={numberFormatter}
      />

      {!props.selectedGroup && (
        <section>
          <SectionHeader
            icon={HeartPulse}
            title={t('Per-group performance')}
            description={t('Average latency, TTFT, TPS, and success rate')}
          />
          {metricsQuery.isPending && (
            <Skeleton className='h-24 w-full' aria-label={t('Loading...')} />
          )}
          {metricsQuery.isError && (
            <p role='status' className='text-muted-foreground text-xs'>
              {t('Performance data is unavailable.')}
            </p>
          )}
          {metricsQuery.isSuccess && performances.length === 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('Performance data is not yet available for this model.')}
            </p>
          )}
          {metricsQuery.isSuccess && performances.length > 0 && (
            <StaticDataTable
              className='rounded-lg'
              tableClassName='text-sm'
              headerRowClassName={tableStyles.compactHeaderRow}
              data={performances}
              getRowKey={(perf) => perf.group}
              columns={[
                {
                  id: 'group',
                  header: t('Group'),
                  className: tableStyles.compactHeaderCell,
                  cellClassName: tableStyles.compactCell,
                  cell: (perf) => <GroupBadge group={perf.group} size='sm' />,
                },
                {
                  id: 'tps',
                  header: 'TPS',
                  className: tableStyles.compactHeaderCellRight,
                  cellClassName: tableStyles.compactNumericCell,
                  cell: (perf) => formatThroughput(perf.avg_tps),
                },
                {
                  id: 'ttft',
                  header: t('Average TTFT'),
                  className: tableStyles.compactHeaderCellRight,
                  cellClassName: tableStyles.compactNumericCell,
                  cell: (perf) => formatLatency(perf.avg_ttft_ms),
                },
                {
                  id: 'latency',
                  header: t('Average latency'),
                  className: tableStyles.compactHeaderCellRight,
                  cellClassName: tableStyles.compactMutedNumericCell,
                  cell: (perf) => formatLatency(perf.avg_latency_ms),
                },
                {
                  id: 'success',
                  header: t('Success rate'),
                  className: cn(tableStyles.compactHeaderCell, 'min-w-[180px]'),
                  cellClassName: tableStyles.compactCell,
                  cell: (perf) => (
                    <UptimeSparkline
                      size='sm'
                      series={uptimeByGroup[perf.group] ?? []}
                    />
                  ),
                },
              ]}
            />
          )}
        </section>
      )}

      {latencySeries.length > 0 && (
        <section>
          <SectionHeader
            icon={Timer}
            title={t('Latency trend (last 24h)')}
            description={t('Average TTFT')}
          />
          <LatencyTrendChart series={latencySeries} />
        </section>
      )}
    </div>
  )
}

function SectionHeader(props: {
  icon: React.ComponentType<{ className?: string }>
  title: string
  description?: string
  accent?: React.ReactNode
}) {
  const Icon = props.icon
  return (
    <div className='mb-2 flex flex-wrap items-center justify-between gap-2'>
      <div className='flex min-w-0 items-center gap-2'>
        <Icon className='text-muted-foreground/70 size-3.5 shrink-0' />
        <div className='min-w-0'>
          <div className='text-foreground text-sm font-semibold'>
            {props.title}
          </div>
          {props.description && (
            <p className='text-muted-foreground/80 text-xs'>
              {props.description}
            </p>
          )}
        </div>
      </div>
      {props.accent && (
        <div className='shrink-0 text-xs font-medium'>{props.accent}</div>
      )}
    </div>
  )
}
