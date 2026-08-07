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
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import dayjs from 'dayjs'
import { Eye, RefreshCw, Search, Trash2, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import {
  getPromptAuditCategories,
  getPromptAuditStats,
  listPromptAudits,
} from './api'
import { PromptAuditDeleteDialog } from './components/prompt-audit-delete-dialog'
import { PromptAuditDetailSheet } from './components/prompt-audit-detail-sheet'
import {
  EMPTY_PROMPT_AUDIT_FILTERS,
  promptAuditDeleteFilter,
  promptAuditFilterParams,
  validatePromptAuditFilters,
} from './lib'
import type {
  PromptAuditDeleteFilter,
  PromptAuditEvent,
  PromptAuditFilters,
} from './types'

const PAGE_SIZE = 20

function decisionBadgeVariant(decision: string) {
  if (decision === 'block' || decision === 'unavailable') return 'destructive'
  if (decision === 'flag') return 'warning'
  if (decision === 'pass') return 'secondary'
  return 'outline'
}

function statusBadgeVariant(status: string) {
  if (status === 'failed') return 'destructive'
  if (status === 'retry' || status === 'processing') return 'warning'
  if (status === 'done') return 'secondary'
  return 'outline'
}

export function PromptAuditRecords() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const canViewFullPrompt = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
    ADMIN_PERMISSION_ACTIONS.VIEW_FULL_PROMPT
  )
  const canManage = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
    ADMIN_PERMISSION_ACTIONS.MANAGE
  )
  const canDelete = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
    ADMIN_PERMISSION_ACTIONS.DELETE
  )

  const [draftFilters, setDraftFilters] = useState<PromptAuditFilters>({
    ...EMPTY_PROMPT_AUDIT_FILTERS,
  })
  const [filters, setFilters] = useState<PromptAuditFilters>({
    ...EMPTY_PROMPT_AUDIT_FILTERS,
  })
  const [page, setPage] = useState(1)
  const [selectedIDs, setSelectedIDs] = useState<Set<number>>(new Set())
  const [detailID, setDetailID] = useState<number | null>(null)
  const [deleteFilter, setDeleteFilter] =
    useState<PromptAuditDeleteFilter | null>(null)

  const filterParams = useMemo(
    () => promptAuditFilterParams(filters),
    [filters]
  )
  const listQuery = useQuery({
    queryKey: ['prompt-audit', 'events', page, filterParams],
    queryFn: async () => {
      const result = await listPromptAudits({
        ...filterParams,
        page,
        page_size: PAGE_SIZE,
      })
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load prompt audits'))
      }
      return result.data
    },
    placeholderData: keepPreviousData,
  })
  const statsQuery = useQuery({
    queryKey: ['prompt-audit', 'stats', filterParams],
    queryFn: async () => {
      const result = await getPromptAuditStats(filterParams)
      if (!result.success || !result.data) {
        throw new Error(
          result.message || t('Failed to load prompt audit statistics')
        )
      }
      return result.data
    },
  })
  const categoriesQuery = useQuery({
    queryKey: ['prompt-audit', 'categories'],
    queryFn: async () => {
      const result = await getPromptAuditCategories()
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load risk categories'))
      }
      return result.data
    },
    staleTime: 5 * 60 * 1000,
  })

  const events = listQuery.data?.items ?? []
  const total = listQuery.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const pageIDs = events.map((event) => event.id)
  const allPageSelected =
    pageIDs.length > 0 && pageIDs.every((id) => selectedIDs.has(id))
  const somePageSelected =
    !allPageSelected && pageIDs.some((id) => selectedIDs.has(id))

  const setFilter = (key: keyof PromptAuditFilters, value: string) => {
    setDraftFilters((current) => ({ ...current, [key]: value }))
  }

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
  }

  const openDelete = (
    scope: 'selected' | 'filtered',
    event?: PromptAuditEvent
  ) => {
    if (event) {
      setDeleteFilter({ ids: [event.id] })
      return
    }
    const ids = scope === 'selected' ? [...selectedIDs] : []
    setDeleteFilter(promptAuditDeleteFilter(filters, ids))
  }

  const stats = statsQuery.data

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Prompt audit records')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            onClick={refresh}
            disabled={listQuery.isFetching}
          >
            <RefreshCw className={listQuery.isFetching ? 'animate-spin' : ''} />
            {t('Refresh')}
          </Button>
          {canDelete && selectedIDs.size > 0 && (
            <Button
              variant='destructive'
              onClick={() => openDelete('selected')}
            >
              <Trash2 />
              {t('Delete selected')} ({selectedIDs.size})
            </Button>
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='space-y-4'>
            <div className='grid grid-cols-2 gap-3 lg:grid-cols-4'>
              {[
                [t('Total'), stats?.total ?? 0],
                [t('Blocked'), stats?.decisions.block ?? 0],
                [t('Flagged'), stats?.decisions.flag ?? 0],
                [t('Failed'), stats?.statuses.failed ?? 0],
              ].map(([label, value]) => (
                <Card key={label} size='sm'>
                  <CardContent>
                    <p className='text-muted-foreground text-xs'>{label}</p>
                    <p className='mt-1 text-2xl font-semibold tabular-nums'>
                      {value}
                    </p>
                  </CardContent>
                </Card>
              ))}
            </div>

            <Card>
              <CardContent className='space-y-3'>
                <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
                  <div className='space-y-1.5'>
                    <Label htmlFor='prompt-audit-status'>{t('Status')}</Label>
                    <NativeSelect
                      id='prompt-audit-status'
                      className='w-full'
                      value={draftFilters.status}
                      onChange={(event) =>
                        setFilter('status', event.target.value)
                      }
                    >
                      <NativeSelectOption value=''>
                        {t('All statuses')}
                      </NativeSelectOption>
                      {['queued', 'processing', 'retry', 'done', 'failed'].map(
                        (status) => (
                          <NativeSelectOption key={status} value={status}>
                            {t(status)}
                          </NativeSelectOption>
                        )
                      )}
                    </NativeSelect>
                  </div>
                  <div className='space-y-1.5'>
                    <Label htmlFor='prompt-audit-decision'>
                      {t('Decision')}
                    </Label>
                    <NativeSelect
                      id='prompt-audit-decision'
                      className='w-full'
                      value={draftFilters.decision}
                      onChange={(event) =>
                        setFilter('decision', event.target.value)
                      }
                    >
                      <NativeSelectOption value=''>
                        {t('All decisions')}
                      </NativeSelectOption>
                      {['pass', 'flag', 'block', 'unavailable'].map(
                        (decision) => (
                          <NativeSelectOption key={decision} value={decision}>
                            {t(decision)}
                          </NativeSelectOption>
                        )
                      )}
                    </NativeSelect>
                  </div>
                  <div className='space-y-1.5'>
                    <Label htmlFor='prompt-audit-category'>
                      {t('Category')}
                    </Label>
                    <NativeSelect
                      id='prompt-audit-category'
                      className='w-full'
                      value={draftFilters.category}
                      onChange={(event) =>
                        setFilter('category', event.target.value)
                      }
                    >
                      <NativeSelectOption value=''>
                        {t('All categories')}
                      </NativeSelectOption>
                      {(categoriesQuery.data ?? []).map((category) => (
                        <NativeSelectOption
                          key={category.id}
                          value={category.id}
                        >
                          {t(category.label)}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                  <div className='space-y-1.5'>
                    <Label htmlFor='prompt-audit-user'>{t('User ID')}</Label>
                    <Input
                      id='prompt-audit-user'
                      inputMode='numeric'
                      value={draftFilters.user_id}
                      onChange={(event) =>
                        setFilter('user_id', event.target.value)
                      }
                    />
                  </div>
                  {[
                    ['group', t('Group')],
                    ['protocol', t('Protocol')],
                    ['model', t('Model')],
                    ['endpoint_id', t('Audit node')],
                    ['prompt_hash', t('Prompt hash')],
                    ['request_id', t('Request ID')],
                  ].map(([key, label]) => (
                    <div key={key} className='space-y-1.5'>
                      <Label htmlFor={`prompt-audit-${key}`}>{label}</Label>
                      <Input
                        id={`prompt-audit-${key}`}
                        value={draftFilters[key as keyof PromptAuditFilters]}
                        onChange={(event) =>
                          setFilter(
                            key as keyof PromptAuditFilters,
                            event.target.value
                          )
                        }
                      />
                    </div>
                  ))}
                  <div className='space-y-1.5'>
                    <Label htmlFor='prompt-audit-start'>
                      {t('Start time')}
                    </Label>
                    <Input
                      id='prompt-audit-start'
                      type='datetime-local'
                      value={draftFilters.start_time}
                      onChange={(event) =>
                        setFilter('start_time', event.target.value)
                      }
                    />
                  </div>
                  <div className='space-y-1.5'>
                    <Label htmlFor='prompt-audit-end'>{t('End time')}</Label>
                    <Input
                      id='prompt-audit-end'
                      type='datetime-local'
                      value={draftFilters.end_time}
                      onChange={(event) =>
                        setFilter('end_time', event.target.value)
                      }
                    />
                  </div>
                </div>
                <div className='flex flex-wrap justify-end gap-2'>
                  <Button
                    variant='outline'
                    onClick={() => {
                      setDraftFilters({ ...EMPTY_PROMPT_AUDIT_FILTERS })
                      setFilters({ ...EMPTY_PROMPT_AUDIT_FILTERS })
                      setPage(1)
                      setSelectedIDs(new Set())
                    }}
                  >
                    <X />
                    {t('Clear filters')}
                  </Button>
                  <Button
                    onClick={() => {
                      const validationError =
                        validatePromptAuditFilters(draftFilters)
                      if (validationError) {
                        toast.error(t(validationError))
                        return
                      }
                      setFilters({ ...draftFilters })
                      setPage(1)
                      setSelectedIDs(new Set())
                    }}
                  >
                    <Search />
                    {t('Apply filters')}
                  </Button>
                  {canDelete && (
                    <Button
                      variant='destructive'
                      onClick={() => openDelete('filtered')}
                    >
                      <Trash2 />
                      {t('Delete filtered')}
                    </Button>
                  )}
                </div>
              </CardContent>
            </Card>

            <Card className='gap-0 py-0'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-10'>
                      <Checkbox
                        aria-label={t('Select current page')}
                        checked={allPageSelected}
                        indeterminate={somePageSelected}
                        onCheckedChange={(checked) => {
                          setSelectedIDs((current) => {
                            const next = new Set(current)
                            for (const id of pageIDs) {
                              if (checked) next.add(id)
                              else next.delete(id)
                            }
                            return next
                          })
                        }}
                      />
                    </TableHead>
                    <TableHead>{t('Time')}</TableHead>
                    <TableHead>{t('Result')}</TableHead>
                    <TableHead>{t('Identity')}</TableHead>
                    <TableHead>{t('Request')}</TableHead>
                    <TableHead className='min-w-64'>
                      {t('Redacted preview')}
                    </TableHead>
                    <TableHead className='text-right'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {listQuery.isLoading && (
                    <TableRow>
                      <TableCell colSpan={7} className='py-10 text-center'>
                        {t('Loading prompt audits...')}
                      </TableCell>
                    </TableRow>
                  )}
                  {listQuery.isError && (
                    <TableRow>
                      <TableCell
                        colSpan={7}
                        className='text-destructive py-10 text-center'
                      >
                        {listQuery.error.message}
                      </TableCell>
                    </TableRow>
                  )}
                  {!listQuery.isLoading &&
                    !listQuery.isError &&
                    events.length === 0 && (
                      <TableRow>
                        <TableCell
                          colSpan={7}
                          className='text-muted-foreground py-10 text-center'
                        >
                          {t('No prompt audit records found')}
                        </TableCell>
                      </TableRow>
                    )}
                  {events.map((event) => (
                    <TableRow key={event.id}>
                      <TableCell>
                        <Checkbox
                          aria-label={t('Select audit record')}
                          checked={selectedIDs.has(event.id)}
                          onCheckedChange={(checked) => {
                            setSelectedIDs((current) => {
                              const next = new Set(current)
                              if (checked) next.add(event.id)
                              else next.delete(event.id)
                              return next
                            })
                          }}
                        />
                      </TableCell>
                      <TableCell>
                        <div>
                          {dayjs
                            .unix(event.created_at)
                            .format('MM-DD HH:mm:ss')}
                        </div>
                        <div className='text-muted-foreground text-xs'>
                          #{event.id}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className='flex flex-wrap gap-1'>
                          <Badge variant={decisionBadgeVariant(event.decision)}>
                            {t(event.decision || 'pending')}
                          </Badge>
                          <Badge variant={statusBadgeVariant(event.status)}>
                            {t(event.status)}
                          </Badge>
                        </div>
                        {event.categories.length > 0 && (
                          <p className='text-muted-foreground mt-1 max-w-48 truncate text-xs'>
                            {event.categories
                              .map((category) => t(category))
                              .join(', ')}
                          </p>
                        )}
                      </TableCell>
                      <TableCell>
                        <div>
                          {t('User')} #{event.user_id}
                        </div>
                        <div className='text-muted-foreground text-xs'>
                          {event.group || '—'} ·{' '}
                          {event.token_name || `#${event.token_id}`}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className='max-w-48 truncate'>
                          {event.model || '—'}
                        </div>
                        <div className='text-muted-foreground max-w-48 truncate text-xs'>
                          {event.protocol} · {event.endpoint_id || '—'}
                        </div>
                        <div className='text-muted-foreground max-w-48 truncate font-mono text-xs'>
                          {event.request_id || event.prompt_hash}
                        </div>
                      </TableCell>
                      <TableCell className='max-w-96 whitespace-normal'>
                        <p className='line-clamp-3 break-words'>
                          {event.redacted_preview || '—'}
                        </p>
                      </TableCell>
                      <TableCell className='text-right'>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('View details')}
                          onClick={() => setDetailID(event.id)}
                        >
                          <Eye />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <div className='flex flex-wrap items-center justify-between gap-3 border-t p-3'>
                <p className='text-muted-foreground text-sm'>
                  {t('{{total}} records · Page {{page}} of {{pages}}', {
                    total,
                    page,
                    pages: pageCount,
                  })}
                </p>
                <div className='flex gap-2'>
                  <Button
                    variant='outline'
                    disabled={page <= 1}
                    onClick={() =>
                      setPage((current) => Math.max(1, current - 1))
                    }
                  >
                    {t('Previous')}
                  </Button>
                  <Button
                    variant='outline'
                    disabled={page >= pageCount}
                    onClick={() =>
                      setPage((current) => Math.min(pageCount, current + 1))
                    }
                  >
                    {t('Next')}
                  </Button>
                </div>
              </div>
            </Card>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PromptAuditDetailSheet
        eventID={detailID}
        canViewFullPrompt={canViewFullPrompt}
        canManage={canManage}
        canDelete={canDelete}
        onOpenChange={(open) => !open && setDetailID(null)}
        onDelete={(event) => {
          setDetailID(null)
          openDelete('selected', event)
        }}
      />
      <PromptAuditDeleteDialog
        open={deleteFilter !== null}
        filter={deleteFilter}
        onOpenChange={(open) => !open && setDeleteFilter(null)}
        onDeleted={() => {
          setSelectedIDs(new Set())
          void refresh()
        }}
      />
    </>
  )
}
