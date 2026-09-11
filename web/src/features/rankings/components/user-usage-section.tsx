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
import { ArrowLeft01Icon, ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { VChart } from '@visactor/react-vchart'
import { BarChart3, LogIn, Users } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useChartTheme } from '@/lib/use-chart-theme'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'

import { formatShare, formatTokens, formatUSD } from '../lib/format'
import {
  buildRankingPieSlices,
  rankingBreakdownRows,
  type RankingBreakdownMode,
  type RankingPieSlice,
} from '../lib/user-usage'
import type {
  RankingUser,
  RankingUserGroup,
  RankingUserModel,
  RankingUserUsage,
} from '../types'

const USER_PAGE_SIZE = 10

const USER_COLOURS = [
  '#0ea5e9',
  '#10b981',
  '#f97316',
  '#8b5cf6',
  '#e11d48',
  '#14b8a6',
  '#eab308',
  '#6366f1',
  '#ec4899',
  '#84cc16',
  '#94a3b8',
]

type UserUsageSectionProps = {
  usage?: RankingUserUsage
  isAuthenticated: boolean
}

export function UserUsageSection(props: UserUsageSectionProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const [page, setPage] = useState(1)
  const users = props.usage?.users ?? EMPTY_USERS
  const totalPages = Math.max(1, Math.ceil(users.length / USER_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const pagedUsers = useMemo(() => {
    const start = (currentPage - 1) * USER_PAGE_SIZE
    return users.slice(start, start + USER_PAGE_SIZE)
  }, [currentPage, users])
  // The inline usage bars are relative to the biggest spender, so the leader
  // fills the bar and every other row reads as a fraction of it.
  const topUserUSD = users.reduce(
    (max, user) => Math.max(max, user.total_usd),
    0
  )
  const userColumnSize = Math.ceil(pagedUsers.length / 2)
  const pagedUserColumns = [
    pagedUsers.slice(0, userColumnSize),
    pagedUsers.slice(userColumnSize),
  ]
  const slices = useMemo(
    () =>
      props.usage ? buildRankingPieSlices(props.usage, 5, t('Other')) : [],
    [props.usage, t]
  )
  const displaySlices = useMemo(
    () =>
      slices.map((slice) => ({
        ...slice,
        name: localizeUsageLabel(slice.name, t),
        groups: slice.groups.map((group) => ({
          ...group,
          use_group: localizeUsageLabel(group.use_group, t),
        })),
        models: slice.models,
      })),
    [slices, t]
  )
  const colourMap = useMemo(
    () =>
      Object.fromEntries(
        slices.map((slice, index) => [
          slice.key,
          USER_COLOURS[index % USER_COLOURS.length],
        ])
      ),
    [slices]
  )
  const chartColourMap = colourMap
  const chartSpec = useMemo(() => {
    if (displaySlices.length === 0) return null
    return {
      type: 'pie' as const,
      data: [
        {
          id: 'ranking-user-usage',
          values: displaySlices.map((slice) => ({
            sliceKey: slice.key,
            username: slice.name,
            rank: slice.userRank,
            quota: slice.quota,
            usd: slice.usd,
            share: slice.share,
            groups: slice.groups,
            models: slice.models,
          })),
        },
      ],
      valueField: 'quota',
      categoryField: 'sliceKey',
      outerRadius: 0.86,
      innerRadius: 0.58,
      color: { specified: chartColourMap },
      legends: { visible: false },
      label: { visible: false },
      // The tooltip is fully custom (React portal) so the by-group/by-model
      // toggle is clickable inside the hover card; `enterable` keeps the card
      // open while the pointer moves onto it. The default panel still casts a
      // sharp-cornered shadow halo around the rounded card unless its shadow
      // and border are neutralized here.
      tooltip: {
        enterable: true,
        style: {
          panel: {
            padding: 0,
            backgroundColor: 'transparent',
            border: { width: 0, radius: 0 },
            shadow: { x: 0, y: 0, blur: 0, spread: 0, color: 'transparent' },
          },
        },
        tooltipRender: (
          _element: HTMLElement,
          actualTooltip: unknown,
          params: unknown
        ) => (
          <UserUsageTooltipCard
            datum={
              findRankingTooltipDatum(actualTooltip) ??
              findRankingTooltipDatum(params)
            }
          />
        ),
      },
    }
  }, [chartColourMap, displaySlices])

  const changePage = (nextPage: number) => {
    setPage(Math.min(Math.max(nextPage, 1), totalPages))
  }

  return (
    <section className='bg-card overflow-hidden rounded-lg border'>
      <header className='flex items-start justify-between gap-4 border-b px-5 py-4'>
        <div className='min-w-0'>
          <h2 className='text-foreground inline-flex items-center gap-2 text-base font-semibold'>
            <Users className='text-primary size-4' />
            {t('User usage')}
          </h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Usage by user, charged amount, and request group')}
          </p>
        </div>
        {props.usage && (
          <div className='shrink-0 text-right'>
            <div className='text-foreground font-mono text-sm font-semibold tabular-nums'>
              {formatUSD(props.usage.total_usd)}
            </div>
            <div className='text-muted-foreground/80 font-mono text-[11px] tabular-nums'>
              {formatTokens(props.usage.total_tokens)} {t('tokens')}
            </div>
          </div>
        )}
      </header>

      {!props.isAuthenticated && (
        <div className='flex flex-col items-center gap-3 px-5 py-12 text-center'>
          <p className='text-muted-foreground text-sm'>
            {t('Sign in to view usage by user')}
          </p>
          <Button
            variant='outline'
            render={<Link to='/sign-in' search={{ redirect: '/rankings' }} />}
          >
            <LogIn data-icon='inline-start' />
            {t('Sign in')}
          </Button>
        </div>
      )}
      {props.isAuthenticated && users.length === 0 && (
        <div className='text-muted-foreground/80 px-5 py-12 text-center text-sm'>
          {t('No user usage data available')}
        </div>
      )}
      {props.isAuthenticated && users.length > 0 && (
        <div className='grid grid-cols-1 gap-6 p-5 lg:grid-cols-[minmax(220px,0.8fr)_minmax(0,1.2fr)]'>
          <div className='min-w-0'>
            <div className='relative mx-auto h-64 max-w-xs'>
              {themeReady && chartSpec ? (
                <VChart
                  key={`user-usage-${resolvedTheme}`}
                  spec={{
                    ...chartSpec,
                    theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                    background: 'transparent',
                  }}
                  option={VCHART_OPTION}
                />
              ) : (
                <div className='text-muted-foreground/80 flex h-full items-center justify-center text-xs'>
                  {t('No user usage data available')}
                </div>
              )}
              <div className='pointer-events-none absolute inset-0 flex flex-col items-center justify-center'>
                <span className='text-foreground font-mono text-lg font-semibold tabular-nums'>
                  {formatUSD(props.usage?.total_usd ?? 0)}
                </span>
                <span className='text-muted-foreground text-[10px] uppercase'>
                  {t('charged')}
                </span>
              </div>
            </div>
          </div>

          <div className='min-w-0'>
            <div className='mb-2 flex items-center justify-between gap-3'>
              <h3 className='text-foreground inline-flex items-center gap-2 text-sm font-semibold'>
                <BarChart3 className='size-3.5 text-sky-500' />
                {t('Users ranked by charged amount')}
              </h3>
              <span className='text-muted-foreground/80 text-xs'>
                {t('Top {{count}}', { count: users.length })}
              </span>
            </div>
            <div
              id='ranking-user-list'
              className='grid grid-cols-1 gap-x-8 md:grid-cols-2'
            >
              {pagedUserColumns.map((column) => (
                <ul
                  key={`column-${column[0]?.rank ?? 'empty'}`}
                  className='divide-border/70 divide-y'
                >
                  {column.map((user) => {
                    return (
                      <li key={`${user.rank}-${user.username}`}>
                        <UserRow
                          user={user}
                          share={
                            topUserUSD > 0 ? user.total_usd / topUserUSD : 0
                          }
                          colour={
                            colourMap[
                              slices.find(
                                (slice) => slice.userRank === user.rank
                              )?.key ?? 'other'
                            ]
                          }
                        />
                      </li>
                    )
                  })}
                </ul>
              ))}
            </div>
            {totalPages > 1 && (
              <nav
                className='text-muted-foreground mt-3 flex flex-col items-center justify-between gap-3 border-t pt-3 text-sm sm:flex-row'
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
                    aria-controls='ranking-user-list'
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
                    aria-controls='ranking-user-list'
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
            )}
          </div>
        </div>
      )}
    </section>
  )
}

const EMPTY_USERS: RankingUser[] = []

function rankingSliceFromDatum(
  datum: Record<string, unknown>
): RankingPieSlice {
  return {
    key: String(datum.sliceKey ?? ''),
    name: String(datum.username ?? ''),
    userRank: Number.isInteger(Number(datum.rank))
      ? Number(datum.rank)
      : undefined,
    quota: Number(datum.quota) || 0,
    usd: Number(datum.usd) || 0,
    share: Number(datum.share) || 0,
    isOther: datum.sliceKey === 'other',
    groups: Array.isArray(datum.groups)
      ? (datum.groups as RankingUserGroup[])
      : [],
    models: Array.isArray(datum.models)
      ? (datum.models as RankingUserModel[])
      : [],
  }
}

// Shared breakdown content for the pie hover card and the ranked-row
// popover: the header carries the user and their share exactly once, and the
// clickable toggle switches rows between request groups and models. The
// selected mode is component state; in the chart tooltip the portal
// reconciles into the same DOM node across hovers, so the choice persists
// while browsing slices.
function UserUsageBreakdown(props: {
  name: string
  share: number
  usd: number
  groups: RankingUserGroup[]
  models: RankingUserModel[]
  // Compact mode (pie hover card) moves the breakdown toggle into the
  // header row and drops the share/amount summary to save vertical space;
  // the ranked-row popover keeps the summary with the toggle on its own row.
  compact?: boolean
}) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<RankingBreakdownMode>('group')
  const rows = rankingBreakdownRows(props, mode)
  const hasBreakdown = props.groups.length > 0 || props.models.length > 0
  const breakdownToggle = hasBreakdown && (
    <ToggleGroup
      className={props.compact ? 'shrink-0' : 'mt-2'}
      value={[mode]}
      onValueChange={(values) => {
        if (values[0] === 'group' || values[0] === 'model') {
          setMode(values[0])
        }
      }}
      variant='outline'
      size='sm'
      aria-label={t('Usage breakdown')}
    >
      <ToggleGroupItem value='group'>{t('By group')}</ToggleGroupItem>
      <ToggleGroupItem value='model'>{t('By model')}</ToggleGroupItem>
    </ToggleGroup>
  )
  return (
    <>
      <div className='flex items-center justify-between gap-2'>
        <span className='truncate text-sm font-semibold'>
          {localizeUsageLabel(props.name, t)}
        </span>
        {props.compact ? (
          breakdownToggle
        ) : (
          <span className='shrink-0 font-mono text-xs tabular-nums'>
            {formatShare(props.share)} · {formatUSD(props.usd)}
          </span>
        )}
      </div>
      {!props.compact && breakdownToggle}
      {rows.length > 0 && (
        <div className='border-border/70 mt-2 flex flex-col gap-1 border-t pt-2'>
          {rows.map((row) => (
            <div
              key={row.label}
              className='flex items-center justify-between gap-4'
            >
              <span className='text-muted-foreground truncate'>
                {row.label}
              </span>
              <span className='shrink-0 font-mono tabular-nums'>
                {formatTokens(row.tokens)} · {formatUSD(row.usd)}
              </span>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

// VChart provides mark tooltip data through a few nested shapes depending on
// the renderer; find the first datum carrying the user slice fields.
function findRankingTooltipDatum(
  value: unknown
): Record<string, unknown> | undefined {
  if (Array.isArray(value)) {
    for (const item of value) {
      const datum = findRankingTooltipDatum(item)
      if (datum) return datum
    }
    return undefined
  }
  if (!value || typeof value !== 'object') return undefined
  const record = value as Record<string, unknown>
  if ('sliceKey' in record) return record
  if ('datum' in record) return findRankingTooltipDatum(record.datum)
  if ('data' in record) return findRankingTooltipDatum(record.data)
  return undefined
}

// Custom hover card for a pie slice.
export function UserUsageTooltipCard(props: {
  datum?: Record<string, unknown>
}) {
  const slice = rankingSliceFromDatum(props.datum ?? {})
  return (
    <div className='bg-popover text-popover-foreground ring-foreground/10 w-64 rounded-lg p-2.5 text-xs shadow-md ring-1'>
      <UserUsageBreakdown
        name={slice.name}
        share={slice.share}
        usd={slice.usd}
        groups={slice.groups}
        models={slice.models}
        compact
      />
    </div>
  )
}

function UserRow(props: {
  user: RankingUser
  colour?: string
  /** 0..1 share of the largest user, used to size the inline usage bar. */
  share: number
}) {
  const { t } = useTranslation()
  const displayUsername = localizeUsageLabel(props.user.username, t)
  const isPodium = props.user.rank <= 3
  const groups = props.user.groups.map((group) => ({
    ...group,
    use_group: localizeUsageLabel(group.use_group, t),
  }))
  return (
    <Popover>
      <PopoverTrigger
        id={`ranking-user-${props.user.rank}`}
        className='hover:bg-muted/60 flex w-full cursor-pointer items-center gap-3 rounded-md px-2 py-2.5 text-left transition-colors'
      >
        <span
          className={cn(
            'w-6 shrink-0 text-right font-mono text-xs tabular-nums',
            isPodium
              ? 'text-foreground font-semibold'
              : 'text-muted-foreground/80'
          )}
        >
          {props.user.rank}.
        </span>
        <span
          aria-hidden
          className='size-2.5 shrink-0 rounded-full'
          style={{ backgroundColor: props.colour ?? '#94a3b8' }}
        />
        <span className='min-w-0 flex-1 truncate'>
          <span className='text-foreground truncate text-sm font-medium'>
            {displayUsername}
          </span>
          <span
            aria-hidden
            className='bg-muted/70 mt-1 block h-1 w-full overflow-hidden rounded-full'
          >
            <span
              className='block h-full rounded-full'
              style={{
                width: `${Math.max(2, Math.min(100, props.share * 100))}%`,
                backgroundColor: props.colour ?? '#94a3b8',
              }}
            />
          </span>
        </span>
        <span className='shrink-0 text-right'>
          <span className='text-foreground block font-mono text-sm font-semibold tabular-nums'>
            {formatUSD(props.user.total_usd)}
          </span>
          <span className='text-muted-foreground/80 block font-mono text-[11px] tabular-nums'>
            {formatTokens(props.user.total_tokens)} {t('tokens')} ·{' '}
            {formatShare(props.user.quota_share)}
          </span>
        </span>
      </PopoverTrigger>
      <PopoverContent
        side='left'
        align='center'
        className='w-64 gap-0 p-2.5 text-xs'
      >
        <UserUsageBreakdown
          name={displayUsername}
          share={props.user.quota_share}
          usd={props.user.total_usd}
          groups={groups}
          models={props.user.models ?? []}
        />
      </PopoverContent>
    </Popover>
  )
}

function localizeUsageLabel(
  label: string,
  translate: (key: string) => string
): string {
  if (label === 'Other users' || label === 'Unknown') {
    return translate(label)
  }
  return label
}
