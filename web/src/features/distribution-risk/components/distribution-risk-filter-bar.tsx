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
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'

import { CompactDateTimeRangePicker } from '../../usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '../../usage-logs/components/logs-filter-toolbar'
import type { DistributionRiskEvent } from '../types'

export type DistributionRiskEventDraftFilters = {
  status: 'all' | 'pending' | 'confirmed' | 'false_positive'
  level: 'all' | 'medium' | 'high'
  scope_kind: 'all' | 'user' | 'token'
  user_id: string
  start_timestamp?: number
  end_timestamp?: number
}

function RiskFilterSelect(props: {
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
        onValueChange={(value) => {
          if (value !== null) props.onChange(value)
        }}
        aria-label={props.label}
        className='w-full'
      />
    </LogsFilterField>
  )
}

export function DistributionRiskFilterBar(props: {
  table: Table<DistributionRiskEvent>
  filters: DistributionRiskEventDraftFilters
  onChange: (patch: Partial<DistributionRiskEventDraftFilters>) => void
  onSearch: () => void
  onReset: () => void
  isFetching: boolean
  actionEnd?: React.ReactNode
}) {
  const { t } = useTranslation()
  const dateFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={
          props.filters.start_timestamp === undefined
            ? undefined
            : new Date(props.filters.start_timestamp * 1000)
        }
        end={
          props.filters.end_timestamp === undefined
            ? undefined
            : new Date(props.filters.end_timestamp * 1000)
        }
        onChange={({ start, end }) =>
          props.onChange({
            start_timestamp: start
              ? Math.floor(start.getTime() / 1000)
              : undefined,
            end_timestamp: end ? Math.floor(end.getTime() / 1000) : undefined,
          })
        }
      />
    </LogsFilterField>
  )
  const mainFilters = (
    <>
      <RiskFilterSelect
        label={t('Status')}
        value={props.filters.status}
        options={[
          { value: 'all', label: t('All statuses') },
          { value: 'pending', label: t('Pending') },
          { value: 'confirmed', label: t('Confirmed') },
          { value: 'false_positive', label: t('False positive') },
        ]}
        onChange={(value) =>
          props.onChange({
            status: value as DistributionRiskEventDraftFilters['status'],
          })
        }
      />
      <RiskFilterSelect
        label={t('Risk level')}
        value={props.filters.level}
        options={[
          { value: 'all', label: t('All risk levels') },
          { value: 'high', label: t('High') },
          { value: 'medium', label: t('Medium') },
        ]}
        onChange={(value) =>
          props.onChange({
            level: value as DistributionRiskEventDraftFilters['level'],
          })
        }
      />
      <RiskFilterSelect
        label={t('Scope')}
        value={props.filters.scope_kind}
        options={[
          { value: 'all', label: t('All scopes') },
          { value: 'user', label: t('User account') },
          { value: 'token', label: t('Token') },
        ]}
        onChange={(value) =>
          props.onChange({
            scope_kind:
              value as DistributionRiskEventDraftFilters['scope_kind'],
          })
        }
      />
    </>
  )
  const advancedFilters = (
    <LogsFilterField>
      <LogsFilterInput
        type='number'
        min={1}
        step={1}
        aria-label={t('Search by user ID')}
        placeholder={t('Search by user ID')}
        value={props.filters.user_id}
        onChange={(event) => props.onChange({ user_id: event.target.value })}
      />
    </LogsFilterField>
  )
  const hasDateFilter =
    props.filters.start_timestamp !== undefined ||
    props.filters.end_timestamp !== undefined
  const hasActiveFilters =
    props.filters.status !== 'all' ||
    props.filters.level !== 'all' ||
    props.filters.scope_kind !== 'all' ||
    props.filters.user_id !== '' ||
    hasDateFilter
  const filterCount =
    Number(props.filters.status !== 'all') +
    Number(props.filters.level !== 'all') +
    Number(props.filters.scope_kind !== 'all') +
    Number(props.filters.user_id !== '') +
    Number(hasDateFilter)

  return (
    <LogsFilterToolbar
      table={props.table}
      primaryFilters={
        <>
          {dateFilter}
          {mainFilters}
        </>
      }
      advancedFilters={advancedFilters}
      mobilePinnedFilters={dateFilter}
      mobileFilters={
        <>
          {mainFilters}
          {advancedFilters}
        </>
      }
      mobileFilterCount={filterCount}
      advancedFilterCount={Number(props.filters.user_id !== '')}
      hasActiveFilters={hasActiveFilters}
      hasAdvancedActiveFilters={props.filters.user_id !== ''}
      searchLoading={props.isFetching}
      onSearch={props.onSearch}
      onReset={props.onReset}
      actionEnd={props.actionEnd}
    />
  )
}
