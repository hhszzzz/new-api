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

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import { formatAccountPoolOffset } from '../lib/quota'
import type {
  AccountPoolQuotaOverride,
  AccountPoolQuotaWindowOverride,
} from '../types'

function QuotaLimitBadge(props: {
  label: string
  override: AccountPoolQuotaWindowOverride
}) {
  const { t } = useTranslation()
  if (props.override.offset_percent === 0 && !props.override.pinned) {
    return null
  }
  const value = props.override.pinned
    ? t('Pinned at 100%')
    : t('Offset {{percent}}%', {
        percent: formatAccountPoolOffset(props.override.offset_percent),
      })
  return (
    <Badge
      variant='outline'
      className={cn(
        props.override.pinned
          ? 'border-success/30 bg-success/10 text-success'
          : 'border-warning/30 bg-warning/10 text-warning'
      )}
    >
      {props.label}: {value}
    </Badge>
  )
}

export function QuotaOverrideBadge(props: {
  override: AccountPoolQuotaOverride
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap gap-1'>
      <QuotaLimitBadge
        label={t('5-hour quota')}
        override={props.override.primary}
      />
      <QuotaLimitBadge
        label={t('Weekly / monthly quota')}
        override={props.override.secondary}
      />
    </div>
  )
}
