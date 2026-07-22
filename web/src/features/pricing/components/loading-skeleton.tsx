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
import { Skeleton } from '@/components/ui/skeleton'

import { VIEW_MODES, type ViewMode } from '../constants'

const SIDEBAR_SKELETON_IDS = [
  'sidebar-1',
  'sidebar-2',
  'sidebar-3',
  'sidebar-4',
  'sidebar-5',
  'sidebar-6',
]
const CARD_SKELETON_IDS = [
  'card-1',
  'card-2',
  'card-3',
  'card-4',
  'card-5',
  'card-6',
  'card-7',
  'card-8',
  'card-9',
]
const FILTER_SKELETONS = [
  { id: 'filter-1', width: 80 },
  { id: 'filter-2', width: 90 },
  { id: 'filter-3', width: 75 },
  { id: 'filter-4', width: 85 },
  { id: 'filter-5', width: 70 },
]
const TABLE_COLUMNS = [
  { id: 'model', width: 200 },
  { id: 'type', width: 110 },
  { id: 'price', width: 180 },
  { id: 'cached', width: 110 },
  { id: 'vendor', width: 130 },
  { id: 'actions', width: 90 },
]
const TABLE_ROW_SKELETON_IDS = [
  'row-1',
  'row-2',
  'row-3',
  'row-4',
  'row-5',
  'row-6',
  'row-7',
  'row-8',
  'row-9',
  'row-10',
]
const PAGINATION_SKELETON_IDS = ['previous', 'page-1', 'page-2', 'next']

export interface LoadingSkeletonProps {
  viewMode?: ViewMode
}

export function LoadingSkeleton(props: LoadingSkeletonProps) {
  const viewMode = props.viewMode ?? VIEW_MODES.CARD

  return (
    <div>
      <Skeleton className='mb-4 h-10 w-full max-w-2xl rounded-lg' />
      <div className='grid gap-4 xl:grid-cols-[330px_minmax(0,1fr)]'>
        <div className='hidden rounded-xl border p-4 xl:flex xl:flex-col xl:gap-4'>
          <Skeleton className='h-5 w-24' />
          {SIDEBAR_SKELETON_IDS.map((id) => (
            <Skeleton key={id} className='h-8 w-full rounded-lg' />
          ))}
        </div>
        <div className='flex min-w-0 flex-col gap-4'>
          <FilterBarSkeleton />
          {viewMode === VIEW_MODES.TABLE ? (
            <TableContentSkeleton />
          ) : (
            <CardContentSkeleton />
          )}
        </div>
      </div>
    </div>
  )
}

function CardContentSkeleton() {
  return (
    <div className='grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3'>
      {CARD_SKELETON_IDS.map((id) => (
        <div key={id} className='rounded-xl border p-5'>
          <div className='flex items-start justify-between gap-3'>
            <div className='flex min-w-0 items-start gap-3'>
              <Skeleton className='size-10 shrink-0 rounded-xl' />
              <div className='min-w-0 flex-1 space-y-2'>
                <Skeleton className='h-5 w-36' />
                <Skeleton className='h-3.5 w-48' />
              </div>
            </div>
            <Skeleton className='h-8 w-16 rounded-md' />
          </div>
          <Skeleton className='mt-4 h-3.5 w-4/5' />
          <div className='mt-4 flex items-center justify-between gap-3'>
            <div className='flex items-center gap-2'>
              <Skeleton className='h-3.5 w-14' />
              <Skeleton className='h-3.5 w-8' />
              <Skeleton className='h-4 w-16' />
            </div>
            <Skeleton className='h-3.5 w-14' />
          </div>
        </div>
      ))}
    </div>
  )
}

function FilterBarSkeleton() {
  return (
    <div className='space-y-3'>
      <div className='flex items-center gap-3'>
        <div className='flex flex-1 flex-wrap items-center gap-2'>
          {FILTER_SKELETONS.map((filter) => (
            <Skeleton
              key={filter.id}
              className='h-8 rounded-lg'
              style={{ width: `${filter.width}px` }}
            />
          ))}
        </div>
        <div className='flex items-center gap-2'>
          <Skeleton className='h-8 w-24 rounded-lg' />
          <Skeleton className='h-8 w-20 rounded-lg' />
          <Skeleton className='h-8 w-24' />
          <Skeleton className='h-8 w-20 rounded-lg' />
        </div>
      </div>
      <Skeleton className='h-5 w-24' />
    </div>
  )
}

function TableContentSkeleton() {
  return (
    <div className='space-y-4'>
      <div className='overflow-hidden rounded-lg border'>
        <div className='bg-muted/30 border-b px-4 py-3'>
          <div className='flex items-center gap-4'>
            {TABLE_COLUMNS.map((column) => (
              <Skeleton
                key={column.id}
                className='h-4'
                style={{ width: `${column.width}px` }}
              />
            ))}
          </div>
        </div>
        {TABLE_ROW_SKELETON_IDS.map((rowId) => (
          <div
            key={rowId}
            className='flex items-center gap-4 border-b px-4 py-3 last:border-b-0'
          >
            {TABLE_COLUMNS.map((column) => (
              <Skeleton
                key={column.id}
                className='h-5'
                style={{ width: `${column.width}px` }}
              />
            ))}
          </div>
        ))}
      </div>
      <div className='flex items-center justify-between'>
        <Skeleton className='h-5 w-32' />
        <div className='flex items-center gap-2'>
          {PAGINATION_SKELETON_IDS.map((id) => (
            <Skeleton key={id} className='size-8' />
          ))}
        </div>
      </div>
    </div>
  )
}
