import { CalendarDays } from 'lucide-react'
import { useEffect, useState } from 'react'
import type { DateRange } from 'react-day-picker'
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

import { MAX_RANKING_CUSTOM_DAYS, type RankingDateRange } from '../lib/range'
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

type RankingsHeroProps = {
  period: RankingPeriod
  customRange?: RankingDateRange
  onPeriodChange: (period: RankingPeriod) => void
  onCustomRangeChange: (range: DateRange | undefined) => void
}

/**
 * Hero strip for the rankings page. Intentionally minimal — title +
 * subtitle + period tabs only.
 */
export function RankingsHero(props: RankingsHeroProps) {
  const { t, i18n } = useTranslation()
  const language = i18n.resolvedLanguage ?? i18n.language
  const calendarLocale =
    calendarLocales[language as keyof typeof calendarLocales] ??
    calendarLocales[language.split('-')[0] as keyof typeof calendarLocales] ??
    enUS
  const [draftRange, setDraftRange] = useState<DateRange | undefined>(() =>
    props.customRange
      ? { from: props.customRange.from, to: props.customRange.to }
      : undefined
  )
  const fromTimestamp = props.customRange?.from.getTime()
  const toTimestamp = props.customRange?.to?.getTime()
  useEffect(() => {
    if (fromTimestamp === undefined) {
      setDraftRange(undefined)
      return
    }
    setDraftRange({
      from: new Date(fromTimestamp),
      to: toTimestamp === undefined ? undefined : new Date(toTimestamp),
    })
  }, [fromTimestamp, toTimestamp])
  let rangeLabel = t('Select dates')
  if (draftRange?.from) {
    rangeLabel = formatDate(draftRange.from, language)
    if (draftRange.to) {
      rangeLabel = `${rangeLabel} – ${formatDate(draftRange.to, language)}`
    }
  }

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
        <Popover>
          <PopoverTrigger
            render={
              <Button
                variant='outline'
                className='w-full justify-start text-start sm:w-auto'
                aria-label={t('Choose a custom date range')}
              />
            }
          >
            <CalendarDays data-icon='inline-start' />
            <span className='min-w-0 truncate'>{rangeLabel}</span>
          </PopoverTrigger>
          <PopoverContent className='w-auto p-0' align='start'>
            <Calendar
              mode='range'
              selected={draftRange}
              onSelect={(range) => {
                setDraftRange(range)
                if (range?.from && range.to) {
                  props.onCustomRangeChange(range)
                }
              }}
              numberOfMonths={1}
              max={MAX_RANKING_CUSTOM_DAYS - 1}
              locale={calendarLocale}
              disabled={(date: Date) => date > new Date()}
              footer={
                <p className='text-muted-foreground px-2 pb-2 text-xs'>
                  {t('Up to {{count}} days', {
                    count: MAX_RANKING_CUSTOM_DAYS,
                  })}
                </p>
              }
            />
          </PopoverContent>
        </Popover>
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
