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
import { Plus, RefreshCw, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { getGroups } from '@/features/users/api'

import type { GroupRateLimitPolicy, GroupRateLimitValues } from '../types'
import { RateLimitDialog, type RateLimitEntryData } from './rate-limit-dialog'

type RequestCountLimits = Record<string, [number, number]>
type GroupPolicies = Record<string, GroupRateLimitPolicy>

type RateLimitVisualEditorProps = {
  requestCounts: string
  policies: string
  onRequestCountsChange: (value: string) => void
  onPoliciesChange: (value: string) => void
}

function parseJsonObject<T extends object>(value: string): T {
  if (!value.trim()) return {} as T
  try {
    const parsed = JSON.parse(value)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as T)
      : ({} as T)
  } catch {
    return {} as T
  }
}

function configuredLimit(value: number | null | undefined) {
  return typeof value === 'number' ? value : undefined
}

function hasConfiguredLimits(limits: GroupRateLimitValues) {
  return (
    limits.rpm_limit !== undefined ||
    limits.concurrency_limit !== undefined ||
    limits.stream_tps_limit !== undefined ||
    limits.first_token_delay_ms !== undefined
  )
}

function limitsFromEntry(
  rpm: number | undefined,
  concurrency: number | undefined,
  streamTps: number | undefined,
  firstTokenDelay?: number
): GroupRateLimitValues {
  const limits: GroupRateLimitValues = {}
  if (rpm !== undefined) limits.rpm_limit = rpm
  if (concurrency !== undefined) limits.concurrency_limit = concurrency
  if (streamTps !== undefined) limits.stream_tps_limit = streamTps
  if (firstTokenDelay !== undefined) {
    limits.first_token_delay_ms = firstTokenDelay
  }
  return limits
}

export function RateLimitVisualEditor({
  requestCounts,
  policies,
  onRequestCountsChange,
  onPoliciesChange,
}: RateLimitVisualEditorProps) {
  const { t } = useTranslation()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<RateLimitEntryData | null>(null)
  const groupQuery = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    staleTime: 5 * 60 * 1000,
  })
  const groupListFailed =
    groupQuery.isError || groupQuery.data?.success === false
  const currentGroups = useMemo(() => {
    if (groupListFailed) return []
    return [...(groupQuery.data?.data ?? [])].sort((left, right) => {
      if (left === 'default') return -1
      if (right === 'default') return 1
      return left.localeCompare(right)
    })
  }, [groupListFailed, groupQuery.data?.data])
  const currentGroupSet = useMemo(() => new Set(currentGroups), [currentGroups])

  const entries = useMemo(() => {
    const parsedCounts = parseJsonObject<RequestCountLimits>(requestCounts)
    const parsedPolicies = parseJsonObject<GroupPolicies>(policies)
    const groups = new Set([
      ...Object.keys(parsedCounts),
      ...Object.keys(parsedPolicies),
    ])

    return [...groups]
      .sort((left, right) => {
        if (left === 'default') return -1
        if (right === 'default') return 1
        return left.localeCompare(right)
      })
      .map((groupName): RateLimitEntryData => {
        const requestCount = parsedCounts[groupName]
        const policy = parsedPolicies[groupName] ?? {}
        const member = policy.member_limits ?? {}
        const shared = policy.shared_pool ?? {}
        return {
          groupName,
          requestCountEnabled: Array.isArray(requestCount),
          maxRequests: requestCount?.[0] ?? 0,
          maxSuccess: requestCount?.[1] ?? 1,
          memberRpmLimit: configuredLimit(member.rpm_limit),
          memberConcurrencyLimit: configuredLimit(member.concurrency_limit),
          memberStreamTpsLimit: configuredLimit(member.stream_tps_limit),
          memberFirstTokenDelayMs: configuredLimit(member.first_token_delay_ms),
          sharedRpmLimit: configuredLimit(shared.rpm_limit),
          sharedConcurrencyLimit: configuredLimit(shared.concurrency_limit),
          sharedStreamTpsLimit: configuredLimit(shared.stream_tps_limit),
        }
      })
  }, [policies, requestCounts])

  const availableGroupOptions = useMemo(() => {
    const configured = new Set(entries.map((entry) => entry.groupName))
    return currentGroups
      .filter((group) => !configured.has(group))
      .map((group) => ({ value: group, label: group }))
  }, [currentGroups, entries])

  const filteredEntries = useMemo(() => {
    const search = searchText.trim().toLowerCase()
    if (!search) return entries
    return entries.filter((entry) =>
      entry.groupName.toLowerCase().includes(search)
    )
  }, [entries, searchText])

  const handleSave = (data: RateLimitEntryData) => {
    const parsedCounts = parseJsonObject<RequestCountLimits>(requestCounts)
    const parsedPolicies = parseJsonObject<GroupPolicies>(policies)
    const previousName = editData?.groupName
    if (previousName && previousName !== data.groupName) {
      delete parsedCounts[previousName]
      delete parsedPolicies[previousName]
    }

    if (data.requestCountEnabled) {
      parsedCounts[data.groupName] = [data.maxRequests, data.maxSuccess]
    } else {
      delete parsedCounts[data.groupName]
    }

    const memberLimits = limitsFromEntry(
      data.memberRpmLimit,
      data.memberConcurrencyLimit,
      data.memberStreamTpsLimit,
      data.memberFirstTokenDelayMs
    )
    const sharedPool = limitsFromEntry(
      data.sharedRpmLimit,
      data.sharedConcurrencyLimit,
      data.sharedStreamTpsLimit
    )
    if (hasConfiguredLimits(memberLimits) || hasConfiguredLimits(sharedPool)) {
      parsedPolicies[data.groupName] = {
        member_limits: memberLimits,
        shared_pool: sharedPool,
      }
    } else {
      delete parsedPolicies[data.groupName]
    }

    onRequestCountsChange(JSON.stringify(parsedCounts, null, 2))
    onPoliciesChange(JSON.stringify(parsedPolicies, null, 2))
  }

  const handleDelete = (groupName: string) => {
    const parsedCounts = parseJsonObject<RequestCountLimits>(requestCounts)
    const parsedPolicies = parseJsonObject<GroupPolicies>(policies)
    delete parsedCounts[groupName]
    delete parsedPolicies[groupName]
    onRequestCountsChange(JSON.stringify(parsedCounts, null, 2))
    onPoliciesChange(JSON.stringify(parsedPolicies, null, 2))
  }

  const formatLimits = (entry: RateLimitEntryData, shared: boolean) => {
    const values = shared
      ? [
          entry.sharedRpmLimit &&
            `RPM ${entry.sharedRpmLimit.toLocaleString()}`,
          entry.sharedConcurrencyLimit &&
            `${t('Concurrency')} ${entry.sharedConcurrencyLimit.toLocaleString()}`,
          entry.sharedStreamTpsLimit &&
            `TPS ${entry.sharedStreamTpsLimit.toLocaleString()}`,
        ]
      : [
          entry.memberRpmLimit &&
            `RPM ${entry.memberRpmLimit.toLocaleString()}`,
          entry.memberConcurrencyLimit &&
            `${t('Concurrency')} ${entry.memberConcurrencyLimit.toLocaleString()}`,
          entry.memberStreamTpsLimit &&
            `TPS ${entry.memberStreamTpsLimit.toLocaleString()}`,
          entry.memberFirstTokenDelayMs &&
            `${t('First visible text delay')} ${entry.memberFirstTokenDelayMs.toLocaleString()} ms`,
        ]
    const configured = values.filter(Boolean)
    return configured.length > 0 ? configured.join(' · ') : t('Unlimited')
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center'>
        <div className='relative flex-1'>
          <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' />
          <Input
            placeholder={t('Search group names...')}
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
            className='pl-9'
          />
        </div>
        <Button
          type='button'
          onClick={() => {
            setEditData(null)
            setDialogOpen(true)
          }}
          disabled={
            groupQuery.isLoading ||
            groupListFailed ||
            availableGroupOptions.length === 0
          }
        >
          <Plus className='mr-2 h-4 w-4' />
          {t('Add group')}
        </Button>
      </div>

      {groupListFailed && (
        <div className='border-destructive/40 bg-destructive/5 flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Failed to load existing groups. Existing policies remain editable, but new policies cannot be added.'
            )}
          </p>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => void groupQuery.refetch()}
            disabled={groupQuery.isFetching}
          >
            <RefreshCw
              className={groupQuery.isFetching ? 'animate-spin' : undefined}
            />
            {t('Retry')}
          </Button>
        </div>
      )}

      <StaticDataTable
        data={filteredEntries}
        getRowKey={(entry) => entry.groupName}
        emptyContent={
          searchText
            ? t('No groups match your search')
            : t('No group rate limits configured. Add a group to get started.')
        }
        columns={[
          {
            id: 'group',
            header: t('Group Name'),
            cellClassName: 'font-medium',
            cell: (entry) => (
              <div className='flex flex-wrap items-center gap-2'>
                <span>{entry.groupName}</span>
                {!groupListFailed &&
                  !groupQuery.isLoading &&
                  !currentGroupSet.has(entry.groupName) && (
                    <Badge variant='outline'>
                      {t('Group no longer exists')}
                    </Badge>
                  )}
              </div>
            ),
          },
          {
            id: 'request-counts',
            header: t('Request-count override'),
            cell: (entry) =>
              entry.requestCountEnabled
                ? `${entry.maxRequests === 0 ? t('Unlimited') : entry.maxRequests.toLocaleString()} / ${entry.maxSuccess.toLocaleString()} ${t('successful')}`
                : t('Use global values'),
          },
          {
            id: 'member-limits',
            header: t('Group member limits'),
            cellClassName: 'text-muted-foreground text-xs',
            cell: (entry) => formatLimits(entry, false),
          },
          {
            id: 'shared-pool',
            header: t('Group shared pool'),
            cellClassName: 'text-muted-foreground text-xs',
            cell: (entry) => formatLimits(entry, true),
          },
          {
            id: 'actions',
            header: t('Actions'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (entry) => (
              <StaticRowActions
                editLabel={t('Edit')}
                deleteLabel={t('Delete')}
                menuLabel={t('Open menu')}
                onEdit={() => {
                  setEditData(entry)
                  setDialogOpen(true)
                }}
                onDelete={() => handleDelete(entry.groupName)}
              />
            ),
          },
        ]}
      />

      <RateLimitDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
        groupOptions={availableGroupOptions}
        groupListLoading={groupQuery.isLoading}
        groupListFailed={groupListFailed}
        editGroupMissing={
          Boolean(editData) &&
          !groupListFailed &&
          !groupQuery.isLoading &&
          !currentGroupSet.has(editData?.groupName ?? '')
        }
      />
    </div>
  )
}
