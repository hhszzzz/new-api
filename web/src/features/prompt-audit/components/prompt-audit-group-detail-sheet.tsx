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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { CollapsibleDetailSection } from '@/features/usage-logs/components/dialogs/log-detail-layout'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { createServerError } from '@/lib/server-error-message'

import { getPromptAuditGroupContent } from '../api'
import { promptAuditGroupQueryKey } from '../lib'
import type { PromptAuditGroupTarget } from '../types'
import { PromptAuditContentVersionView } from './prompt-audit-content-version'
import { PromptAuditGroupPagination } from './prompt-audit-group-pagination'
import { PromptAuditOutcomeCounts } from './prompt-audit-outcome-counts'
import { PromptAuditSessionQuestions } from './prompt-audit-session-questions'

type GroupDetailProps = {
  target: PromptAuditGroupTarget
  canViewFullPrompt: boolean
  onOpenChange: (open: boolean) => void
  onOpenRecord: (id: number) => void
}

export function PromptAuditGroupDetailSheet(props: GroupDetailProps) {
  const { t } = useTranslation()
  const targetKey = JSON.stringify(props.target)
  return (
    <Sheet open onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full gap-0 sm:max-w-2xl xl:max-w-3xl'>
        <SheetHeader className='shrink-0 p-4 pr-12 sm:px-6 sm:pr-14'>
          <SheetTitle>{t('Question details')}</SheetTitle>
        </SheetHeader>
        <PromptAuditGroupReader
          key={targetKey}
          {...props}
          initialMaxID={props.target.maxID}
        />
      </SheetContent>
    </Sheet>
  )
}

function PromptAuditGroupReader(
  props: GroupDetailProps & {
    initialMaxID?: number
  }
) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const queryClient = useQueryClient()
  const [maxID, setMaxID] = useState(props.initialMaxID)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [activeTab, setActiveTab] = useState('content')
  const params = {
    ...props.target.filters,
    page: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    max_id: maxID,
  }
  const query = useQuery({
    queryKey: promptAuditGroupQueryKey(props.target, 'content', params),
    // Once established, the bound does not drift on focus or review/retry.
    staleTime: Infinity,
    gcTime: 0,
    queryFn: async () => {
      const result = await getPromptAuditGroupContent(props.target.id, params)
      if (!result.success || !result.data) {
        throw createServerError(result, t('Failed to load question details'))
      }
      if (maxID === undefined) {
        // The first response establishes a database-wide bound. Seed the bounded
        // query before switching to it, avoiding a second metadata request.
        const boundedParams = { ...params, max_id: result.data.max_id }
        queryClient.setQueryData(
          promptAuditGroupQueryKey(props.target, 'content', boundedParams),
          result.data
        )
        setMaxID(result.data.max_id)
      }
      return result.data
    },
  })

  if (query.isPending) {
    return <LoadingState message={t('Loading audit details...')} />
  }
  if (query.isError) {
    return (
      <ErrorState
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  }
  const bound = maxID ?? query.data.max_id
  const versions = query.data.items
  const main = versions.filter((version) => version.kind === 'main')
  const branches = versions.filter((version) => version.kind !== 'main')
  const summary = query.data.summary
  const firstAt = formatTimestampToDate(summary.first_at)
  const lastAt = formatTimestampToDate(summary.last_at)
  const sameDay = firstAt.slice(0, 10) === lastAt.slice(0, 10)
  const panelClassName =
    'bg-muted/20 min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain p-4 sm:p-6 data-hidden:hidden'
  return (
    <div
      className='flex min-h-0 min-w-0 flex-1 flex-col'
      aria-busy={query.isFetching}
    >
      <section
        aria-label={t('Result')}
        className='flex shrink-0 flex-col gap-2 px-4 pb-3 sm:px-6'
      >
        <div className='flex flex-wrap gap-1.5'>
          <PromptAuditOutcomeCounts repeat={summary} />
        </div>
        <div className='text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-xs tabular-nums'>
          <span>
            {t('Request count')} {formatNumber(summary.count, locale)}
          </span>
          <span>
            {firstAt}
            {firstAt !== lastAt && ` → ${sameDay ? lastAt.slice(11) : lastAt}`}
          </span>
          {summary.blocks > 0 && summary.outcome_counts && (
            <span>
              {t('Block actions')} {formatNumber(summary.blocks, locale)}
            </span>
          )}
        </div>
      </section>
      <Tabs
        value={activeTab}
        onValueChange={(value) => setActiveTab(String(value))}
        className='min-h-0 min-w-0 flex-1 gap-0'
      >
        <div className='shrink-0 overflow-x-auto border-b px-4 sm:px-6'>
          <TabsList
            variant='line'
            aria-label={t('Question details')}
            className='min-w-max justify-start gap-5 group-data-horizontal/tabs:h-11'
          >
            <TabsTrigger value='content'>{t('Inspected content')}</TabsTrigger>
            <TabsTrigger value='session'>{t('Session questions')}</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value='content' className={panelClassName}>
          {activeTab === 'content' && (
            <section
              aria-label={t('Content versions')}
              className='flex min-w-0 flex-col gap-5'
            >
              {!props.canViewFullPrompt && (
                <p className='text-muted-foreground text-xs leading-relaxed'>
                  {t('Your permission only allows the redacted preview.')}
                </p>
              )}
              {versions.length === 0 ? (
                <EmptyState
                  className='min-h-24'
                  title={t('No content versions in this scope')}
                />
              ) : (
                <>
                  <div className='flex min-w-0 flex-col gap-4'>
                    {main.map((version) => (
                      <PromptAuditContentVersionView
                        key={version.id}
                        version={version}
                        target={props.target}
                        maxID={bound}
                        canViewFullPrompt={props.canViewFullPrompt}
                        onOpenRecord={props.onOpenRecord}
                      />
                    ))}
                  </div>
                  {main.length === 0 && (
                    <p className='text-muted-foreground text-xs'>
                      {t('No main request content on this page')}
                    </p>
                  )}
                  {branches.length > 0 && (
                    <CollapsibleDetailSection
                      label={t('Subagent and background content')}
                      variant='plain'
                    >
                      <div className='flex min-w-0 flex-col gap-4'>
                        {branches.map((version) => (
                          <PromptAuditContentVersionView
                            key={version.id}
                            version={version}
                            target={props.target}
                            maxID={bound}
                            canViewFullPrompt={props.canViewFullPrompt}
                            onOpenRecord={props.onOpenRecord}
                          />
                        ))}
                      </div>
                    </CollapsibleDetailSection>
                  )}
                </>
              )}
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
          )}
        </TabsContent>
        <TabsContent value='session' className={panelClassName}>
          {activeTab === 'session' && (
            <PromptAuditSessionQuestions target={props.target} maxID={bound} />
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}
