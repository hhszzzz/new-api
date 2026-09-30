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
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { createServerError } from '@/lib/server-error-message'

import { getPromptAudit } from '../api'
import { promptAuditContentKindLabel, promptAuditGroupQueryKey } from '../lib'
import type {
  PromptAuditContentVersion,
  PromptAuditGroupTarget,
} from '../types'
import { PromptAuditPayloadView } from './prompt-audit-payload-view'

/**
 * One grouped snapshot, framed as a card. With permission the inspected
 * payload is shown directly; without it only the redacted preview is shown.
 */
export function PromptAuditContentVersionView(props: {
  version: PromptAuditContentVersion
  target: PromptAuditGroupTarget
  maxID: number
  canViewFullPrompt: boolean
  onOpenRecord: (id: number) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const query = useQuery({
    queryKey: promptAuditGroupQueryKey(props.target, 'payload', {
      content_id: props.version.id,
      max_id: props.maxID,
    }),
    enabled: props.canViewFullPrompt,
    gcTime: 0,
    queryFn: async () => {
      const result = await getPromptAudit(props.version.id)
      if (!result.success || !result.data) {
        throw createServerError(result, t('Failed to load audit details'))
      }
      return result.data
    },
  })
  return (
    <section
      aria-label={t('Source record #{{id}}', { id: props.version.id })}
      className='bg-background flex min-w-0 flex-col overflow-hidden rounded-xl border'
    >
      <div className='flex min-w-0 items-start justify-between gap-3 px-4 pt-3 pb-1'>
        <div className='flex min-w-0 flex-col gap-1 py-1'>
          <h3 className='text-sm font-medium'>
            {promptAuditContentKindLabel(props.version.kind, t)}
          </h3>
          <div className='text-muted-foreground flex flex-wrap items-center gap-x-2 gap-y-1 text-xs'>
            <span>
              {props.version.direction === 'output'
                ? t('Generated output')
                : t('Request input')}
            </span>
            <span className='tabular-nums'>
              {t('Appears in {{count}} requests', {
                count: formatNumber(props.version.count, locale),
              })}
            </span>
          </div>
        </div>
        <Button
          variant='ghost'
          size='sm'
          className='shrink-0 font-mono text-xs'
          aria-label={t('Source record #{{id}}', { id: props.version.id })}
          onClick={() => props.onOpenRecord(props.version.id)}
        >
          #{props.version.id}
        </Button>
      </div>
      {props.canViewFullPrompt ? (
        <div className='min-w-0' aria-busy={query.isFetching}>
          {query.isPending && (
            <LoadingState
              size='sm'
              className='min-h-20'
              message={t('Loading audit details...')}
            />
          )}
          {query.isError && (
            <ErrorState
              className='min-h-24'
              description={query.error.message}
              onRetry={() => void query.refetch()}
            />
          )}
          {!query.isError &&
            query.data &&
            (!query.data.scan_payload ? (
              <p className='text-muted-foreground p-3 text-xs'>
                {t('No inspected content retained')}
              </p>
            ) : (
              <PromptAuditPayloadView
                payload={query.data.scan_payload}
                truncated={query.data.scan_payload_truncated}
                background={props.version.kind !== 'main'}
              />
            ))}
        </div>
      ) : (
        <p className='p-3 font-mono text-sm leading-relaxed [overflow-wrap:anywhere] break-words whitespace-pre-wrap'>
          {props.version.redacted_preview || t('No prompt text retained')}
        </p>
      )}
    </section>
  )
}
