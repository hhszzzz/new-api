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
import { AlertTriangle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'

import {
  getProtocolTranslationTarget,
  hasProtocolFieldAdjustments,
} from '../lib/protocol-conversion'
import type { LogOtherData } from '../types'

/**
 * Protocol translation label shown under a log's model name. The badge stays
 * informational; only a presentation-class conversion warning — a field that
 * was actually dropped or rewritten — adds the amber marker.
 */
export function ProtocolTranslationBadge(props: {
  logType: number
  other: LogOtherData | null
  isAdminView: boolean
}) {
  const { t } = useTranslation()
  const target = props.isAdminView
    ? getProtocolTranslationTarget(props.logType, props.other)
    : null
  if (!target) return null

  const label = t('{{protocol}} translation', { protocol: target })
  const adjusted = hasProtocolFieldAdjustments(props.logType, props.other)
  const adjustmentLabel = t('Fields were adjusted during conversion')

  return (
    <StatusBadge
      variant='info'
      type='text'
      size='sm'
      copyable={false}
      title={label}
      className='h-3 text-[10px] leading-3'
    >
      <span className='min-w-0 truncate leading-normal'>{label}</span>
      {adjusted && (
        <span
          data-protocol-field-adjustment
          className='inline-flex shrink-0 text-amber-600 dark:text-amber-400'
          title={adjustmentLabel}
          aria-label={adjustmentLabel}
        >
          <AlertTriangle className='size-3' aria-hidden='true' />
        </span>
      )}
    </StatusBadge>
  )
}
