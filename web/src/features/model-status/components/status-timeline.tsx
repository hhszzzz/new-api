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

import { cn } from '@/lib/utils'

import type { ModelStatusTimelinePoint } from '../types'
import { getModelStatusLabel, STATUS_PRESENTATION } from './status-presentation'

type StatusTimelineProps = {
  timeline: ModelStatusTimelinePoint[]
  hourFormatter: Intl.DateTimeFormat
}

function formatHour(timestamp: number, formatter: Intl.DateTimeFormat): string {
  return formatter.format(new Date(timestamp * 1000))
}

export function StatusTimeline(props: StatusTimelineProps) {
  const { t } = useTranslation()
  const firstPoint = props.timeline[0]
  const lastPoint = props.timeline.at(-1)

  return (
    <div>
      <ol
        className='grid grid-cols-[repeat(24,minmax(0,1fr))] gap-0.5 sm:gap-1'
        aria-label={t('Status over the last 24 hours')}
      >
        {props.timeline.map((point) => {
          const presentation = STATUS_PRESENTATION[point.status]
          const statusLabel = getModelStatusLabel(t, point.status)
          const timeLabel = formatHour(point.ts, props.hourFormatter)
          const accessibleLabel = `${timeLabel}: ${statusLabel}`

          return (
            <li
              key={point.ts}
              className={cn(
                'h-7 min-w-0 overflow-hidden rounded-[3px] sm:h-8 sm:rounded',
                presentation.barClassName
              )}
              aria-label={accessibleLabel}
            />
          )
        })}
      </ol>
      {firstPoint && lastPoint ? (
        <div className='text-muted-foreground mt-1.5 flex justify-between font-mono text-[10px] tabular-nums'>
          <time dateTime={new Date(firstPoint.ts * 1000).toISOString()}>
            {formatHour(firstPoint.ts, props.hourFormatter)}
          </time>
          <time dateTime={new Date(lastPoint.ts * 1000).toISOString()}>
            {formatHour(lastPoint.ts, props.hourFormatter)}
          </time>
        </div>
      ) : null}
    </div>
  )
}
