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
import {
  Alert02Icon,
  Radar02Icon,
  RefreshIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import { getModelRadar } from './api'
import { CapabilityMatrix } from './components/capability-matrix'
import { DegradationAlerts } from './components/degradation-alerts'
import { EfficiencyScatter } from './components/efficiency-scatter'
import { HistoryComparison } from './components/history-comparison'
import { IQTrends } from './components/iq-trends'
import { useRadarFormatters } from './hooks/use-radar-formatters'

const REFRESH_INTERVAL_MS = 10 * 60 * 1000
const SOURCE_URL = 'https://codexradar.com'

export function ModelRadar() {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const radarQuery = useQuery({
    queryKey: ['model-radar'],
    queryFn: getModelRadar,
    refetchInterval: REFRESH_INTERVAL_MS,
    staleTime: REFRESH_INTERVAL_MS,
  })
  const snapshot = radarQuery.data?.data
  const updatedAt = snapshot ? format.dateTime(snapshot.fetched_at) : null
  const sourceUpdatedAt = snapshot
    ? format.dateTime(snapshot.source_updated_at)
    : null
  const initialLoading = radarQuery.isLoading && !radarQuery.isFetched
  const unavailable =
    radarQuery.isError &&
    isAxiosError(radarQuery.error) &&
    radarQuery.error.response?.status === 503
  const sourceURL = snapshot?.source.url || SOURCE_URL
  const hasConfigurations = (snapshot?.configurations.length ?? 0) > 0

  let content: React.ReactNode
  if (initialLoading) {
    content = <RadarLoading />
  } else if (radarQuery.isError && !snapshot) {
    content = (
      <RadarError
        unavailable={unavailable}
        isRetrying={radarQuery.isFetching}
        onRetry={() => void radarQuery.refetch()}
      />
    )
  } else if (!snapshot || !hasConfigurations) {
    content = <RadarEmpty />
  } else {
    content = (
      <>
        {snapshot.stale ? (
          <StaleNotice sourceUpdatedAt={sourceUpdatedAt} />
        ) : null}
        {radarQuery.isError ? <RefreshFailureNotice /> : null}
        <DegradationAlerts alerts={snapshot.degradation_alerts} />
        <CapabilityMatrix configurations={snapshot.configurations} />
        <EfficiencyScatter configurations={snapshot.configurations} />
        <IQTrends
          configurations={snapshot.configurations}
          history={snapshot.history}
        />
        <HistoryComparison
          configurations={snapshot.configurations}
          history={snapshot.history}
        />
      </>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-7xl px-3 pt-20 pb-10 sm:px-6 sm:pt-24 sm:pb-12 xl:px-8'>
        <main aria-busy={radarQuery.isFetching}>
          <header className='border-border/70 flex flex-col gap-4 border-b pb-5 sm:flex-row sm:items-end sm:justify-between'>
            <div className='min-w-0'>
              <div className='flex flex-wrap items-center gap-2'>
                <HugeiconsIcon
                  icon={Radar02Icon}
                  className='text-primary size-5'
                  strokeWidth={2}
                  aria-hidden='true'
                />
                <h1 className='text-xl font-semibold sm:text-2xl'>
                  {t('Model Radar')}
                </h1>
                {snapshot?.stale ? (
                  <Badge variant='destructive'>{t('Stale data')}</Badge>
                ) : null}
              </div>
              <p className='text-muted-foreground mt-1.5 max-w-2xl text-sm'>
                {t(
                  'Model capability, cost, and efficiency across reasoning efforts.'
                )}
              </p>
              <div className='text-muted-foreground mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
                <a
                  href={sourceURL}
                  target='_blank'
                  rel='noreferrer'
                  className='text-foreground decoration-border hover:decoration-foreground focus-visible:ring-ring underline underline-offset-4 focus-visible:ring-2 focus-visible:outline-none'
                >
                  {t('Data from Codex Radar codexradar.com')}
                </a>
                {snapshot ? (
                  <span>
                    {t('{{models}} models, {{configurations}} configurations', {
                      models: snapshot.model_count,
                      configurations: snapshot.configuration_count,
                    })}
                  </span>
                ) : null}
              </div>
            </div>

            <div className='flex shrink-0 items-center gap-2 sm:justify-end'>
              {snapshot ? (
                <p
                  className='text-muted-foreground text-xs tabular-nums'
                  aria-live='polite'
                >
                  {t('Updated {{time}}', { time: updatedAt })}
                </p>
              ) : null}
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        type='button'
                        variant='outline'
                        size='icon-sm'
                        aria-label={
                          radarQuery.isFetching
                            ? t('Refreshing...')
                            : t('Refresh')
                        }
                        onClick={() => void radarQuery.refetch()}
                        disabled={radarQuery.isFetching}
                      />
                    }
                  >
                    <span
                      className={cn(
                        radarQuery.isFetching && 'motion-safe:animate-spin'
                      )}
                      aria-hidden='true'
                    >
                      <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{t('Refresh local snapshot')}</TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
          </header>

          {content}
        </main>
      </PageTransition>
    </PublicLayout>
  )
}

function StaleNotice(props: { sourceUpdatedAt: string | null }) {
  const { t } = useTranslation()
  return (
    <div
      role='status'
      className='mt-5 flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/8 px-4 py-3 text-amber-900 dark:text-amber-200'
    >
      <HugeiconsIcon
        icon={Alert02Icon}
        className='mt-0.5 size-4 shrink-0'
        strokeWidth={2}
        aria-hidden='true'
      />
      <div>
        <p className='text-sm font-medium'>{t('This snapshot is stale')}</p>
        <p className='mt-0.5 text-xs opacity-80'>
          {t(
            'The last source update was {{time}}. Showing the latest valid data.',
            {
              time: props.sourceUpdatedAt ?? t('unknown'),
            }
          )}
        </p>
      </div>
    </div>
  )
}

function RefreshFailureNotice() {
  const { t } = useTranslation()
  return (
    <div
      role='alert'
      className='border-destructive/25 bg-destructive/5 mt-5 rounded-lg border px-4 py-3 text-sm'
    >
      {t('The latest refresh failed. Showing the previously loaded snapshot.')}
    </div>
  )
}

function RadarLoading() {
  const { t } = useTranslation()
  return (
    <div
      role='status'
      aria-label={t('Loading model radar')}
      aria-live='polite'
      className='py-6'
    >
      <span className='sr-only'>{t('Loading model radar')}</span>
      <div className='flex flex-col gap-6' aria-hidden='true'>
        <Skeleton className='h-24 w-full rounded-lg' />
        <div>
          <Skeleton className='mb-3 h-5 w-40' />
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6'>
            {Array.from({ length: 12 }, (_, index) => (
              <Skeleton key={index} className='h-24 rounded-lg' />
            ))}
          </div>
        </div>
        <Skeleton className='h-80 w-full rounded-lg' />
      </div>
    </div>
  )
}

function RadarError(props: {
  unavailable: boolean
  isRetrying: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  const title = props.unavailable
    ? t('Model radar data is not available yet')
    : t('Unable to load model radar')
  const description = props.unavailable
    ? t('The first upstream synchronization has not completed.')
    : t('The latest snapshot could not be loaded. Please try again.')
  return (
    <Empty className='my-6 min-h-64 border' role='alert' aria-label={title}>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <HugeiconsIcon
            icon={Radar02Icon}
            strokeWidth={2}
            aria-hidden='true'
          />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.isRetrying}
          onClick={props.onRetry}
        >
          <HugeiconsIcon
            icon={RefreshIcon}
            data-icon='inline-start'
            strokeWidth={2}
            aria-hidden='true'
          />
          {props.isRetrying ? t('Refreshing...') : t('Retry')}
        </Button>
      </EmptyContent>
    </Empty>
  )
}

function RadarEmpty() {
  const { t } = useTranslation()
  return (
    <Empty
      className='my-6 min-h-64 border'
      role='status'
      aria-label={t('No model radar data')}
    >
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <HugeiconsIcon
            icon={Radar02Icon}
            strokeWidth={2}
            aria-hidden='true'
          />
        </EmptyMedia>
        <EmptyTitle>{t('No model radar data')}</EmptyTitle>
        <EmptyDescription>
          {t('The current snapshot does not contain any model configurations.')}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
