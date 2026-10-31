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

import { formatAccountPoolOffset } from '../lib/quota'
import type { AccountPoolQuotaOverride } from '../types'

export function QuotaOverrideBadge(props: {
  override: AccountPoolQuotaOverride
}) {
  const { t } = useTranslation()
  if (props.override.pinned) {
    return (
      <Badge
        variant='outline'
        className='border-success/30 bg-success/10 text-success'
      >
        {t('Pinned at 100%')}
      </Badge>
    )
  }
  if (props.override.offset_percent === 0) return null
  return (
    <Badge
      variant='outline'
      className='border-warning/30 bg-warning/10 text-warning'
    >
      {t('Offset {{percent}}%', {
        percent: formatAccountPoolOffset(props.override.offset_percent),
      })}
    </Badge>
  )
}
