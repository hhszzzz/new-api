import { ArrowLeft01Icon, ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { VChart } from '@visactor/react-vchart'
import type { EventParamsDefinition } from '@visactor/vchart'
import { BarChart3, LogIn, Users } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useChartTheme } from '@/lib/use-chart-theme'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'

import { formatShare, formatTokens, formatUSD } from '../lib/format'
import {
  buildRankingPieSlices,
  findRankingUser,
  formatRankingUserTooltip,
  type RankingPieSlice,
} from '../lib/user-usage'
import type { RankingUser, RankingUserGroup, RankingUserUsage } from '../types'

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
  const [selectedRank, setSelectedRank] = useState<number>()
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
  const selectedUser = findRankingUser(props.usage, selectedRank) ?? users[0]
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
  const activeSliceKey =
    slices.find((slice) => slice.userRank === selectedUser?.rank)?.key ??
    (selectedUser ? slices.find((slice) => slice.isOther)?.key : undefined)
  const chartColourMap = useMemo(
    () =>
      Object.fromEntries(
        slices.map((slice) => {
          const colour = colourMap[slice.key] ?? '#94a3b8'
          if (!activeSliceKey || slice.key === activeSliceKey) {
            return [slice.key, colour]
          }
          const red = Number.parseInt(colour.slice(1, 3), 16)
          const green = Number.parseInt(colour.slice(3, 5), 16)
          const blue = Number.parseInt(colour.slice(5, 7), 16)
          return [slice.key, `rgba(${red}, ${green}, ${blue}, 0.28)`]
        })
      ),
    [activeSliceKey, colourMap, slices]
  )
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
      tooltip: {
        style: { valueLabel: { multiLine: true } },
        mark: {
          title: {
            value: (datum: Record<string, unknown>) =>
              String(datum?.username ?? ''),
          },
          content: [
            {
              key: (datum: Record<string, unknown>) =>
                String(datum?.username ?? ''),
              value: (datum: Record<string, unknown>) => {
                const groups = rankingGroupsFromDatum(datum)
                return formatRankingUserTooltip(
                  rankingPieSliceFromDatum(datum, groups),
                  t
                ).split('\n')[0]
              },
            },
          ],
          updateContent: (previous: RankingTooltipLine[], data: unknown) => {
            const datum = findRankingTooltipDatum(data)
            if (!datum) return previous
            const groups = rankingGroupsFromDatum(datum)
            if (groups.length === 0) return previous
            const lines = formatRankingUserTooltip(
              rankingPieSliceFromDatum(datum, groups),
              t
            ).split('\n')
            const firstLine = previous[0] ?? {
              key: String(datum.username ?? ''),
              value: lines[0],
            }
            return [
              { ...firstLine, value: lines[0] },
              { key: lines[1], value: ' ' },
              ...lines.slice(2).map((line) => {
                const separator = line.indexOf(': ')
                return separator >= 0
                  ? {
                      key: line.slice(0, separator),
                      value: line.slice(separator + 2),
                    }
                  : { key: line, value: ' ' }
              }),
            ]
          },
        },
      },
    }
  }, [chartColourMap, displaySlices, t])

  const selectByIndex = (index: number) => {
    const user = users[index]
    if (!user) return
    setSelectedRank(user.rank)
    setPage(Math.floor(index / USER_PAGE_SIZE) + 1)
  }

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
                  key={`user-usage-${resolvedTheme}-${activeSliceKey ?? 'none'}`}
                  spec={{
                    ...chartSpec,
                    theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                    background: 'transparent',
                  }}
                  option={VCHART_OPTION}
                  onClick={(event: EventParamsDefinition['click']) => {
                    const rank = Number(event.datum?.rank)
                    if (Number.isInteger(rank)) {
                      const index = users.findIndex(
                        (user) => user.rank === rank
                      )
                      if (index >= 0) selectByIndex(index)
                    }
                  }}
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
            <div
              aria-label={t('Usage share by user')}
              className='mt-2 flex flex-wrap justify-center gap-x-3 gap-y-1'
            >
              {displaySlices.map((slice) => (
                <button
                  key={slice.key}
                  type='button'
                  aria-pressed={!slice.isOther && activeSliceKey === slice.key}
                  aria-label={
                    slice.isOther
                      ? t('Other')
                      : `${t('Usage share by user')}: ${slice.name}`
                  }
                  className={cn(
                    'text-muted-foreground inline-flex max-w-full items-center gap-1 truncate text-xs transition-colors hover:text-foreground',
                    !slice.isOther &&
                      activeSliceKey === slice.key &&
                      'text-foreground font-semibold'
                  )}
                  onClick={() => {
                    if (slice.userRank !== undefined) {
                      const index = users.findIndex(
                        (user) => user.rank === slice.userRank
                      )
                      if (index >= 0) selectByIndex(index)
                    }
                  }}
                  disabled={slice.isOther}
                >
                  <span
                    aria-hidden
                    className='size-2 shrink-0 rounded-full'
                    style={{ backgroundColor: colourMap[slice.key] }}
                  />
                  <span className='truncate'>{slice.name}</span>
                </button>
              ))}
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
              {pagedUserColumns.map((column, columnIndex) => (
                <ul
                  key={`column-${column[0]?.rank ?? 'empty'}`}
                  className='divide-border/70 divide-y'
                >
                  {column.map((user, rowIndex) => {
                    const userIndex =
                      (currentPage - 1) * USER_PAGE_SIZE +
                      columnIndex * userColumnSize +
                      rowIndex
                    return (
                      <li key={`${user.rank}-${user.username}`}>
                        <UserRow
                          user={user}
                          selected={selectedUser?.rank === user.rank}
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
                          onSelect={() => selectByIndex(userIndex)}
                          onMove={(direction) => {
                            const next =
                              direction === 'next'
                                ? Math.min(users.length - 1, userIndex + 1)
                                : Math.max(0, userIndex - 1)
                            selectByIndex(next)
                            const nextUser = users[next]
                            if (nextUser) {
                              window.requestAnimationFrame(() => {
                                document
                                  .querySelector<HTMLElement>(
                                    `#ranking-user-${nextUser.rank}`
                                  )
                                  ?.focus()
                              })
                            }
                          }}
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

type RankingTooltipLine = {
  key?: string
  value?: string
  [key: string]: unknown
}

function rankingPieSliceFromDatum(
  datum: Record<string, unknown>,
  groups: RankingUserGroup[]
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
    isOther: false,
    groups,
  }
}

function rankingGroupsFromDatum(
  datum: Record<string, unknown>
): RankingUserGroup[] {
  return Array.isArray(datum.groups) ? (datum.groups as RankingUserGroup[]) : []
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

function UserRow(props: {
  user: RankingUser
  selected: boolean
  colour?: string
  /** 0..1 share of the largest user, used to size the inline usage bar. */
  share: number
  onSelect: () => void
  onMove: (direction: 'next' | 'previous') => void
}) {
  const { t } = useTranslation()
  const displayUsername = localizeUsageLabel(props.user.username, t)
  const isPodium = props.user.rank <= 3
  return (
    <button
      id={`ranking-user-${props.user.rank}`}
      type='button'
      aria-pressed={props.selected}
      aria-label={t('Select {{user}}', { user: displayUsername })}
      className={cn(
        'focus-visible:ring-ring/50 group flex w-full items-center gap-3 rounded-md px-2 py-2.5 text-left outline-none transition-colors focus-visible:ring-2',
        'hover:bg-muted/40',
        props.selected && 'bg-muted/60'
      )}
      onClick={props.onSelect}
      onKeyDown={(event) => {
        if (event.key === 'ArrowDown') {
          event.preventDefault()
          props.onMove('next')
        }
        if (event.key === 'ArrowUp') {
          event.preventDefault()
          props.onMove('previous')
        }
      }}
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
        className={cn(
          'size-2.5 shrink-0 rounded-full',
          props.selected && 'ring-offset-background ring-2 ring-offset-1'
        )}
        style={{
          backgroundColor: props.colour ?? '#94a3b8',
          ...(props.selected
            ? { '--tw-ring-color': props.colour ?? '#94a3b8' }
            : {}),
        }}
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
    </button>
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
