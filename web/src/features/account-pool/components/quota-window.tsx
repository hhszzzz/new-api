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

import { Progress } from '@/components/ui/progress'
import { cn } from '@/lib/utils'

import { formatAccountPoolCountdown } from '../lib/quota'
import type { AccountPoolWindow } from '../types'

type QuotaWindowProps = {
  window: AccountPoolWindow | null
  now: number
  label?: string
  compact?: boolean
}

function quotaTone(remainingPercent: number | null) {
  if (remainingPercent === null) {
    return {
      indicator: 'bg-muted-foreground/50',
      track: 'bg-muted',
      text: 'text-muted-foreground',
    }
  }
  if (remainingPercent < 20) {
    return {
      indicator: 'bg-destructive',
      track: 'bg-destructive/15',
      text: 'text-destructive',
    }
  }
  if (remainingPercent < 50) {
    return {
      indicator: 'bg-warning',
      track: 'bg-warning/15',
      text: 'text-warning',
    }
  }
  return {
    indicator: 'bg-success',
    track: 'bg-success/15',
    text: 'text-success',
  }
}

export function QuotaWindow(props: QuotaWindowProps) {
  const { t } = useTranslation()
  const remaining = props.window?.remaining_percent ?? null
  const tone = quotaTone(remaining)
  const countdown = formatAccountPoolCountdown(
    props.window?.reset_at ?? null,
    props.now
  )
  const value = remaining ?? 0
  let resetText = t('Reset time unknown')
  if (countdown === '0s') {
    resetText = t('Refreshing soon')
  } else if (countdown !== null) {
    resetText = t('Resets in {{time}}', { time: countdown })
  }

  if (!props.window) {
    return (
      <div className='text-muted-foreground text-xs'>
        {props.label ? <div className='font-medium'>{props.label}</div> : null}
        <div>{t('Quota data unavailable')}</div>
      </div>
    )
  }

  return (
    <div className={cn('min-w-36 space-y-1.5', props.compact && 'min-w-0')}>
      <div className='flex items-center justify-between gap-3 text-xs'>
        <span className='text-muted-foreground font-medium'>{props.label}</span>
        <span className={cn('font-semibold tabular-nums', tone.text)}>
          {remaining === null
            ? t('Unknown')
            : t('{{percent}}% remaining', {
                percent: Math.round(remaining * 10) / 10,
              })}
        </span>
      </div>
      <Progress
        value={value}
        aria-label={
          remaining === null
            ? t('Quota data unavailable')
            : t('{{percent}}% remaining', {
                percent: Math.round(remaining * 10) / 10,
              })
        }
        className='gap-0'
        trackClassName={cn('h-1.5 transition-colors', tone.track)}
        indicatorClassName={cn('transition-colors', tone.indicator)}
      />
      <div className='text-muted-foreground flex min-h-4 items-center justify-between gap-2 text-[11px] tabular-nums'>
        <span>
          {props.window.used_percent === null
            ? t('Usage unknown')
            : t('{{percent}}% used', {
                percent: Math.round(props.window.used_percent * 10) / 10,
              })}
        </span>
        <span>{resetText}</span>
      </div>
    </div>
  )
}
