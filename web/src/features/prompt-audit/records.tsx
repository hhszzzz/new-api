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
  useQueries,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from '@tanstack/react-query'
import type {
  ExpandedState,
  OnChangeFn,
  PaginationState,
  RowSelectionState,
} from '@tanstack/react-table'
import { Trash2 } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { useMediaQuery } from '@/hooks'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { createServerError } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  getPromptAuditCategories,
  getPromptAuditStats,
  listPromptAudits,
} from './api'
import { usePromptAuditColumns } from './components/prompt-audit-columns'
import { PromptAuditDeleteDialog } from './components/prompt-audit-delete-dialog'
import { PromptAuditDetailSheet } from './components/prompt-audit-detail-sheet'
import { PromptAuditFilterBar } from './components/prompt-audit-filter-bar'
import { PromptAuditNavigation } from './components/prompt-audit-navigation'
import {
  getDefaultPromptAuditFilters,
  isMergedPromptAuditRow,
  promptAuditDeleteFilter,
  promptAuditFilterParams,
  promptAuditRowID,
  readPromptAuditCollapseRepeats,
  validatePromptAuditFilters,
  writePromptAuditCollapseRepeats,
} from './lib'
import type {
  PromptAuditDeleteFilter,
  PromptAuditEvent,
  PromptAuditFilters,
  PromptAuditListData,
} from './types'

/**
 * The rows the records table renders. A collapsed row holds the requests it
 * merged once the operator expands it; the API never nests them.
 */
type PromptAuditListRow = PromptAuditEvent & { children?: PromptAuditEvent[] }

export function PromptAuditRecords() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isMobile = useMediaQuery('(max-width: 640px)')
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

  const [draftFilters, setDraftFilters] = useState<PromptAuditFilters>(
    getDefaultPromptAuditFilters
  )
  const [filters, setFilters] = useState<PromptAuditFilters>(
    getDefaultPromptAuditFilters
  )
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
  const [collapseRepeats, setCollapseRepeats] = useState(
    readPromptAuditCollapseRepeats
  )
  const [expanded, setExpanded] = useState<ExpandedState>({})
  const [detailID, setDetailID] = useState<number | null>(null)
  const [deleteFilter, setDeleteFilter] =
    useState<PromptAuditDeleteFilter | null>(null)

  const filterParams = useMemo(
    () => promptAuditFilterParams(filters, { collapseRepeats }),
    [filters, collapseRepeats]
  )
  // Collapsing changes how the listing reads, not which requests match, so the
  // statistics are asked for with the filters alone and survive the switch.
  const statsParams = useMemo(() => promptAuditFilterParams(filters), [filters])
  const listQuery = useQuery({
    queryKey: [
      'prompt-audit',
      'events',
      pagination.pageIndex,
      pagination.pageSize,
      filterParams,
    ],
    queryFn: async () => {
      const result = await listPromptAudits({
        ...filterParams,
        page: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      })
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load prompt audits'))
      }
      return result.data
    },
    placeholderData: keepPreviousData,
  })
  const statsQuery = useQuery({
    queryKey: ['prompt-audit', 'stats', statsParams],
    queryFn: async () => {
      const result = await getPromptAuditStats(statsParams)
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

  // A collapsed row mentions how many requests it merged but carries none of
  // them, so a group is fetched once the operator expands its row. Only the
  // merged rows this page lists are asked for: a group that was deleted,
  // filtered out or paged away stops being fetched along with its row.
  const expandedGroupIDs = useMemo(() => {
    // An ExpandedState is either a per-row record or the "everything" flag, and
    // this table only ever produces the record.
    if (!collapseRepeats || expanded === true) return []
    return (listQuery.data?.items ?? [])
      .filter(
        (item) => isMergedPromptAuditRow(item) && expanded[String(item.id)]
      )
      .map((item) => item.id)
  }, [collapseRepeats, expanded, listQuery.data])
  // Query-core structurally shares what this returns between renders, which
  // only works on plain objects and arrays: kept to those, the rows and the
  // loading list change only when a group actually does.
  const combineGroups = useCallback(
    (results: UseQueryResult<PromptAuditListData>[]) => {
      const rows: Record<number, PromptAuditEvent[]> = {}
      const totals: Record<number, number> = {}
      const loadingIDs: number[] = []
      expandedGroupIDs.forEach((groupID, index) => {
        const result = results[index]
        if (result?.data) {
          rows[groupID] = result.data.items
          totals[groupID] = result.data.total
        }
        if (result?.isFetching) loadingIDs.push(groupID)
      })
      return { rows, totals, loadingIDs }
    },
    [expandedGroupIDs]
  )
  const groups = useQueries({
    queries: expandedGroupIDs.map((groupID) => {
      const params = promptAuditFilterParams(filters, {
        collapseRepeats: true,
        groupID,
      })
      return {
        // Under the listing's prefix, so a refresh reloads the group as well.
        queryKey: ['prompt-audit', 'events', 'group', params],
        queryFn: async () => {
          const result = await listPromptAudits(params)
          if (!result.success || !result.data) {
            throw createServerError(result, t('Failed to load prompt audits'))
          }
          return result.data
        },
      }
    }),
    combine: combineGroups,
  })

  const events = useMemo<PromptAuditListRow[]>(() => {
    const items = listQuery.data?.items ?? []
    if (!collapseRepeats) return items
    return items.map((item) => {
      const children = groups.rows[item.id]
      return children ? { ...item, children } : item
    })
  }, [listQuery.data, collapseRepeats, groups.rows])
  const total = listQuery.data?.total ?? 0
  const columns = usePromptAuditColumns({
    canDelete,
    onOpen: setDetailID,
    collapsed: collapseRepeats,
    loadingGroupIDs: groups.loadingIDs,
    groupTotals: groups.totals,
  })
  // Expanded rows belong to the page that listed them, so paging closes them
  // rather than reopening them whenever those rows are listed again.
  const changePagination = useCallback<OnChangeFn<PaginationState>>(
    (updater) => {
      setPagination(updater)
      setExpanded({})
    },
    []
  )
  const ensurePageInRange = useCallback((pageCount: number) => {
    setPagination((current) => ({
      ...current,
      pageIndex: Math.min(current.pageIndex, Math.max(0, pageCount - 1)),
    }))
  }, [])
  const { table } = useDataTable({
    data: events,
    columns,
    tableStateStorageKey: 'prompt-audit-records',
    pagination,
    rowSelection,
    expanded,
    onPaginationChange: changePagination,
    onRowSelectionChange: setRowSelection,
    onExpandedChange: setExpanded,
    getRowId: promptAuditRowID,
    getSubRows: (row: PromptAuditListRow) => row.children,
    // A merged row receives the requests it counted only once it is expanded,
    // so whether it can be opened cannot be read off the data the table was
    // handed and the row has to say so itself.
    getRowCanExpand: (row) => isMergedPromptAuditRow(row.original),
    withExpandedRowModel: true,
    // Only the collapsed rows stand for a group; their children are single
    // requests the operator can open or delete one at a time.
    enableRowSelection: (row) => canDelete && row.depth === 0,
    enableSorting: false,
    manualFiltering: true,
    manualPagination: true,
    totalCount: total,
    ensurePageInRange,
  })

  const selectedIDs = useMemo(
    () =>
      Object.entries(rowSelection)
        .filter(([, selected]) => selected)
        .map(([id]) => Number(id))
        .filter(Number.isFinite),
    [rowSelection]
  )
  const setFilter = useCallback(
    (key: keyof PromptAuditFilters, value: string) => {
      setDraftFilters((current) => ({ ...current, [key]: value }))
    },
    []
  )
  const applyFilters = useCallback(() => {
    const validationError = validatePromptAuditFilters(draftFilters)
    if (validationError) {
      toast.error(t(validationError))
      return
    }
    setFilters({ ...draftFilters })
    setPagination((current) => ({ ...current, pageIndex: 0 }))
    setRowSelection({})
    setExpanded({})
    // Searching is also how an operator asks for the newest rows, and the same
    // filters keep the same query key: without this the listing would answer
    // from what it already held. A changed filter is a query of its own and
    // fetches by itself, so the refresh covers both cases the way the log
    // screens refresh on search.
    void queryClient.invalidateQueries({ queryKey: ['prompt-audit', 'events'] })
    void queryClient.invalidateQueries({ queryKey: ['prompt-audit', 'stats'] })
  }, [draftFilters, queryClient, t])
  const resetFilters = useCallback(() => {
    const defaults = getDefaultPromptAuditFilters()
    setDraftFilters(defaults)
    setFilters(defaults)
    setPagination((current) => ({ ...current, pageIndex: 0 }))
    setRowSelection({})
    setExpanded({})
    void queryClient.invalidateQueries({ queryKey: ['prompt-audit', 'events'] })
    void queryClient.invalidateQueries({ queryKey: ['prompt-audit', 'stats'] })
  }, [queryClient])
  // Collapsing changes what a row is, so the expanded rows of the previous
  // listing cannot survive it.
  const toggleCollapseRepeats = useCallback(() => {
    const next = !collapseRepeats
    setCollapseRepeats(next)
    writePromptAuditCollapseRepeats(next)
    setExpanded({})
    setPagination((current) => ({ ...current, pageIndex: 0 }))
    setRowSelection({})
  }, [collapseRepeats])
  // Deletion always names what it removes: one record from the detail sheet, or
  // the rows the operator ticked. There is deliberately no "everything matching
  // the filters" path, which is why the header has no such button either.
  const openDelete = useCallback(
    (event?: PromptAuditEvent) => {
      if (event) {
        setDeleteFilter({ ids: [event.id] })
        return
      }
      setDeleteFilter(
        promptAuditDeleteFilter(filters, selectedIDs, {
          groups: collapseRepeats,
        })
      )
    },
    [collapseRepeats, filters, selectedIDs]
  )

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('Prompt audit')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          {canDelete && selectedIDs.length > 0 && (
            <Button variant='destructive' onClick={() => openDelete()}>
              <Trash2 />
              {t('Delete selected')} ({selectedIDs.length})
            </Button>
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 w-full flex-col gap-4'>
            <PromptAuditNavigation />
            <div className='min-h-0 flex-1'>
              <DataTablePage
                table={table}
                columns={columns}
                isLoading={listQuery.isLoading}
                isFetching={listQuery.isFetching}
                compactPagination={isMobile}
                emptyTitle={t('No prompt audit records found')}
                emptyDescription={t(
                  'Prompt audit records will appear after inspected requests.'
                )}
                skeletonKeyPrefix='prompt-audit-skeleton'
                applyHeaderSize
                tableClassName='[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
                mobileProps={{
                  enableRowSelection: canDelete,
                  getRowKey: (row) => row.id,
                }}
                toolbar={
                  <PromptAuditFilterBar
                    table={table}
                    filters={draftFilters}
                    categories={categoriesQuery.data ?? []}
                    stats={statsQuery.data}
                    statsLoading={statsQuery.isLoading}
                    searchLoading={listQuery.isFetching}
                    collapsed={collapseRepeats}
                    groupTotal={total}
                    onToggleCollapse={toggleCollapseRepeats}
                    onChange={setFilter}
                    onSearch={applyFilters}
                    onReset={resetFilters}
                  />
                }
                getRowClassName={(row, { isMobile }) => {
                  const classes: string[] = []
                  // A request revealed inside a merged row is drawn as part of
                  // the run the group forms: the row anchors the rule its time
                  // cell drops through the column, and leaves the room that rule
                  // and its dot take before the timestamp. The marks stay hidden
                  // in the cell, because only a table row can carry a rule down
                  // a column — a card holds one request and has no column to
                  // cross.
                  if (!isMobile && row.depth > 0) {
                    classes.push(
                      'relative',
                      '[&_td_.prompt-audit-timeline]:block',
                      '[&_td_.prompt-audit-timeline-text]:pl-3'
                    )
                  }
                  if (row.original.decision === 'block') {
                    classes.push('bg-rose-50/35 dark:bg-rose-950/15')
                  } else if (row.original.decision === 'flag') {
                    classes.push('bg-amber-50/35 dark:bg-amber-950/15')
                  }
                  return classes.join(' ') || undefined
                }}
              />
            </div>
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
          openDelete(event)
        }}
      />
      <PromptAuditDeleteDialog
        open={deleteFilter !== null}
        filter={deleteFilter}
        onOpenChange={(open) => !open && setDeleteFilter(null)}
        onDeleted={() => {
          setRowSelection({})
          // The deletion can take an expanded group with it. Its row closes,
          // and its cached requests are dropped before the refresh, which
          // reaches every query still mounted and would otherwise ask the
          // server again for a group that no longer exists.
          setExpanded({})
          queryClient.removeQueries({
            queryKey: ['prompt-audit', 'events', 'group'],
          })
          void queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
        }}
      />
    </>
  )
}
