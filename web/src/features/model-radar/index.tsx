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
import { RefreshIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useMemo } from 'react'
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
import { getPricing } from '@/features/pricing/api'

import { getModelRadar } from './api'
import { CapabilityMatrix } from './components/capability-matrix'
import { DegradationAlerts } from './components/degradation-alerts'
import { useRadarFormatters } from './hooks/use-radar-formatters'
import { createModelRadarIconRegistry } from './lib/model-radar'

const REFRESH_INTERVAL_MS = 10 * 60 * 1000
const PRICING_STALE_TIME_MS = 5 * 60 * 1000

export function ModelRadar() {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const radarQuery = useQuery({
    queryKey: ['model-radar'],
    queryFn: getModelRadar,
    refetchInterval: REFRESH_INTERVAL_MS,
    staleTime: REFRESH_INTERVAL_MS,
  })
  const pricingQuery = useQuery({
    queryKey: ['pricing'],
    queryFn: getPricing,
    staleTime: PRICING_STALE_TIME_MS,
  })
  const iconRegistry = useMemo(
    () => createModelRadarIconRegistry(pricingQuery.data),
    [pricingQuery.data]
  )
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
        <DegradationAlerts
          alerts={snapshot.degradation_alerts}
          history={snapshot.history}
          configurations={snapshot.configurations}
          iconRegistry={iconRegistry}
        />
        <CapabilityMatrix
          configurations={snapshot.configurations}
          iconRegistry={iconRegistry}
        />
      </>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-6xl px-3 pt-20 pb-10 sm:px-6 sm:pt-24 sm:pb-12 xl:px-8'>
        <main aria-busy={radarQuery.isFetching}>
          <header className='border-border/70 flex flex-col gap-4 border-b pb-5 sm:flex-row sm:items-end sm:justify-between'>
            <div className='min-w-0'>
              <div className='flex flex-wrap items-center gap-2.5'>
                <h1 className='text-xl font-semibold tracking-tight sm:text-2xl'>
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
              {snapshot ? (
                <p
                  className='text-muted-foreground mt-2 text-xs tabular-nums'
                  aria-live='polite'
                >
                  {t('{{models}} models, {{configurations}} configurations', {
                    models: snapshot.model_count,
                    configurations: snapshot.configuration_count,
                  })}
                  <span className='mx-1.5' aria-hidden='true'>
                    ·
                  </span>
                  {t('Updated {{time}}', { time: updatedAt })}
                  <span className='mx-1.5' aria-hidden='true'>
                    ·
                  </span>
                  <a
                    href={snapshot.source.url}
                    target='_blank'
                    rel='noreferrer'
                    className='text-foreground decoration-border hover:decoration-foreground focus-visible:ring-ring underline underline-offset-4 focus-visible:ring-2 focus-visible:outline-none'
                  >
                    {t('Data from Codex Radar codexradar.com')}
                  </a>
                </p>
              ) : null}
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
      className='mt-5 rounded-lg border border-amber-500/30 bg-amber-500/8 px-4 py-3 text-amber-900 dark:text-amber-200'
    >
      <p className='text-sm font-medium'>{t('This data is outdated')}</p>
      <p className='mt-0.5 text-xs opacity-80'>
        {t(
          'The last source update was {{time}}. Showing the latest valid data.',
          {
            time: props.sourceUpdatedAt ?? t('unknown'),
          }
        )}
      </p>
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
      <div className='flex flex-col gap-8' aria-hidden='true'>
        <div>
          <Skeleton className='mb-4 h-5 w-32' />
          <div className='grid gap-3 lg:grid-cols-2'>
            {Array.from({ length: 4 }, (_, index) => (
              <Skeleton key={index} className='h-32 rounded-xl' />
            ))}
          </div>
        </div>
        <div>
          <Skeleton className='mb-4 h-5 w-32' />
          <div className='flex flex-col gap-3'>
            {Array.from({ length: 3 }, (_, index) => (
              <Skeleton key={index} className='h-24 rounded-xl' />
            ))}
          </div>
        </div>
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
            icon={RefreshIcon}
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
            icon={RefreshIcon}
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
