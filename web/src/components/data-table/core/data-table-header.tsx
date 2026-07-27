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
  type ColumnSizingState,
  flexRender,
  type Header,
  type Table as TanstackTable,
} from '@tanstack/react-table'
import type { KeyboardEvent, MouseEvent, PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'

import { DataTableColumnHeader } from './column-header'
import { isContentSizedColumn } from './content-sized-columns'
import type { DataTableColumnClassName } from './types'

type DataTableHeaderProps<TData> = {
  table: TanstackTable<TData>
  applyHeaderSize?: boolean
  className?: string
  rowClassName?: string
  getColumnClassName?: DataTableColumnClassName
}

export function DataTableHeader<TData>({
  table,
  applyHeaderSize,
  className,
  rowClassName,
  getColumnClassName,
}: DataTableHeaderProps<TData>) {
  const { t } = useTranslation()

  return (
    <TableHeader className={className}>
      {table.getHeaderGroups().map((headerGroup) => (
        <TableRow key={headerGroup.id} className={rowClassName}>
          {headerGroup.headers.map((header) => (
            <TableHead
              key={header.id}
              colSpan={header.colSpan}
              data-column-id={header.column.id}
              className={cn(
                'relative',
                getColumnClassName?.(header.column.id, 'header')
              )}
              style={getHeaderSizeStyle(header, applyHeaderSize)}
            >
              {renderHeaderContent(header)}
              {shouldRenderColumnResizer(table, header) && (
                <div
                  role='separator'
                  aria-orientation='vertical'
                  aria-label={t('Resize column')}
                  data-column-resizer
                  tabIndex={0}
                  onDoubleClick={(event) =>
                    handleColumnAutoSize(event, table, header)
                  }
                  onPointerDown={(event) =>
                    handleColumnResizeStart(event, table, header)
                  }
                  onKeyDown={(event) =>
                    handleColumnResizeKeyDown(event, table, header)
                  }
                  className={cn(
                    'absolute top-0 right-0 h-full w-2 cursor-col-resize touch-none select-none',
                    'after:bg-border hover:after:bg-primary after:absolute after:top-2 after:right-0 after:h-[calc(100%-1rem)] after:w-px after:transition-colors',
                    'data-[resizing=true]:after:bg-primary'
                  )}
                />
              )}
            </TableHead>
          ))}
        </TableRow>
      ))}
    </TableHeader>
  )
}

function handleColumnResizeKeyDown<TData>(
  event: KeyboardEvent<HTMLDivElement>,
  table: TanstackTable<TData>,
  header: Header<TData, unknown>
) {
  const step = event.shiftKey ? 50 : 10

  if (event.key === 'ArrowLeft') {
    event.preventDefault()
    resizeColumnByKeyboard(event.currentTarget, table, header, -step)
    return
  }

  if (event.key === 'ArrowRight') {
    event.preventDefault()
    resizeColumnByKeyboard(event.currentTarget, table, header, step)
    return
  }

  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    autoSizeColumn(event.currentTarget, table, header)
  }
}

function handleColumnResizeStart<TData>(
  event: PointerEvent<HTMLDivElement>,
  table: TanstackTable<TData>,
  header: Header<TData, unknown>
) {
  if (
    event.isPrimary === false ||
    (event.pointerType === 'mouse' && event.button !== 0)
  ) {
    return
  }

  const renderedSizing =
    measureRenderedColumnSizing(event.currentTarget, table) ??
    Object.fromEntries(
      table
        .getVisibleLeafColumns()
        .map((column) => [column.id, column.getSize()])
    )

  event.preventDefault()
  const resizerElement = event.currentTarget
  resizerElement.dataset.resizing = 'true'
  const ownerDocument = event.currentTarget.ownerDocument
  const pointerId = event.pointerId
  try {
    resizerElement.setPointerCapture(pointerId)
  } catch {
    // Synthetic pointer events may not have an active pointer to capture.
  }
  const startOffset = event.clientX
  const deltaDirection = table.options.columnResizeDirection === 'rtl' ? -1 : 1
  const columnSizingStart = header.getLeafHeaders().map((leafHeader) => ({
    header: leafHeader,
    size: renderedSizing[leafHeader.column.id] ?? leafHeader.column.getSize(),
  }))
  const startSize = columnSizingStart.reduce(
    (total, column) => total + column.size,
    0
  )
  let didMove = false

  const updateSizing = (clientX: number) => {
    const deltaOffset = (clientX - startOffset) * deltaDirection
    const deltaPercentage = Math.max(deltaOffset / startSize, -0.999999)
    const resizedColumns = Object.fromEntries(
      columnSizingStart.map((column) => [
        column.header.column.id,
        getClampedColumnSize(
          column.header,
          column.size + column.size * deltaPercentage
        ),
      ])
    )

    table.setColumnSizing((previous) => {
      const nextSizing = { ...renderedSizing, ...resizedColumns }
      const didChange = Object.entries(nextSizing).some(
        ([columnId, size]) => previous[columnId] !== size
      )

      return didChange ? { ...previous, ...nextSizing } : previous
    })
  }

  const finishResize = (pointerEvent: globalThis.PointerEvent) => {
    if (pointerEvent.pointerId !== pointerId) return
    if (didMove && pointerEvent.type === 'pointerup') {
      updateSizing(pointerEvent.clientX)
    }
    ownerDocument.removeEventListener('pointermove', handlePointerMove)
    ownerDocument.removeEventListener('pointerup', finishResize)
    ownerDocument.removeEventListener('pointercancel', finishResize)
    if (
      typeof resizerElement.hasPointerCapture === 'function' &&
      resizerElement.hasPointerCapture(pointerId)
    ) {
      resizerElement.releasePointerCapture(pointerId)
    }
    delete resizerElement.dataset.resizing
  }

  const handlePointerMove = (pointerEvent: globalThis.PointerEvent) => {
    if (pointerEvent.pointerId !== pointerId) return
    if (pointerEvent.cancelable) {
      pointerEvent.preventDefault()
    }
    didMove = true
    updateSizing(pointerEvent.clientX)
  }

  ownerDocument.addEventListener('pointermove', handlePointerMove, {
    passive: false,
  })
  ownerDocument.addEventListener('pointerup', finishResize)
  ownerDocument.addEventListener('pointercancel', finishResize)
}

function resizeColumnByKeyboard<TData>(
  resizerElement: HTMLElement,
  table: TanstackTable<TData>,
  header: Header<TData, unknown>,
  delta: number
) {
  const renderedSizing = measureRenderedColumnSizing(resizerElement, table)
  const currentSize =
    renderedSizing?.[header.column.id] ?? header.column.getSize()

  table.setColumnSizing((previous) => ({
    ...previous,
    ...renderedSizing,
    [header.column.id]: getClampedColumnSize(header, currentSize + delta),
  }))
}

function handleColumnAutoSize<TData>(
  event: MouseEvent<HTMLDivElement>,
  table: TanstackTable<TData>,
  header: Header<TData, unknown>
) {
  event.preventDefault()
  autoSizeColumn(event.currentTarget, table, header)
}

function autoSizeColumn<TData>(
  resizerElement: HTMLElement,
  table: TanstackTable<TData>,
  header: Header<TData, unknown>
) {
  const measuredSize = measureColumnContentWidth(
    resizerElement,
    header.column.id
  )

  if (measuredSize === undefined) {
    return
  }

  table.setColumnSizing((previous) => ({
    ...previous,
    ...measureRenderedColumnSizing(resizerElement, table),
    [header.column.id]: getClampedColumnSize(header, measuredSize),
  }))
}

function measureRenderedColumnSizing<TData>(
  resizerElement: HTMLElement,
  table: TanstackTable<TData>
): ColumnSizingState | undefined {
  const tableElement = resizerElement.closest('table')
  if (!tableElement) {
    return undefined
  }

  const sizing: ColumnSizingState = {}

  for (const column of table.getVisibleLeafColumns()) {
    const cells = tableElement.querySelectorAll<HTMLElement>(
      getColumnElementSelector(column.id)
    )
    if (cells.length === 0) {
      return undefined
    }

    const renderedWidth = [...cells].reduce(
      (width, cell) => Math.max(width, cell.getBoundingClientRect().width),
      0
    )
    const contentWidth = isContentSizedColumn(column.id)
      ? [...cells].reduce(
          (width, cell) => Math.max(width, cell.scrollWidth),
          renderedWidth
        )
      : renderedWidth
    const measuredWidth = Math.round(contentWidth * 100) / 100

    if (!Number.isFinite(measuredWidth) || measuredWidth <= 0) {
      return undefined
    }

    sizing[column.id] = measuredWidth
  }

  return sizing
}

function getClampedColumnSize<TData>(
  header: Header<TData, unknown>,
  nextSize: number
) {
  const { minSize, maxSize } = header.column.columnDef

  if (typeof minSize === 'number' && nextSize < minSize) {
    return minSize
  }

  if (typeof maxSize === 'number' && nextSize > maxSize) {
    return maxSize
  }

  return nextSize
}

function measureColumnContentWidth(
  resizerElement: HTMLElement,
  columnId: string
) {
  const tableElement = resizerElement.closest('table')
  if (!tableElement) {
    return undefined
  }

  const cells = tableElement.querySelectorAll<HTMLElement>(
    getColumnElementSelector(columnId)
  )
  if (cells.length === 0) {
    return undefined
  }

  const measuredWidth = [...cells].reduce(
    (maxWidth, cell) => Math.max(maxWidth, measureElementWidth(cell)),
    0
  )

  return measuredWidth > 0 ? Math.ceil(measuredWidth) : undefined
}

function measureElementWidth(element: HTMLElement) {
  const clone = element.cloneNode(true) as HTMLElement

  clone.querySelectorAll('[data-column-resizer]').forEach((resizer) => {
    resizer.remove()
  })

  clone.style.position = 'absolute'
  clone.style.visibility = 'hidden'
  clone.style.pointerEvents = 'none'
  clone.style.left = '-10000px'
  clone.style.top = '0'
  clone.style.width = 'max-content'
  clone.style.minWidth = '0'
  clone.style.maxWidth = 'none'
  clone.style.height = 'auto'
  clone.style.whiteSpace = 'nowrap'

  document.body.append(clone)
  const width = clone.scrollWidth
  clone.remove()

  return width
}

function getColumnElementSelector(columnId: string) {
  const escapedColumnId =
    typeof CSS !== 'undefined' && typeof CSS.escape === 'function'
      ? CSS.escape(columnId)
      : columnId.replaceAll('\\', '\\\\').replaceAll('"', '\\"')

  return `[data-column-id="${escapedColumnId}"]`
}

function shouldRenderColumnResizer<TData>(
  table: TanstackTable<TData>,
  header: Header<TData, unknown>
) {
  return (
    table.options.enableColumnResizing === true &&
    !header.isPlaceholder &&
    header.column.getCanResize() &&
    !isContentSizedColumn(header.column.id)
  )
}

function getHeaderSizeStyle<TData>(
  header: Header<TData, unknown>,
  applyHeaderSize: boolean | undefined
) {
  if (!applyHeaderSize || isContentSizedColumn(header.column.id)) {
    return undefined
  }

  return { width: header.getSize() }
}

function renderHeaderContent<TData>(header: Header<TData, unknown>) {
  if (header.isPlaceholder) return null
  const { header: headerDef, meta } = header.column.columnDef
  // A string header means the user wrote e.g. `header: t('Name')` — auto-render
  // with DataTableColumnHeader so sorting works without boilerplate.
  // A function (including TanStack's default accessor-key fallback) is passed
  // through as-is. meta.label is kept as a fallback for legacy columns.
  if (typeof headerDef === 'string') {
    return <DataTableColumnHeader column={header.column} title={headerDef} />
  }
  if (meta?.label) {
    return <DataTableColumnHeader column={header.column} title={meta.label} />
  }
  return flexRender(headerDef, header.getContext())
}
