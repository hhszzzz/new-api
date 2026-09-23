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
import type { Table } from '@tanstack/react-table'
import { Rows3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import type {
  PromptAuditCategory,
  PromptAuditFilters,
  PromptAuditStats,
} from '../types'

function PromptAuditStat(props: {
  label: string
  value: number
  accent: string
}) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {formatNumber(props.value)}
      </span>
    </span>
  )
}

function PromptAuditStatsBar(props: {
  stats?: PromptAuditStats
  loading: boolean
  collapsed: boolean
  groupTotal: number
  recordsTotal: number
}) {
  const { t } = useTranslation()

  if (props.loading) {
    return (
      <div className='flex flex-wrap items-center gap-2'>
        <Skeleton className='h-7 w-24 rounded-md' />
        <Skeleton className='h-7 w-24 rounded-md' />
        <Skeleton className='h-7 w-24 rounded-md' />
        <Skeleton className='h-7 w-24 rounded-md' />
      </div>
    )
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <PromptAuditStat
        label={t('Total')}
        value={props.stats?.total ?? 0}
        accent='bg-sky-500/70'
      />
      <PromptAuditStat
        label={t('Blocked')}
        value={props.stats?.decisions.block ?? 0}
        accent='bg-rose-500/70'
      />
      <PromptAuditStat
        label={t('Flagged')}
        value={props.stats?.decisions.flag ?? 0}
        accent='bg-amber-500/75'
      />
      <PromptAuditStat
        label={t('Failed')}
        value={props.stats?.statuses.failed ?? 0}
        accent='bg-slate-400/80'
      />
      {/* While the listing merges requests, the two counts that used to be one
          are shown together, so nobody has to reconcile them by hand. */}
      {props.collapsed && (
        <>
          <PromptAuditStat
            label={t('Groups')}
            value={props.groupTotal}
            accent='bg-violet-500/70'
          />
          <PromptAuditStat
            label={t('Requests')}
            value={props.recordsTotal}
            accent='bg-emerald-500/70'
          />
        </>
      )}
    </div>
  )
}

/**
 * The collapse switch. It decides how the listing is read, not which records it
 * contains, so it sits with the actions instead of the filters.
 */
function PromptAuditCollapseToggle(props: {
  collapsed: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const label = t('Collapse repeated audits')

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            // Merging the repeated requests is a different reading of the same
            // records, and a bare icon that only changes color is far too easy to
            // leave on without noticing: the pressed state fills in.
            variant={props.collapsed ? 'secondary' : 'ghost'}
            size='icon'
            onClick={props.onToggle}
            aria-pressed={props.collapsed}
            aria-label={label}
            className={cn(
              'text-muted-foreground hover:text-foreground size-7 max-sm:size-11',
              props.collapsed && 'text-primary hover:text-primary'
            )}
          >
            <Rows3 />
          </Button>
        }
      />
      <TooltipContent>
        <p>{label}</p>
        <p>{t('Merge requests that submitted the same text into one row.')}</p>
      </TooltipContent>
    </Tooltip>
  )
}

function PromptAuditFilterSelect(props: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
}) {
  return (
    <LogsFilterField>
      <Combobox
        options={props.options}
        value={props.value}
        onValueChange={(value) => props.onChange(value ?? '')}
        aria-label={props.label}
        className='h-8 w-full text-sm leading-5'
      />
    </LogsFilterField>
  )
}

function filterDateValue(value?: Date) {
  return value ? dayjs(value).format('YYYY-MM-DDTHH:mm') : ''
}

export function PromptAuditFilterBar<TData>(props: {
  table: Table<TData>
  filters: PromptAuditFilters
  categories: PromptAuditCategory[]
  stats?: PromptAuditStats
  statsLoading: boolean
  searchLoading: boolean
  collapsed: boolean
  groupTotal: number
  recordsTotal: number
  onToggleCollapse: () => void
  onChange: (key: keyof PromptAuditFilters, value: string) => void
  onSearch: () => void
  onReset: () => void
}) {
  const { t } = useTranslation()
  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter') props.onSearch()
  }
  const dateFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={
          props.filters.start_time
            ? new Date(props.filters.start_time)
            : undefined
        }
        end={
          props.filters.end_time ? new Date(props.filters.end_time) : undefined
        }
        onChange={({ start, end }) => {
          props.onChange('start_time', filterDateValue(start))
          props.onChange('end_time', filterDateValue(end))
        }}
      />
    </LogsFilterField>
  )
  const decisionFilter = (
    <PromptAuditFilterSelect
      label={t('Decision')}
      value={props.filters.decision || 'all'}
      options={[
        { value: 'all', label: t('All decisions') },
        ...['pass', 'flag', 'block', 'unavailable'].map((value) => ({
          value,
          label: t(value),
        })),
      ]}
      onChange={(value) =>
        props.onChange('decision', value === 'all' ? '' : value)
      }
    />
  )
  const categoryFilter = (
    <PromptAuditFilterSelect
      label={t('Category')}
      value={props.filters.category || 'all'}
      options={[
        { value: 'all', label: t('All categories') },
        ...props.categories.map((category) => ({
          value: category.id,
          label: t(category.label),
        })),
      ]}
      onChange={(value) =>
        props.onChange('category', value === 'all' ? '' : value)
      }
    />
  )
  const userFilter = (
    <LogsFilterField>
      <LogsFilterInput
        aria-label={t('Username')}
        placeholder={t('Username')}
        value={props.filters.username}
        onChange={(event) => props.onChange('username', event.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const statusFilter = (
    <PromptAuditFilterSelect
      label={t('Status')}
      value={props.filters.status || 'all'}
      options={[
        { value: 'all', label: t('All statuses') },
        ...['queued', 'processing', 'retry', 'done', 'failed'].map((value) => ({
          value,
          label: t(value),
        })),
      ]}
      onChange={(value) =>
        props.onChange('status', value === 'all' ? '' : value)
      }
    />
  )
  const directionFilter = (
    <PromptAuditFilterSelect
      label={t('Audit stage')}
      value={props.filters.direction || 'all'}
      options={[
        { value: 'all', label: t('All stages') },
        { value: 'input', label: t('Request input') },
        { value: 'output', label: t('Generated output') },
      ]}
      onChange={(value) =>
        props.onChange('direction', value === 'all' ? '' : value)
      }
    />
  )
  const advancedFilters = (
    <>
      {statusFilter}
      {directionFilter}
      {[
        ['model', t('Model')],
        ['group', t('Group')],
        ['protocol', t('Protocol')],
        ['request_id', t('Request ID')],
      ].map(([key, label]) => (
        <LogsFilterField key={key}>
          <LogsFilterInput
            aria-label={label}
            placeholder={label}
            value={props.filters[key as keyof PromptAuditFilters]}
            onChange={(event) =>
              props.onChange(
                key as keyof PromptAuditFilters,
                event.target.value
              )
            }
            onKeyDown={handleKeyDown}
          />
        </LogsFilterField>
      ))}
    </>
  )
  const advancedCount = [
    props.filters.status,
    props.filters.model,
    props.filters.group,
    props.filters.protocol,
    props.filters.request_id,
    props.filters.direction,
  ].filter(Boolean).length
  const primaryCount = [
    props.filters.decision,
    props.filters.category,
    props.filters.username,
  ].filter(Boolean).length
  const hasActiveFilters = Object.values(props.filters).some(Boolean)

  return (
    <LogsFilterToolbar
      table={props.table}
      compactMobile
      stats={
        <PromptAuditStatsBar
          stats={props.stats}
          loading={props.statsLoading}
          collapsed={props.collapsed}
          groupTotal={props.groupTotal}
          recordsTotal={props.recordsTotal}
        />
      }
      actionStart={
        <PromptAuditCollapseToggle
          collapsed={props.collapsed}
          onToggle={props.onToggleCollapse}
        />
      }
      primaryFilters={
        <>
          {dateFilter}
          {decisionFilter}
          {categoryFilter}
          {userFilter}
        </>
      }
      advancedFilters={advancedFilters}
      mobilePinnedFilters={dateFilter}
      mobileFilters={
        <>
          {decisionFilter}
          {categoryFilter}
          {userFilter}
          {advancedFilters}
        </>
      }
      mobileFilterCount={primaryCount + advancedCount}
      advancedFilterCount={advancedCount}
      hasAdvancedActiveFilters={advancedCount > 0}
      hasActiveFilters={hasActiveFilters}
      searchLoading={props.searchLoading}
      onSearch={props.onSearch}
      onReset={props.onReset}
    />
  )
}
