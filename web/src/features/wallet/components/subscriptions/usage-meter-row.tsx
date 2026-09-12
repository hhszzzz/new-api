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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { USAGE_WINDOW_LABELS } from '@/features/subscriptions/constants'
import type { UsageMeter } from '@/features/subscriptions/lib'
import { formatQuota, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

interface UsageMeterRowProps {
  meter: UsageMeter
  /** Muted rendering for subscriptions that are not currently funding. */
  inactive?: boolean
}

function indicatorTone(percent: number): string {
  if (percent >= 100) return 'bg-destructive'
  if (percent >= 80) return 'bg-warning'
  return 'bg-primary'
}

// One line of the subscription ledger: what the window is, how much of it is
// left, and when it opens again.
export function UsageMeterRow(props: UsageMeterRowProps) {
  const { t } = useTranslation()
  const meter = props.meter
  const resetLabel = meter.resetAt
    ? t('Resets {{time}}', { time: formatTimestampToDate(meter.resetAt) })
    : t('Opens on next request')

  return (
    <div
      data-testid={`usage-meter-${meter.key}`}
      className={cn('space-y-1', props.inactive && 'opacity-60')}
    >
      <div className='flex items-baseline justify-between gap-3 text-xs'>
        <span className='text-muted-foreground truncate'>
          {t(USAGE_WINDOW_LABELS[meter.key])}
        </span>
        <Tooltip>
          <TooltipTrigger
            render={<span className='shrink-0 cursor-help tabular-nums' />}
          >
            <span className='text-foreground font-medium'>
              {formatQuota(meter.remaining)}
            </span>
            <span className='text-muted-foreground'>
              {' '}
              / {formatQuota(meter.amount)}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            {t('Used')} {formatQuota(meter.used)} · {t('Raw Quota')}{' '}
            {meter.used}/{meter.amount}
          </TooltipContent>
        </Tooltip>
      </div>
      <Progress
        value={meter.percent}
        aria-label={t(USAGE_WINDOW_LABELS[meter.key])}
        trackClassName='h-1.5'
        indicatorClassName={indicatorTone(meter.percent)}
      />
      {meter.key !== 'monthly' || meter.resetAt > 0 ? (
        <div className='text-muted-foreground/70 text-[11px]'>{resetLabel}</div>
      ) : null}
    </div>
  )
}
