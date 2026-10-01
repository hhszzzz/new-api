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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { createServerError } from '@/lib/server-error-message'

import { getPromptAuditSessionQuestions } from '../api'
import {
  isPromptAuditContinuation,
  promptAuditContentKindLabel,
  promptAuditGroupQueryKey,
} from '../lib'
import type { PromptAuditGroupTarget } from '../types'
import { PromptAuditGroupPagination } from './prompt-audit-group-pagination'
import { PromptAuditOutcomeCounts } from './prompt-audit-outcome-counts'

/**
 * The questions recorded in the same session and time range, as a read-only
 * index. Deliberately not a turn-by-turn transcript and not clickable.
 */
export function PromptAuditSessionQuestions(props: {
  target: PromptAuditGroupTarget
  maxID: number
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  // Deliberately do not spread the list filters: this index reads all questions
  // in the time range, even when the list was showing only unavailable results.
  const filters = {
    start_time: props.target.filters.start_time,
    end_time: props.target.filters.end_time,
  }
  const params = {
    ...filters,
    page: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    max_id: props.maxID,
  }
  const query = useQuery({
    queryKey: promptAuditGroupQueryKey(
      { ...props.target, scope: 'session', filters },
      'session-questions',
      params
    ),
    gcTime: 0,
    queryFn: async () => {
      const result = await getPromptAuditSessionQuestions(
        props.target.id,
        params
      )
      if (!result.success || !result.data) {
        throw createServerError(result, t('Failed to load session questions'))
      }
      return result.data
    },
  })
  if (query.isPending) return <LoadingState className='min-h-24' />
  if (query.isError) {
    return (
      <ErrorState
        className='min-h-24'
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!query.data.has_session) {
    return (
      <EmptyState
        className='min-h-24'
        title={t('No session is recorded; related questions cannot be linked.')}
      />
    )
  }
  if (query.data.items.length === 0) {
    return (
      <EmptyState
        className='min-h-24'
        title={t('No questions in this time range')}
      />
    )
  }
  return (
    <section
      aria-label={t('Session questions')}
      className='flex min-w-0 flex-col gap-4'
      aria-busy={query.isFetching}
    >
      <ul className='bg-background divide-border divide-y overflow-hidden rounded-xl border'>
        {query.data.items.map((question) => (
          <li key={question.id} className='flex min-w-0 flex-col gap-2 p-4'>
            <p className='text-sm leading-relaxed break-words'>
              {question.redacted_preview || t('No prompt text retained')}
            </p>
            <div className='text-muted-foreground flex flex-wrap items-center gap-x-2 gap-y-1 text-xs tabular-nums'>
              {question.request_kind !== 'prompt' &&
                question.request_kind !== 'step' && (
                  <span>
                    {!isPromptAuditContinuation(question.request_kind) && (
                      <>{t('Branch-only question group')} · </>
                    )}
                    {promptAuditContentKindLabel(
                      question.request_kind || '',
                      t
                    )}
                  </span>
                )}
              <span>
                {formatTimestampToDate(
                  question.repeat?.first_at ?? question.created_at
                )}
              </span>
              {question.repeat && (
                <span>
                  {t('Request count')}{' '}
                  {formatNumber(question.repeat.count, locale)}
                </span>
              )}
            </div>
            {question.repeat && (
              <div className='flex flex-wrap gap-1.5'>
                <PromptAuditOutcomeCounts repeat={question.repeat} />
              </div>
            )}
          </li>
        ))}
      </ul>
      {query.data.total > query.data.page_size && (
        <PromptAuditGroupPagination
          total={query.data.total}
          pagination={{
            pageIndex: query.data.page - 1,
            pageSize: query.data.page_size,
          }}
          onChange={setPagination}
        />
      )}
    </section>
  )
}
