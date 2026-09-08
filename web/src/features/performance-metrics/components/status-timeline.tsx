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

import type { ModelStatusTimelinePoint } from '../status-types'
import { getModelStatusBarClass } from './status-presentation'

export function StatusTimeline(props: {
  timeline: ModelStatusTimelinePoint[]
}) {
  const { t } = useTranslation()
  return (
    <span
      role='img'
      aria-label={t('Status over the last 24 hours')}
      className='flex h-3 w-24 items-center justify-between'
    >
      {props.timeline.map((point) => (
        <span
          key={point.ts}
          aria-hidden
          className={cn(
            'h-full w-[3px] shrink-0 rounded-xs',
            getModelStatusBarClass(point.status, point.success_rate)
          )}
        />
      ))}
    </span>
  )
}
