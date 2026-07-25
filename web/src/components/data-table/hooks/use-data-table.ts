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
  type ColumnDef,
  type ColumnFiltersState,
  type ColumnOrderState,
  type ColumnSizingState,
  type ExpandedState,
  type OnChangeFn,
  type PaginationState,
  type RowSelectionState,
  type SortingState,
  type TableOptions,
  type Updater,
  type VisibilityState,
  getCoreRowModel,
  getExpandedRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table'
import * as React from 'react'

type DataTableFeatureOptions<TData> = Pick<
  TableOptions<TData>,
  | 'enableRowSelection'
  | 'getRowId'
  | 'getSubRows'
  | 'globalFilterFn'
  | 'autoResetPageIndex'
  | 'manualFiltering'
  | 'manualPagination'
  | 'manualSorting'
  | 'enableSorting'
  | 'enableColumnResizing'
>

type DataTableStateOptions = {
  tableStateStorageKey?: string | false
  initialSorting?: SortingState
  sortingStorageKey?: string | false
  sorting?: SortingState
  onSortingChange?: OnChangeFn<SortingState>
  initialColumnVisibility?: VisibilityState
  columnVisibilityStorageKey?: string | false
  columnVisibility?: VisibilityState
  onColumnVisibilityChange?: OnChangeFn<VisibilityState>
  initialColumnSizing?: ColumnSizingState
  columnSizingStorageKey?: string | false
  columnSizing?: ColumnSizingState
  onColumnSizingChange?: OnChangeFn<ColumnSizingState>
  initialColumnOrder?: ColumnOrderState
  columnOrderStorageKey?: string | false
  columnOrder?: ColumnOrderState
  onColumnOrderChange?: OnChangeFn<ColumnOrderState>
  initialRowSelection?: RowSelectionState
  rowSelection?: RowSelectionState
  onRowSelectionChange?: OnChangeFn<RowSelectionState>
  initialExpanded?: ExpandedState
  expanded?: ExpandedState
  onExpandedChange?: OnChangeFn<ExpandedState>
  columnFilters?: ColumnFiltersState
  onColumnFiltersChange?: OnChangeFn<ColumnFiltersState>
  globalFilter?: string
  onGlobalFilterChange?: OnChangeFn<string>
  initialPagination?: PaginationState
  pagination?: PaginationState
  onPaginationChange?: OnChangeFn<PaginationState>
  pageSizeStorageKey?: string | false
}

type DataTableRowModelOptions = {
  withFilteredRowModel?: boolean
  withPaginationRowModel?: boolean
  withSortedRowModel?: boolean
  withFacetedRowModel?: boolean
  withExpandedRowModel?: boolean
}

type UseDataTableOptions<TData> = DataTableFeatureOptions<TData> &
  DataTableStateOptions &
  DataTableRowModelOptions & {
    data: TData[]
    columns: ColumnDef<TData, unknown>[]
    totalCount?: number
    pageCount?: number
    ensurePageInRange?: (pageCount: number) => void
  }

type ColumnSizingBounds = Record<
  string,
  {
    minSize?: number
    maxSize?: number
  }
>

type ColumnWithSizing<TData> = ColumnDef<TData, unknown> & {
  accessorKey?: string | number
  columns?: ColumnDef<TData, unknown>[]
}

const COLUMN_SIZING_PERSIST_DELAY_MS = 250

function resolveUpdater<TValue>(
  updater: Updater<TValue>,
  previous: TValue
): TValue {
  return typeof updater === 'function'
    ? (updater as (old: TValue) => TValue)(previous)
    : updater
}

function useControllableTableState<TValue>(
  controlledValue: TValue | undefined,
  defaultValue: TValue,
  onChange: OnChangeFn<TValue> | undefined
): [TValue, OnChangeFn<TValue>] {
  const [uncontrolledValue, setUncontrolledValue] =
    React.useState<TValue>(defaultValue)

  const value = controlledValue ?? uncontrolledValue

  const setValue = React.useCallback<OnChangeFn<TValue>>(
    (updater) => {
      if (controlledValue === undefined) {
        setUncontrolledValue((previous) => resolveUpdater(updater, previous))
      }
      onChange?.(updater)
    },
    [controlledValue, onChange]
  )

  return [value, setValue]
}

function readColumnVisibility(
  storageKey: string | undefined,
  validColumnIds?: Set<string>
): VisibilityState {
  if (!storageKey || typeof window === 'undefined') return {}

  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return {}

    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {}
    }

    return Object.entries(parsed).reduce<VisibilityState>(
      (visibility, [key, value]) => {
        if (
          typeof value === 'boolean' &&
          (!validColumnIds || validColumnIds.has(key))
        ) {
          visibility[key] = value
        }
        return visibility
      },
      {}
    )
  } catch {
    return {}
  }
}

function getColumnId<TData>(column: ColumnDef<TData, unknown>) {
  const columnWithSizing = column as ColumnWithSizing<TData>

  if (typeof columnWithSizing.id === 'string') {
    return columnWithSizing.id
  }

  if (typeof columnWithSizing.accessorKey === 'string') {
    return columnWithSizing.accessorKey.replaceAll('.', '_')
  }

  if (typeof columnWithSizing.accessorKey === 'number') {
    return String(columnWithSizing.accessorKey)
  }

  return undefined
}

function getLeafColumnIds<TData>(
  columns: ColumnDef<TData, unknown>[]
): string[] {
  return columns.flatMap((column) => {
    const nested = (column as ColumnWithSizing<TData>).columns
    if (Array.isArray(nested) && nested.length > 0) {
      return getLeafColumnIds(nested)
    }
    const id = getColumnId(column)
    return id ? [id] : []
  })
}

function readJsonStorage(storageKey: string | undefined): unknown {
  if (!storageKey || typeof window === 'undefined') return undefined
  try {
    const raw = window.localStorage.getItem(storageKey)
    return raw ? (JSON.parse(raw) as unknown) : undefined
  } catch {
    return undefined
  }
}

function readColumnOrder(
  storageKey: string | undefined,
  validColumnIds: string[]
): ColumnOrderState {
  const parsed = readJsonStorage(storageKey)
  if (!Array.isArray(parsed)) return validColumnIds
  const valid = new Set(validColumnIds)
  const restored = parsed.filter(
    (value): value is string => typeof value === 'string' && valid.has(value)
  )
  const seen = new Set(restored)
  return [...restored, ...validColumnIds.filter((id) => !seen.has(id))]
}

function readSorting(
  storageKey: string | undefined,
  validColumnIds: Set<string>
): SortingState {
  const parsed = readJsonStorage(storageKey)
  if (!Array.isArray(parsed)) return []
  return parsed.flatMap((value) => {
    if (!value || typeof value !== 'object') return []
    const item = value as { id?: unknown; desc?: unknown }
    if (typeof item.id !== 'string' || !validColumnIds.has(item.id)) return []
    return [{ id: item.id, desc: item.desc === true }]
  })
}

function readPageSize(storageKey: string | undefined): number | undefined {
  if (!storageKey || typeof window === 'undefined') return undefined
  try {
    const value = Number(window.localStorage.getItem(storageKey))
    return Number.isInteger(value) && value > 0 ? value : undefined
  } catch {
    return undefined
  }
}

function resolveStorageKey(
  explicitKey: string | false | undefined,
  tableKey: string | undefined,
  suffix: string
): string | undefined {
  if (explicitKey === false) return undefined
  if (typeof explicitKey === 'string') return explicitKey
  return tableKey ? `${tableKey}:${suffix}` : undefined
}

function buildColumnSizingBounds<TData>(
  columns: ColumnDef<TData, unknown>[]
): ColumnSizingBounds {
  return columns.reduce<ColumnSizingBounds>((bounds, column) => {
    const columnWithSizing = column as ColumnWithSizing<TData>
    const columnId = getColumnId(column)

    if (columnId) {
      const minSize =
        typeof columnWithSizing.minSize === 'number' &&
        Number.isFinite(columnWithSizing.minSize)
          ? columnWithSizing.minSize
          : undefined
      const maxSize =
        typeof columnWithSizing.maxSize === 'number' &&
        Number.isFinite(columnWithSizing.maxSize)
          ? columnWithSizing.maxSize
          : undefined

      if (minSize !== undefined || maxSize !== undefined) {
        bounds[columnId] = { minSize, maxSize }
      }
    }

    if (Array.isArray(columnWithSizing.columns)) {
      Object.assign(bounds, buildColumnSizingBounds(columnWithSizing.columns))
    }

    return bounds
  }, {})
}

function getBoundedColumnSize(
  columnId: string,
  value: unknown,
  bounds: ColumnSizingBounds
) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return undefined
  }

  const columnBounds = bounds[columnId]
  let size = value

  if (columnBounds?.minSize !== undefined && size < columnBounds.minSize) {
    size = columnBounds.minSize
  }

  if (columnBounds?.maxSize !== undefined && size > columnBounds.maxSize) {
    size = columnBounds.maxSize
  }

  return size > 0 ? size : undefined
}

function readColumnSizing(
  storageKey: string | undefined,
  bounds: ColumnSizingBounds,
  validColumnIds?: Set<string>
): ColumnSizingState {
  if (!storageKey || typeof window === 'undefined') return {}

  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return {}

    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {}
    }

    return Object.entries(parsed).reduce<ColumnSizingState>(
      (sizing, [key, value]) => {
        if (validColumnIds && !validColumnIds.has(key)) return sizing
        const boundedSize = getBoundedColumnSize(key, value, bounds)

        if (boundedSize !== undefined) {
          sizing[key] = boundedSize
        }
        return sizing
      },
      {}
    )
  } catch {
    return {}
  }
}

export function useDataTable<TData>(options: UseDataTableOptions<TData>) {
  const {
    data,
    columns,
    totalCount,
    pageCount: explicitPageCount,
    ensurePageInRange,
    manualFiltering,
    manualPagination,
    manualSorting,
    initialSorting = [],
    initialColumnVisibility = {},
    initialColumnSizing = {},
    initialColumnOrder = [],
    initialRowSelection = {},
    initialExpanded = {},
    initialPagination = { pageIndex: 0, pageSize: 20 },
    withFilteredRowModel = !manualFiltering,
    withPaginationRowModel = !manualPagination,
    withSortedRowModel = !manualSorting && !manualPagination,
    withFacetedRowModel = !manualFiltering,
    withExpandedRowModel = false,
  } = options

  const tableStateStorageKey =
    typeof options.tableStateStorageKey === 'string'
      ? options.tableStateStorageKey
      : undefined

  const columnVisibilityStorageKey = resolveStorageKey(
    options.columnVisibilityStorageKey,
    tableStateStorageKey,
    'column-visibility'
  )
  const columnSizingStorageKey = resolveStorageKey(
    options.columnSizingStorageKey,
    tableStateStorageKey,
    'column-sizing'
  )
  const columnOrderStorageKey = resolveStorageKey(
    options.columnOrderStorageKey,
    tableStateStorageKey,
    'column-order'
  )
  const sortingStorageKey = resolveStorageKey(
    options.sortingStorageKey,
    tableStateStorageKey,
    'sorting'
  )
  const pageSizeStorageKey = resolveStorageKey(
    options.pageSizeStorageKey,
    tableStateStorageKey,
    'page-size'
  )
  const leafColumnIds = React.useMemo(
    () => getLeafColumnIds(columns),
    [columns]
  )
  const validColumnIds = React.useMemo(
    () => new Set(leafColumnIds),
    [leafColumnIds]
  )
  const resolvedInitialSorting = React.useMemo(() => {
    const restored = readSorting(sortingStorageKey, validColumnIds)
    return restored.length > 0 ? restored : initialSorting
  }, [initialSorting, sortingStorageKey, validColumnIds])
  const resolvedInitialColumnVisibility = React.useMemo(
    () => ({
      ...initialColumnVisibility,
      ...readColumnVisibility(columnVisibilityStorageKey, validColumnIds),
    }),
    [columnVisibilityStorageKey, initialColumnVisibility, validColumnIds]
  )
  const columnSizingBounds = React.useMemo(
    () => buildColumnSizingBounds(columns),
    [columns]
  )
  const resolvedInitialColumnSizing = React.useMemo(
    () => ({
      ...initialColumnSizing,
      ...readColumnSizing(
        columnSizingStorageKey,
        columnSizingBounds,
        validColumnIds
      ),
    }),
    [
      columnSizingBounds,
      columnSizingStorageKey,
      initialColumnSizing,
      validColumnIds,
    ]
  )
  const resolvedInitialColumnOrder = React.useMemo(() => {
    const defaultOrder =
      initialColumnOrder.length > 0 ? initialColumnOrder : leafColumnIds
    const restored = readColumnOrder(columnOrderStorageKey, leafColumnIds)
    return columnOrderStorageKey ? restored : defaultOrder
  }, [columnOrderStorageKey, initialColumnOrder, leafColumnIds])
  const resolvedInitialPagination = React.useMemo(
    () => ({
      ...initialPagination,
      pageSize: readPageSize(pageSizeStorageKey) ?? initialPagination.pageSize,
    }),
    [initialPagination, pageSizeStorageKey]
  )

  const [sorting, onSortingChange] = useControllableTableState(
    options.sorting,
    resolvedInitialSorting,
    options.onSortingChange
  )
  const [columnVisibility, onColumnVisibilityChange] =
    useControllableTableState(
      options.columnVisibility,
      resolvedInitialColumnVisibility,
      options.onColumnVisibilityChange
    )
  const [columnSizing, onColumnSizingChange] = useControllableTableState(
    options.columnSizing,
    resolvedInitialColumnSizing,
    options.onColumnSizingChange
  )
  const [columnOrder, onColumnOrderChange] = useControllableTableState(
    options.columnOrder,
    resolvedInitialColumnOrder,
    options.onColumnOrderChange
  )
  const hydratedColumnVisibilityStorageKeyRef = React.useRef(
    columnVisibilityStorageKey
  )
  const hydratedColumnSizingStorageKeyRef = React.useRef(columnSizingStorageKey)
  const skipNextColumnVisibilityPersistRef = React.useRef(false)
  const skipNextColumnSizingPersistRef = React.useRef(false)
  const hydratedColumnOrderStorageKeyRef = React.useRef<string | undefined>(
    undefined
  )
  const skipNextColumnOrderPersistRef = React.useRef(false)
  const hydratedSortingStorageKeyRef = React.useRef<string | undefined>(
    undefined
  )
  const skipNextSortingPersistRef = React.useRef(false)
  const hydratedPageSizeStorageKeyRef = React.useRef<string | undefined>(
    undefined
  )
  const skipNextPageSizePersistRef = React.useRef(false)
  const columnSizingPersistTimerRef = React.useRef<number | undefined>(
    undefined
  )
  const [rowSelection, onRowSelectionChange] = useControllableTableState(
    options.rowSelection,
    initialRowSelection,
    options.onRowSelectionChange
  )
  const [expanded, onExpandedChange] = useControllableTableState(
    options.expanded,
    initialExpanded,
    options.onExpandedChange
  )
  const [pagination, onPaginationChange] = useControllableTableState(
    options.pagination,
    resolvedInitialPagination,
    options.onPaginationChange
  )

  const resolvedPageCount =
    explicitPageCount ??
    (totalCount !== undefined
      ? Math.ceil(totalCount / pagination.pageSize)
      : undefined)
  const resolvedEnableSorting =
    options.enableSorting ??
    (!manualPagination ||
      Boolean(options.sorting) ||
      Boolean(options.onSortingChange))
  const resolvedEnableColumnResizing =
    options.enableColumnResizing ??
    Boolean(
      tableStateStorageKey &&
      typeof window !== 'undefined' &&
      !window.matchMedia('(max-width: 640px)').matches
    )

  const table = useReactTable({
    data,
    columns,
    rowCount: totalCount,
    pageCount: resolvedPageCount,
    state: {
      sorting,
      columnVisibility,
      columnSizing,
      columnOrder,
      rowSelection,
      expanded,
      columnFilters: options.columnFilters,
      globalFilter: options.globalFilter,
      pagination,
    },
    enableRowSelection: options.enableRowSelection,
    enableSorting: resolvedEnableSorting,
    getRowId: options.getRowId,
    getSubRows: options.getSubRows,
    globalFilterFn: options.globalFilterFn,
    autoResetPageIndex: options.autoResetPageIndex,
    manualFiltering,
    manualPagination,
    manualSorting,
    enableColumnResizing: resolvedEnableColumnResizing,
    columnResizeMode: 'onChange',
    onSortingChange,
    onColumnVisibilityChange,
    onColumnSizingChange,
    onColumnOrderChange,
    onRowSelectionChange,
    onExpandedChange,
    onColumnFiltersChange: options.onColumnFiltersChange,
    onGlobalFilterChange: options.onGlobalFilterChange,
    onPaginationChange,
    meta: {
      resetPersistedView: () => {
        if (typeof window !== 'undefined') {
          for (const key of [
            columnVisibilityStorageKey,
            columnSizingStorageKey,
            columnOrderStorageKey,
            sortingStorageKey,
            pageSizeStorageKey,
          ]) {
            if (key) window.localStorage.removeItem(key)
          }
        }
        onColumnVisibilityChange(() => initialColumnVisibility)
        onColumnSizingChange(() => initialColumnSizing)
        onColumnOrderChange(() => initialColumnOrder)
        onSortingChange(() => initialSorting)
        onPaginationChange(() => ({ ...initialPagination, pageIndex: 0 }))
      },
    },
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: withFilteredRowModel
      ? getFilteredRowModel()
      : undefined,
    getPaginationRowModel: withPaginationRowModel
      ? getPaginationRowModel()
      : undefined,
    getSortedRowModel: withSortedRowModel ? getSortedRowModel() : undefined,
    getFacetedRowModel: withFacetedRowModel ? getFacetedRowModel() : undefined,
    getFacetedUniqueValues: withFacetedRowModel
      ? getFacetedUniqueValues()
      : undefined,
    getExpandedRowModel: withExpandedRowModel
      ? getExpandedRowModel()
      : undefined,
  })

  const actualPageCount = table.getPageCount()
  React.useEffect(() => {
    ensurePageInRange?.(actualPageCount)
  }, [actualPageCount, ensurePageInRange])

  React.useEffect(() => {
    if (
      options.columnVisibility !== undefined ||
      columnVisibilityStorageKey ===
        hydratedColumnVisibilityStorageKeyRef.current
    ) {
      return
    }

    hydratedColumnVisibilityStorageKeyRef.current = columnVisibilityStorageKey
    skipNextColumnVisibilityPersistRef.current = true
    onColumnVisibilityChange(() => resolvedInitialColumnVisibility)
  }, [
    columnVisibilityStorageKey,
    onColumnVisibilityChange,
    options.columnVisibility,
    resolvedInitialColumnVisibility,
  ])

  React.useEffect(() => {
    if (
      options.columnSizing !== undefined ||
      columnSizingStorageKey === hydratedColumnSizingStorageKeyRef.current
    ) {
      return
    }

    hydratedColumnSizingStorageKeyRef.current = columnSizingStorageKey
    skipNextColumnSizingPersistRef.current = true
    onColumnSizingChange(() => resolvedInitialColumnSizing)
  }, [
    columnSizingStorageKey,
    onColumnSizingChange,
    options.columnSizing,
    resolvedInitialColumnSizing,
  ])

  React.useEffect(() => {
    if (
      options.columnOrder !== undefined ||
      columnOrderStorageKey === hydratedColumnOrderStorageKeyRef.current
    ) {
      return
    }
    hydratedColumnOrderStorageKeyRef.current = columnOrderStorageKey
    skipNextColumnOrderPersistRef.current = true
    onColumnOrderChange(() => resolvedInitialColumnOrder)
  }, [
    columnOrderStorageKey,
    onColumnOrderChange,
    options.columnOrder,
    resolvedInitialColumnOrder,
  ])

  React.useEffect(() => {
    if (
      options.sorting === undefined ||
      !sortingStorageKey ||
      sortingStorageKey === hydratedSortingStorageKeyRef.current
    ) {
      return
    }
    hydratedSortingStorageKeyRef.current = sortingStorageKey
    skipNextSortingPersistRef.current = true
    onSortingChange(() => resolvedInitialSorting)
  }, [
    onSortingChange,
    options.sorting,
    resolvedInitialSorting,
    sortingStorageKey,
  ])

  React.useEffect(() => {
    if (
      options.pagination === undefined ||
      !pageSizeStorageKey ||
      pageSizeStorageKey === hydratedPageSizeStorageKeyRef.current
    ) {
      return
    }
    hydratedPageSizeStorageKeyRef.current = pageSizeStorageKey
    const storedPageSize = readPageSize(pageSizeStorageKey)
    if (!storedPageSize || storedPageSize === pagination.pageSize) return
    skipNextPageSizePersistRef.current = true
    onPaginationChange(() => ({ pageIndex: 0, pageSize: storedPageSize }))
  }, [
    onPaginationChange,
    options.pagination,
    pageSizeStorageKey,
    pagination.pageSize,
  ])

  React.useEffect(() => {
    if (!columnVisibilityStorageKey || typeof window === 'undefined') return

    if (skipNextColumnVisibilityPersistRef.current) {
      skipNextColumnVisibilityPersistRef.current = false
      return
    }

    try {
      window.localStorage.setItem(
        columnVisibilityStorageKey,
        JSON.stringify(columnVisibility)
      )
    } catch {
      // Storage can be unavailable in private mode; table controls still work.
    }
  }, [columnVisibility, columnVisibilityStorageKey])

  React.useEffect(() => {
    if (!columnSizingStorageKey || typeof window === 'undefined') return

    if (skipNextColumnSizingPersistRef.current) {
      skipNextColumnSizingPersistRef.current = false
      return
    }

    if (columnSizingPersistTimerRef.current !== undefined) {
      window.clearTimeout(columnSizingPersistTimerRef.current)
    }

    columnSizingPersistTimerRef.current = window.setTimeout(() => {
      try {
        window.localStorage.setItem(
          columnSizingStorageKey,
          JSON.stringify(columnSizing)
        )
      } catch {
        // Storage can be unavailable in private mode; table controls still work.
      } finally {
        columnSizingPersistTimerRef.current = undefined
      }
    }, COLUMN_SIZING_PERSIST_DELAY_MS)

    return () => {
      if (columnSizingPersistTimerRef.current !== undefined) {
        window.clearTimeout(columnSizingPersistTimerRef.current)
        columnSizingPersistTimerRef.current = undefined
      }
    }
  }, [columnSizing, columnSizingStorageKey])

  React.useEffect(() => {
    if (!columnOrderStorageKey || typeof window === 'undefined') return
    if (skipNextColumnOrderPersistRef.current) {
      skipNextColumnOrderPersistRef.current = false
      return
    }
    try {
      window.localStorage.setItem(
        columnOrderStorageKey,
        JSON.stringify(columnOrder)
      )
    } catch {
      // Storage can be unavailable in private mode.
    }
  }, [columnOrder, columnOrderStorageKey])

  React.useEffect(() => {
    if (!sortingStorageKey || typeof window === 'undefined') return
    if (skipNextSortingPersistRef.current) {
      skipNextSortingPersistRef.current = false
      return
    }
    try {
      window.localStorage.setItem(sortingStorageKey, JSON.stringify(sorting))
    } catch {
      // Storage can be unavailable in private mode.
    }
  }, [sorting, sortingStorageKey])

  React.useEffect(() => {
    if (!pageSizeStorageKey || typeof window === 'undefined') return
    if (skipNextPageSizePersistRef.current) {
      skipNextPageSizePersistRef.current = false
      return
    }
    try {
      window.localStorage.setItem(
        pageSizeStorageKey,
        String(pagination.pageSize)
      )
    } catch {
      // Storage can be unavailable in private mode.
    }
  }, [pageSizeStorageKey, pagination.pageSize])

  return {
    table,
  }
}
