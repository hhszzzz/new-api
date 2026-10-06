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
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { PromptAuditOutcomeVariant } from '../lib'
import type { PromptAuditRepeat } from '../types'

/** Server-wide counts, never a verdict inferred from a representative or a page. */
export function PromptAuditOutcomeCounts(props: { repeat: PromptAuditRepeat }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const counts = props.repeat.outcome_counts
  if (!counts) {
    return (
      <>
        <Badge variant='outline'>{t('Outcome counts unavailable')}</Badge>
        {props.repeat.blocks > 0 && (
          <Badge variant='destructive'>
            {t('Block actions')} {formatNumber(props.repeat.blocks, locale)}
          </Badge>
        )}
        {props.repeat.unavailable > 0 && (
          <Badge variant='warning'>
            {t('Unavailable actions')}{' '}
            {formatNumber(props.repeat.unavailable, locale)}
          </Badge>
        )}
      </>
    )
  }
  // Each label names what the count is, so a group reading never restates the
  // bare verdict word that a filter or a single record already uses.
  const outcomes: Array<[string, string, PromptAuditOutcomeVariant]> = [
    ['pass', t('Passed'), 'secondary'],
    ['block', t('Blocked'), 'destructive'],
    ['flag', t('Flagged'), 'warning'],
    ['unavailable', t('unavailable'), 'warning'],
    ['stored', t('Stored, not inspected'), 'outline'],
    ['queued', t('queued'), 'outline'],
    ['processing', t('processing'), 'warning'],
    ['retry', t('retry'), 'warning'],
    ['failed', t('failed'), 'warning'],
    ['unknown', t('pending'), 'outline'],
  ]
  return (
    <>
      {outcomes
        .filter(([key]) => counts[key] > 0)
        .map(([key, label, variant]) => (
          <Badge key={key} variant={variant} className='tabular-nums'>
            {label} {formatNumber(counts[key], locale)}
          </Badge>
        ))}
    </>
  )
}
