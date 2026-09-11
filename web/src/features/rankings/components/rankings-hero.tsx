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
import { CalendarDays } from 'lucide-react'
import { useState } from 'react'
import { enUS, fr, ja, ru, vi, zhCN, zhTW } from 'react-day-picker/locale'
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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import {
  defaultRankingDateRange,
  MAX_RANKING_CUSTOM_DAYS,
  type RankingDateRange,
} from '../lib/range'
import type { RankingPeriod } from '../types'

const PERIODS: { id: RankingPeriod; labelKey: string }[] = [
  { id: 'today', labelKey: 'Today' },
  { id: 'week', labelKey: 'Week' },
  { id: 'month', labelKey: 'Month' },
  { id: 'year', labelKey: 'Year' },
  { id: 'custom', labelKey: 'Custom' },
]

const calendarLocales = {
  en: enUS,
  fr,
  ja,
  ru,
  vi,
  zhCN,
  zhTW,
  zh: zhCN,
  'zh-TW': zhTW,
} as const

type CustomRange = { from: Date; to: Date }

type RankingsHeroProps = {
  period: RankingPeriod
  customRange?: RankingDateRange
  onPeriodChange: (period: RankingPeriod) => void
  onCustomRangeChange: (range: CustomRange) => void
}

/**
 * The start and end dates are picked independently, one calendar per side.
 * Falls back to the default window when the URL has no complete custom range
 * yet, so each picker always shows a usable date.
 */
function resolveCustomRange(range?: RankingDateRange): CustomRange {
  if (range?.from && range.to) {
    return { from: range.from, to: range.to }
  }
  const fallback = defaultRankingDateRange()
  return { from: fallback.from, to: fallback.to }
}

function startOfLocalDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate())
}

function addLocalDays(date: Date, days: number): Date {
  const day = startOfLocalDay(date)
  day.setDate(day.getDate() + days)
  return day
}

/**
 * Disabled dates for one side of the custom range: never in the future, never
 * past the opposite anchor's day, and never beyond the inclusive day cap
 * (MAX_RANKING_CUSTOM_DAYS closed days per the backend limit).
 */
function isDisabledForPicker(
  date: Date,
  anchor: Date,
  side: 'start' | 'end'
): boolean {
  if (date > new Date()) return true
  const day = startOfLocalDay(date)
  const anchorDay = startOfLocalDay(anchor)
  if (side === 'start') {
    if (day > anchorDay) return true
    return day < addLocalDays(anchorDay, -(MAX_RANKING_CUSTOM_DAYS - 1))
  }
  if (day < anchorDay) return true
  return day > addLocalDays(anchorDay, MAX_RANKING_CUSTOM_DAYS - 1)
}

/**
 * Hero strip for the rankings page: title + subtitle + period tabs, plus the
 * separate start/end date pickers shown for the custom period.
 */
export function RankingsHero(props: RankingsHeroProps) {
  const { t, i18n } = useTranslation()
  const language = i18n.resolvedLanguage ?? i18n.language
  const calendarLocale =
    calendarLocales[language as keyof typeof calendarLocales] ??
    calendarLocales[language.split('-')[0] as keyof typeof calendarLocales] ??
    enUS
  const [openPicker, setOpenPicker] = useState<'start' | 'end' | null>(null)
  const effectiveRange = resolveCustomRange(props.customRange)

  const handleSelect = (picker: 'start' | 'end', date: Date | undefined) => {
    if (!date) return
    props.onCustomRangeChange(
      picker === 'start'
        ? { from: date, to: effectiveRange.to }
        : { from: effectiveRange.from, to: date }
    )
    setOpenPicker(null)
  }

  const daysFooter = (
    <p className='text-muted-foreground px-2 pb-2 text-xs'>
      {t('Up to {{count}} days', {
        count: MAX_RANKING_CUSTOM_DAYS,
      })}
    </p>
  )

  return (
    <section className='space-y-5'>
      <div className='space-y-2'>
        <h1 className='text-[clamp(1.75rem,4vw,2.5rem)] leading-[1.15] font-bold tracking-tight'>
          {t('Rankings')}
        </h1>
        <p className='text-muted-foreground/80 max-w-2xl text-sm'>
          {t(
            'Discover the most-used models and rising vendors on the platform, updated from live usage data.'
          )}
        </p>
      </div>

      {/* Underline tabs for period — clean and unobtrusive. */}
      <div
        role='tablist'
        aria-label={t('Period')}
        className='border-border/60 flex items-center border-b'
      >
        {PERIODS.map((p) => {
          const isActive = props.period === p.id
          return (
            <button
              key={p.id}
              role='tab'
              type='button'
              aria-selected={isActive}
              onClick={() => props.onPeriodChange(p.id)}
              className={cn(
                'focus-visible:ring-ring/40 relative -mb-px rounded-sm px-3 py-2 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none',
                isActive
                  ? 'text-foreground'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {t(p.labelKey)}
              <span
                aria-hidden
                className={cn(
                  'bg-foreground absolute inset-x-3 -bottom-px h-[2px] rounded-full transition-opacity',
                  isActive ? 'opacity-100' : 'opacity-0'
                )}
              />
            </button>
          )
        })}
      </div>

      {props.period === 'custom' && (
        <div className='flex flex-wrap items-center gap-2'>
          <Popover
            open={openPicker === 'start'}
            onOpenChange={(open) => setOpenPicker(open ? 'start' : null)}
          >
            <PopoverTrigger
              render={
                <Button
                  variant='outline'
                  className='justify-start text-start'
                  aria-label={t('Start date')}
                />
              }
            >
              <CalendarDays data-icon='inline-start' />
              <span className='min-w-0 truncate'>
                {formatDate(effectiveRange.from, language)}
              </span>
            </PopoverTrigger>
            <PopoverContent className='w-auto p-0' align='start'>
              <Calendar
                mode='single'
                selected={effectiveRange.from}
                defaultMonth={effectiveRange.from}
                onSelect={(date) => handleSelect('start', date)}
                numberOfMonths={1}
                locale={calendarLocale}
                disabled={(date: Date) =>
                  isDisabledForPicker(date, effectiveRange.to, 'start')
                }
                footer={daysFooter}
              />
            </PopoverContent>
          </Popover>

          <span aria-hidden className='text-muted-foreground text-sm'>
            –
          </span>

          <Popover
            open={openPicker === 'end'}
            onOpenChange={(open) => setOpenPicker(open ? 'end' : null)}
          >
            <PopoverTrigger
              render={
                <Button
                  variant='outline'
                  className='justify-start text-start'
                  aria-label={t('End date')}
                />
              }
            >
              <CalendarDays data-icon='inline-start' />
              <span className='min-w-0 truncate'>
                {formatDate(effectiveRange.to, language)}
              </span>
            </PopoverTrigger>
            <PopoverContent className='w-auto p-0' align='start'>
              <Calendar
                mode='single'
                selected={effectiveRange.to}
                defaultMonth={effectiveRange.to}
                onSelect={(date) => handleSelect('end', date)}
                numberOfMonths={1}
                locale={calendarLocale}
                disabled={(date: Date) =>
                  isDisabledForPicker(date, effectiveRange.from, 'end')
                }
                footer={daysFooter}
              />
            </PopoverContent>
          </Popover>
        </div>
      )}
    </section>
  )
}

function formatDate(date: Date, language: string): string {
  return new Intl.DateTimeFormat(toIntlLocale(language) ?? 'en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  }).format(date)
}
