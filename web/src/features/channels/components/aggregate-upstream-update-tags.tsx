/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'

import { summarizeUpstreamUpdates } from '../lib/upstream-update-utils'
import type { Channel } from '../types'

type Props = {
  channels: ReadonlyArray<Pick<Channel, 'settings'>>
}

export function AggregateUpstreamUpdateTags(props: Props) {
  const { t } = useTranslation()
  const summary = summarizeUpstreamUpdates(props.channels)

  if (summary.addModelCount === 0 && summary.removeModelCount === 0) {
    return null
  }

  const addDescription = `${t('Upstream Updates')}: +${summary.addModelCount} (${t('{{count}} child channels', { count: summary.addChannelCount })})`
  const removeDescription = `${t('Upstream Updates')}: -${summary.removeModelCount} (${t('{{count}} child channels', { count: summary.removeChannelCount })})`

  return (
    <div className='flex items-center gap-0.5'>
      {summary.addModelCount > 0 && (
        <StatusBadge
          label={`+${summary.addModelCount}`}
          variant='success'
          size='sm'
          copyable={false}
          title={addDescription}
          aria-label={addDescription}
        />
      )}
      {summary.removeModelCount > 0 && (
        <StatusBadge
          label={`-${summary.removeModelCount}`}
          variant='danger'
          size='sm'
          copyable={false}
          title={removeDescription}
          aria-label={removeDescription}
        />
      )}
    </div>
  )
}
