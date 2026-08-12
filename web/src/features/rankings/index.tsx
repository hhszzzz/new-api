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
import { useNavigate, useSearch } from '@tanstack/react-router'
import type { DateRange } from 'react-day-picker'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuthStore } from '@/stores/auth-store'

import {
  MarketShareSection,
  ModelsSection,
  PulseSection,
  RankingsHero,
  UserUsageSection,
} from './components'
import { useRankings } from './hooks/use-rankings'
import {
  datesFromRankingQuery,
  defaultRankingDateRange,
  normalizeRankingDateRange,
  rankingQueryFromSearch,
} from './lib/range'
import type { RankingPeriod } from './types'

export function Rankings() {
  const { t } = useTranslation()
  const search = useSearch({ from: '/rankings/' })
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.auth.user)

  const period: RankingPeriod = search.period ?? 'week'

  const query = rankingQueryFromSearch({
    period,
    start_timestamp: search.start_timestamp,
    end_timestamp: search.end_timestamp,
  })
  const customRange = datesFromRankingQuery(query)
  const viewerKey = user ? `${user.id}:${user.role}` : 'anonymous'
  const rankingsQuery = useRankings(query, viewerKey)
  const snapshot = rankingsQuery.data?.data

  const handlePeriodChange = (next: RankingPeriod) => {
    if (next === 'custom') {
      const range = customRange ?? defaultRankingDateRange()
      const normalized = normalizeRankingDateRange(range)
      navigate({
        to: '/rankings',
        search: {
          period: 'custom',
          start_timestamp: normalized?.startTimestamp,
          end_timestamp: normalized?.endTimestamp,
        },
      })
      return
    }
    navigate({
      to: '/rankings',
      search: {
        period: next,
        start_timestamp: undefined,
        end_timestamp: undefined,
      },
    })
  }

  const handleCustomRangeChange = (range: DateRange | undefined) => {
    if (!range?.from || !range.to) return
    const normalized = normalizeRankingDateRange({
      from: range.from,
      to: range.to,
    })
    if (!normalized) return
    navigate({
      to: '/rankings',
      search: {
        period: 'custom',
        start_timestamp: normalized.startTimestamp,
        end_timestamp: normalized.endTimestamp,
      },
    })
  }

  return (
    <PublicLayout showMainContainer={false}>
      <div className='relative'>
        <div
          aria-hidden
          className='pointer-events-none absolute inset-x-0 top-0 h-[600px] opacity-20 dark:opacity-[0.10]'
          style={{
            background: [
              'radial-gradient(ellipse 60% 50% at 20% 20%, oklch(0.72 0.18 250 / 80%) 0%, transparent 70%)',
              'radial-gradient(ellipse 50% 40% at 80% 15%, oklch(0.65 0.15 200 / 60%) 0%, transparent 70%)',
              'radial-gradient(ellipse 40% 35% at 50% 70%, oklch(0.70 0.12 280 / 40%) 0%, transparent 70%)',
            ].join(', '),
            maskImage:
              'linear-gradient(to bottom, black 40%, transparent 100%)',
            WebkitMaskImage:
              'linear-gradient(to bottom, black 40%, transparent 100%)',
          }}
        />
        <PageTransition className='relative mx-auto w-full max-w-[1280px] space-y-8 px-3 pt-16 pb-10 sm:px-6 sm:pt-20 sm:pb-12 xl:px-8'>
          <RankingsHero
            period={period}
            customRange={customRange}
            onPeriodChange={handlePeriodChange}
            onCustomRangeChange={handleCustomRangeChange}
          />

          {rankingsQuery.isLoading && <RankingsLoading />}
          {!rankingsQuery.isLoading && !snapshot && (
            <RankingsError
              message={
                rankingsQuery.error instanceof Error
                  ? rankingsQuery.error.message
                  : t('Unable to load rankings data')
              }
            />
          )}
          {!rankingsQuery.isLoading && snapshot && (
            <>
              <ModelsSection
                history={snapshot.models_history}
                rows={snapshot.models}
                period={period}
                totalTokens={snapshot.total_tokens}
                totalUSD={snapshot.total_usd}
                bucket={snapshot.range.bucket}
              />

              <UserUsageSection
                usage={snapshot.user_usage}
                isAuthenticated={Boolean(user)}
              />

              <MarketShareSection
                history={snapshot.vendor_share_history}
                rows={snapshot.vendors}
                period={period}
                bucket={snapshot.range.bucket}
              />

              <PulseSection
                movers={snapshot.top_movers}
                droppers={snapshot.top_droppers}
              />
            </>
          )}
        </PageTransition>
      </div>
    </PublicLayout>
  )
}

function RankingsLoading() {
  const { t } = useTranslation()
  return (
    <div role='status' aria-label={t('Loading...')} className='space-y-6'>
      <span className='sr-only'>{t('Loading...')}</span>
      <Skeleton className='h-[420px] w-full rounded-xl' />
      <Skeleton className='h-[360px] w-full rounded-xl' />
      <Skeleton className='h-[520px] w-full rounded-xl' />
      <Skeleton className='h-[180px] w-full rounded-xl' />
    </div>
  )
}

function RankingsError(props: { message: string }) {
  const { t } = useTranslation()
  return (
    <div
      role='alert'
      aria-label={t('Unable to load rankings')}
      className='bg-card rounded-xl border border-dashed px-6 py-12 text-center'
    >
      <h2 className='text-foreground text-base font-semibold'>
        {t('Unable to load rankings')}
      </h2>
      <p className='text-muted-foreground mx-auto mt-2 max-w-md text-sm'>
        {props.message}
      </p>
    </div>
  )
}
