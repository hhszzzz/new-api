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
import type { PaginationState } from '@tanstack/react-table'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { toIntlLocale } from '@/i18n/languages'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  getPromptAuditImportJob,
  getPromptAuditStorage,
  listPromptAuditArchives,
  listPromptAuditImportedEvents,
  listPromptAuditImports,
} from './api'
import { ArchiveImportDialog } from './components/archive-import-dialog'
import { PromptAuditDetailSheet } from './components/prompt-audit-detail-sheet'
import { PromptAuditGroupPagination } from './components/prompt-audit-group-pagination'
import { PromptAuditNavigation } from './components/prompt-audit-navigation'
import { promptAuditOutcome } from './lib'
import type {
  PromptAuditArchive,
  PromptAuditEvent,
  PromptAuditImportJob,
} from './types'

export function PromptAuditStorage() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const canView = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
    ADMIN_PERMISSION_ACTIONS.VIEW_FULL_PROMPT
  )
  const canImport =
    canView &&
    hasPermission(
      user,
      ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
      ADMIN_PERMISSION_ACTIONS.MANAGE
    )
  const [importOpen, setImportOpen] = useState(false)
  const [job, setJob] = useState<PromptAuditImportJob | null>(null)
  const [selection, setSelection] = useState('')
  const [anchor, setAnchor] = useState<number | undefined>()
  const [detailID, setDetailID] = useState<number | null>(null)
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 50,
  })
  const stats = useQuery({
    queryKey: ['prompt-audit', 'storage'],
    queryFn: getPromptAuditStorage,
  })
  const catalog = useQuery({
    queryKey: ['prompt-audit', 'archives'],
    queryFn: listPromptAuditArchives,
  })
  const imports = useQuery({
    queryKey: ['prompt-audit', 'imports'],
    queryFn: listPromptAuditImports,
  })
  const jobID = job?.id
  const jobQuery = useQuery({
    queryKey: ['prompt-audit', 'import-job', jobID],
    enabled: Boolean(jobID),
    queryFn: () => getPromptAuditImportJob(jobID ?? ''),
    refetchInterval: (query) =>
      query.state.data?.status === 'done' ||
      query.state.data?.status === 'failed'
        ? false
        : 2000,
  })
  const jobStatus = jobQuery.data?.status ?? job?.status
  let jobMessage = t('Archive import queued')
  if (jobStatus === 'done') {
    jobMessage = t('Archive import complete')
  } else if (jobStatus === 'failed') {
    jobMessage = t('Archive import failed')
  }
  useEffect(() => {
    if (jobStatus === 'done') {
      void client.invalidateQueries({ queryKey: ['prompt-audit', 'imports'] })
      void client.invalidateQueries({ queryKey: ['prompt-audit', 'storage'] })
      void client.invalidateQueries({
        queryKey: ['prompt-audit', 'imported-events'],
      })
    }
  }, [jobStatus, client])
  const sources = [
    ...new Set(imports.data?.map((item) => item.source_id) ?? []),
  ]
  const source = sources.includes(selection) ? selection : (sources[0] ?? '')
  const records = useQuery({
    queryKey: ['prompt-audit', 'imported-events', source, anchor, pagination],
    enabled: source !== '',
    queryFn: () =>
      listPromptAuditImportedEvents(
        source,
        pagination.pageIndex + 1,
        pagination.pageSize,
        anchor
      ),
  })
  const archiveColumns: StaticDataTableColumn<PromptAuditArchive>[] = [
    {
      id: 'day',
      header: t('Day package'),
      cell: (row) => (
        <div className='space-y-1 py-2'>
          <p>{row.archive.day}</p>
          <p className='text-muted-foreground font-mono text-xs'>
            {row.archive.id.slice(0, 12)}
          </p>
        </div>
      ),
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (row) => (
        <Badge
          variant={row.archive.status === 'verified' ? 'secondary' : 'outline'}
        >
          {row.archive.status === 'verified'
            ? t('Verified archive')
            : t('Awaiting verification')}
        </Badge>
      ),
    },
    {
      id: 'size',
      header: t('Size'),
      cell: (row) =>
        `${formatNumber(row.archive.bytes / 1024 ** 2, locale)} MiB`,
    },
    {
      id: 'records',
      header: t('Audit records'),
      cell: (row) => formatNumber(row.archive.record_count, locale),
    },
    {
      id: 'files',
      header: t('Remote volumes'),
      cell: (row) => (
        <div className='space-y-1 py-2'>
          {row.volumes.map((volume) => (
            <div className='flex items-center gap-2' key={volume.name}>
              <span
                className='max-w-xs truncate font-mono text-xs'
                title={volume.remote_path}
              >
                {volume.name}
              </span>
              <CopyButton
                value={volume.remote_path}
                size='sm'
                variant='ghost'
                tooltip={t('Copy')}
              />
            </div>
          ))}
        </div>
      ),
    },
  ]
  const recordColumns: StaticDataTableColumn<PromptAuditEvent>[] = [
    {
      id: 'time',
      header: t('Time'),
      cell: (row) => formatTimestampToDate(row.created_at),
    },
    {
      id: 'direction',
      header: t('Direction'),
      cell: (row) =>
        row.direction === 'output' ? t('Model reply') : t('Request input'),
    },
    { id: 'model', header: t('Model'), cell: (row) => row.model },
    {
      id: 'result',
      header: t('Result'),
      cell: (row) => {
        const outcome = promptAuditOutcome(row)
        return <Badge variant={outcome.variant}>{t(outcome.key)}</Badge>
      },
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: (row) => (
        <div className='flex gap-2'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setDetailID(row.id)}
          >
            {t('View')}
          </Button>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => {
              setAnchor(row.id)
              setPagination((value) => ({ ...value, pageIndex: 0 }))
            }}
          >
            {t('Related records')}
          </Button>
        </div>
      ),
    },
  ]
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Prompt audit')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {canImport && (
          <Button onClick={() => setImportOpen(true)}>
            {t('Import day packages')}
          </Button>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='w-full space-y-6'>
          <PromptAuditNavigation />
          {stats.data && (
            <p className='text-muted-foreground text-sm'>
              {stats.data.archive_required
                ? t(
                    'Complete bodies stay locally for seven days and are removed only after archive verification.'
                  )
                : t('Shared body storage is not enabled yet.')}
            </p>
          )}
          {stats.isPending && <LoadingState />}
          {stats.isError && (
            <ErrorState
              description={stats.error.message}
              onRetry={() => void stats.refetch()}
            />
          )}
          {stats.data && (
            <dl className='grid gap-4 rounded-xl border p-4 sm:grid-cols-2 lg:grid-cols-4'>
              <div>
                <dt className='text-muted-foreground text-sm'>
                  {t('Local body storage')}
                </dt>
                <dd className='text-xl tabular-nums'>
                  {formatNumber(
                    (stats.data.stored_bytes + stats.data.legacy_bytes) /
                      1024 ** 2,
                    locale
                  )}{' '}
                  MiB
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground text-sm'>
                  {t('New shared bodies in 24 hours')}
                </dt>
                <dd className='text-xl tabular-nums'>
                  {formatNumber(stats.data.new_bytes_24h / 1024 ** 2, locale)}{' '}
                  MiB
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground text-sm'>
                  {t('Archive backlog')}
                </dt>
                <dd className='text-xl tabular-nums'>
                  {formatNumber(stats.data.archive_backlog, locale)}
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground text-sm'>
                  {t('Viewing cache')}
                </dt>
                <dd className='text-xl tabular-nums'>
                  {formatNumber(stats.data.imported_bytes / 1024 ** 2, locale)}{' '}
                  /{' '}
                  {formatNumber(
                    stats.data.cache_limit_bytes / 1024 ** 2,
                    locale
                  )}{' '}
                  MiB
                </dd>
              </div>
              {stats.data.unmigrated > 0 && (
                <div className='sm:col-span-2 lg:col-span-4'>
                  <dt>{t('Records awaiting migration')}</dt>
                  <dd>{formatNumber(stats.data.unmigrated, locale)}</dd>
                </div>
              )}
            </dl>
          )}
          <section className='space-y-3'>
            <h2 className='text-base font-medium'>{t('Archive directory')}</h2>
            {catalog.isPending && <LoadingState />}
            {catalog.isError && (
              <ErrorState
                description={catalog.error.message}
                onRetry={() => void catalog.refetch()}
              />
            )}
            {catalog.data && (
              <StaticDataTable
                columns={archiveColumns}
                data={catalog.data}
                getRowKey={(row) => row.archive.id}
              />
            )}
          </section>
          <section className='space-y-3'>
            <h2 className='text-base font-medium'>{t('Imported records')}</h2>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Imports are a temporary viewing cache: 24 hours, up to 2 GiB. Original audit results are preserved.'
              )}
            </p>
            {jobStatus && (
              <p role='status'>
                {jobMessage}
                {jobQuery.data?.message && `: ${jobQuery.data.message}`}
              </p>
            )}
            {jobQuery.isError && (
              <ErrorState
                description={jobQuery.error.message}
                onRetry={() => void jobQuery.refetch()}
              />
            )}
            {imports.isError && (
              <ErrorState
                description={imports.error.message}
                onRetry={() => void imports.refetch()}
              />
            )}
            {imports.data && (
              <div className='text-muted-foreground space-y-1 text-xs'>
                {imports.data.map((item) => (
                  <p key={item.id}>
                    {item.day} · {t('Cache expires at')}{' '}
                    {formatTimestampToDate(item.expires_at)}
                  </p>
                ))}
              </div>
            )}
            {source && (
              <div className='flex flex-wrap items-center gap-2'>
                <NativeSelect
                  aria-label={t('Archive source')}
                  value={source}
                  onChange={(event) => {
                    setSelection(event.target.value)
                    setAnchor(undefined)
                    setPagination((value) => ({ ...value, pageIndex: 0 }))
                  }}
                >
                  {sources.map((value) => (
                    <NativeSelectOption key={value} value={value}>
                      {value.slice(0, 12)}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                {anchor && (
                  <Button
                    variant='ghost'
                    onClick={() => {
                      setAnchor(undefined)
                      setPagination((value) => ({ ...value, pageIndex: 0 }))
                    }}
                  >
                    {t('All imported records')}
                  </Button>
                )}
              </div>
            )}
            {records.isFetching && source && <LoadingState />}
            {records.isError && (
              <ErrorState
                description={records.error.message}
                onRetry={() => void records.refetch()}
              />
            )}
            {records.data && (
              <>
                <StaticDataTable
                  columns={recordColumns}
                  data={records.data.items}
                  getRowKey={(row) => row.id}
                />
                <PromptAuditGroupPagination
                  total={records.data.total}
                  pagination={pagination}
                  onChange={setPagination}
                />
              </>
            )}
          </section>
        </div>
        {importOpen && (
          <ArchiveImportDialog
            open
            onOpenChange={setImportOpen}
            onImported={setJob}
          />
        )}
        <PromptAuditDetailSheet
          eventID={detailID}
          sourceID={source}
          canViewFullPrompt={canView}
          canManage={false}
          canDelete={false}
          onOpenChange={(open) => {
            if (!open) setDetailID(null)
          }}
          onViewRelated={setDetailID}
          onDelete={() => {}}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
