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
  Activity01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  RefreshIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import { getModelStatus } from './api'
import { ModelStatusCard } from './components/model-status-card'
import {
  getModelStatusContentKind,
  sortModelStatuses,
} from './lib/model-status'

const REFRESH_INTERVAL_MS = 60 * 1000
const REFRESH_COOLDOWN_MS = 5 * 1000
const MODEL_STATUS_PAGE_SIZE = 20

export function ModelStatus() {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [isRefreshCoolingDown, setIsRefreshCoolingDown] = useState(false)
  const modelListRef = useRef<HTMLElement>(null)
  const refreshCooldownTimerRef = useRef<number | null>(null)
  const statusQuery = useQuery({
    queryKey: ['model-status'],
    queryFn: getModelStatus,
    refetchInterval: REFRESH_INTERVAL_MS,
    staleTime: REFRESH_INTERVAL_MS,
  })
  const snapshot = statusQuery.data?.data
  const models = useMemo(
    () => sortModelStatuses(snapshot?.models ?? []),
    [snapshot?.models]
  )
  const totalPages = Math.max(
    1,
    Math.ceil(models.length / MODEL_STATUS_PAGE_SIZE)
  )
  const currentPage = Math.min(page, totalPages)
  const pagedModels = useMemo(() => {
    const start = (currentPage - 1) * MODEL_STATUS_PAGE_SIZE
    return models.slice(start, start + MODEL_STATUS_PAGE_SIZE)
  }, [currentPage, models])

  useEffect(() => {
    setPage((current) => Math.min(current, totalPages))
  }, [totalPages])

  useEffect(
    () => () => {
      if (refreshCooldownTimerRef.current !== null) {
        window.clearTimeout(refreshCooldownTimerRef.current)
      }
    },
    []
  )

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
  const updatedAtFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(locale, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      }),
    [locale]
  )
  const updatedAt = snapshot
    ? updatedAtFormatter.format(new Date(snapshot.generated_at * 1000))
    : null
  const isInitialLoading = statusQuery.isLoading && !statusQuery.isFetched
  const isRefreshPending = statusQuery.isFetching && statusQuery.isFetched
  const contentKind = getModelStatusContentKind(
    snapshot,
    isInitialLoading,
    statusQuery.isError
  )
  const changePage = (nextPage: number) => {
    setPage(Math.min(Math.max(nextPage, 1), totalPages))
    modelListRef.current?.scrollIntoView({ block: 'start' })
  }
  const refreshStatus = () => {
    if (statusQuery.isFetching || isRefreshCoolingDown) return

    setIsRefreshCoolingDown(true)
    void statusQuery.refetch()
    refreshCooldownTimerRef.current = window.setTimeout(() => {
      setIsRefreshCoolingDown(false)
      refreshCooldownTimerRef.current = null
    }, REFRESH_COOLDOWN_MS)
  }
  let statusContent: React.ReactNode

  if (contentKind === 'loading') {
    statusContent = <StatusLoading />
  } else if (contentKind === 'error' || !snapshot) {
    statusContent = (
      <StatusError
        isRetrying={statusQuery.isFetching}
        onRetry={() => void statusQuery.refetch()}
      />
    )
  } else if (contentKind === 'empty') {
    statusContent = (
      <Empty
        className='border py-14'
        role='status'
        aria-label={t('No models available')}
      >
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon
              icon={Activity01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
          </EmptyMedia>
          <EmptyTitle>{t('No models available')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  } else {
    statusContent = (
      <>
        <section
          ref={modelListRef}
          id='model-status-list'
          className='grid scroll-mt-20 gap-3 sm:scroll-mt-24 md:grid-cols-2'
          aria-label={t('Model Status')}
        >
          {pagedModels.map((model) => (
            <ModelStatusCard
              key={model.model_name}
              model={model}
              generatedAt={snapshot.generated_at}
              hourFormatter={hourFormatter}
            />
          ))}
        </section>

        {totalPages > 1 ? (
          <nav
            className='text-muted-foreground mt-4 flex flex-col items-center justify-between gap-3 border-t pt-4 text-sm sm:flex-row'
            aria-label={t('Page')}
          >
            <p aria-live='polite'>
              {t('Page {{current}} of {{total}}', {
                current: currentPage,
                total: totalPages,
              })}
            </p>
            <div className='flex items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                aria-controls='model-status-list'
                onClick={() => changePage(currentPage - 1)}
                disabled={currentPage <= 1}
              >
                <HugeiconsIcon
                  icon={ArrowLeft01Icon}
                  strokeWidth={2}
                  data-icon='inline-start'
                  aria-hidden='true'
                />
                {t('Previous page')}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                aria-controls='model-status-list'
                onClick={() => changePage(currentPage + 1)}
                disabled={currentPage >= totalPages}
              >
                {t('Next page')}
                <HugeiconsIcon
                  icon={ArrowRight01Icon}
                  strokeWidth={2}
                  data-icon='inline-end'
                  aria-hidden='true'
                />
              </Button>
            </div>
          </nav>
        ) : null}
      </>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-6xl px-3 pt-20 pb-10 sm:px-6 sm:pt-24 sm:pb-12 xl:px-8'>
        <main aria-busy={statusQuery.isFetching}>
          <header className='border-border/70 mb-5 flex flex-col gap-4 border-b pb-5 sm:flex-row sm:items-end sm:justify-between'>
            <div>
              <div className='flex items-center gap-2'>
                <HugeiconsIcon
                  icon={Activity01Icon}
                  className='text-primary size-5'
                  strokeWidth={2}
                  aria-hidden='true'
                />
                <h1 className='text-xl font-semibold tracking-tight sm:text-2xl'>
                  {t('Model Status')}
                </h1>
              </div>
              <p className='text-muted-foreground mt-1.5 max-w-2xl text-sm'>
                {t(
                  'Live health from real API requests over the last 24 hours.'
                )}
              </p>
            </div>

            <div className='flex flex-wrap items-center gap-3 sm:justify-end'>
              {snapshot ? (
                <p
                  className='text-muted-foreground text-xs tabular-nums'
                  aria-live='polite'
                >
                  {t('Updated {{time}}', { time: updatedAt })}
                </p>
              ) : null}
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={refreshStatus}
                disabled={statusQuery.isFetching || isRefreshCoolingDown}
              >
                <span
                  data-icon='inline-start'
                  className={cn(
                    statusQuery.isFetching && 'motion-safe:animate-spin'
                  )}
                  aria-hidden='true'
                >
                  <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
                </span>
                {isRefreshPending ? t('Refreshing...') : t('Refresh')}
              </Button>
            </div>
          </header>

          {statusContent}
        </main>
      </PageTransition>
    </PublicLayout>
  )
}

function StatusLoading() {
  const { t } = useTranslation()

  return (
    <div role='status' aria-label={t('Loading...')} aria-live='polite'>
      <span className='sr-only'>{t('Loading...')}</span>
      <div className='grid gap-3 md:grid-cols-2' aria-hidden='true'>
        {Array.from({ length: 4 }, (_, index) => (
          <Card key={index} size='sm'>
            <CardHeader>
              <div className='flex flex-col gap-2'>
                <Skeleton className='h-3 w-20' />
                <Skeleton className='h-5 w-44' />
              </div>
              <CardAction>
                <Skeleton className='h-5 w-20 rounded-full' />
              </CardAction>
            </CardHeader>
            <CardContent className='flex flex-col gap-4'>
              <div className='grid grid-cols-3 gap-2'>
                <Skeleton className='h-14' />
                <Skeleton className='h-14' />
                <Skeleton className='h-14' />
              </div>
              <Skeleton className='h-8 w-full' />
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}

function StatusError(props: { isRetrying: boolean; onRetry: () => void }) {
  const { t } = useTranslation()

  return (
    <Empty
      className='border py-12'
      role='alert'
      aria-label={t('Unable to load model status')}
    >
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <HugeiconsIcon
            icon={RefreshIcon}
            strokeWidth={2}
            aria-hidden='true'
          />
        </EmptyMedia>
        <EmptyTitle>{t('Unable to load model status')}</EmptyTitle>
        <EmptyDescription>
          {t('Check your connection and try again.')}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={props.onRetry}
          disabled={props.isRetrying}
        >
          <span
            data-icon='inline-start'
            className={cn(props.isRetrying && 'motion-safe:animate-spin')}
            aria-hidden='true'
          >
            <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          </span>
          {props.isRetrying ? t('Refreshing...') : t('Retry')}
        </Button>
      </EmptyContent>
    </Empty>
  )
}
