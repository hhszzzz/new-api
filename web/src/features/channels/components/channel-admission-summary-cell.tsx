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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatCompactNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getChannelAdmissionStats } from '../api'

const DAY_IN_SECONDS = 24 * 60 * 60

export function ChannelAdmissionSummaryCell(props: { channelId: number }) {
  const { t, i18n } = useTranslation()
  const statsQuery = useQuery({
    queryKey: ['channel-admission-summary', props.channelId],
    queryFn: async () => {
      const end = Math.floor(Date.now() / 1000)
      return requireServerSuccess(
        await getChannelAdmissionStats(props.channelId, {
          start_timestamp: end - DAY_IN_SECONDS,
          end_timestamp: end,
        })
      ).data
    },
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
  const stats = statsQuery.data
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const formatCount = (value: number | undefined) =>
    value === undefined ? '—' : formatCompactNumber(value, locale)

  return (
    <Button
      type='button'
      variant='ghost'
      size='sm'
      className='h-auto min-h-9 max-w-full justify-start px-1 py-1 text-left whitespace-normal'
      aria-label={t('Admission Monitor')}
      title={t('Admission Monitor')}
      onClick={(event) => event.stopPropagation()}
      render={
        <Link
          to='/channels/admission'
          search={{ channel_id: props.channelId }}
        />
      }
    >
      <span className='grid min-w-0 gap-0.5 text-xs leading-tight'>
        <span>
          {t('Active')}: {formatCount(stats?.active)} · {t('Waiting')}:{' '}
          {formatCount(stats?.waiting)}
        </span>
        <span className='text-muted-foreground'>
          {t('Oldest wait')}: {formatCount(stats?.oldest_wait_ms)} ms
        </span>
      </span>
    </Button>
  )
}
