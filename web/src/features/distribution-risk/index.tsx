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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, Eye } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { formatTimestampToDate } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  getDistributionRiskConfig,
  listDistributionRiskEvents,
  updateDistributionRiskConfig,
} from './api'
import { DistributionRiskEventDialog } from './components/distribution-risk-event-dialog'
import { DistributionRiskSettings } from './components/distribution-risk-settings'

const PAGE_SIZE = 20

export function DistributionRiskPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.DISTRIBUTION_RISK,
    ADMIN_PERMISSION_ACTIONS.READ
  )
  const canManage = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.DISTRIBUTION_RISK,
    ADMIN_PERMISSION_ACTIONS.MANAGE
  )
  const [status, setStatus] = useState<'pending' | 'all'>('pending')
  const [page, setPage] = useState(1)
  const [eventID, setEventID] = useState<number | null>(null)

  const configQuery = useQuery({
    queryKey: ['distribution-risk', 'config'],
    queryFn: getDistributionRiskConfig,
    enabled: canManage,
    refetchInterval: 60_000,
  })
  const eventsQuery = useQuery({
    queryKey: ['distribution-risk', 'events', status, page],
    queryFn: () => listDistributionRiskEvents(status, page, PAGE_SIZE),
    enabled: canRead,
    refetchInterval: 60_000,
  })
  const updateMutation = useMutation({
    mutationFn: updateDistributionRiskConfig,
    onSuccess: async (config) => {
      queryClient.setQueryData(['distribution-risk', 'config'], config)
      await queryClient.invalidateQueries({
        queryKey: ['distribution-risk', 'events'],
      })
      toast.success(t('Distribution risk settings saved'))
    },
    onError: (error) =>
      toast.error(error.message || t('Failed to save settings')),
  })

  const pendingCount = eventsQuery.data?.pending_count ?? 0
  const totalPages = Math.max(
    1,
    Math.ceil((eventsQuery.data?.total ?? 0) / PAGE_SIZE)
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Distribution risk')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {pendingCount > 0 && (
          <Badge variant='warning'>
            {t('Pending: {{count}}', { count: pendingCount })}
          </Badge>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-5'>
          {pendingCount > 0 && canRead && (
            <div
              className='border-warning/40 bg-warning/10 flex flex-wrap items-center justify-between gap-3 rounded-md border px-3 py-2.5'
              role='status'
            >
              <p className='text-sm'>
                {t(
                  '{{count}} distribution risk events are waiting for review.',
                  {
                    count: pendingCount,
                  }
                )}
              </p>
              <Button
                size='sm'
                variant='outline'
                onClick={() => {
                  setStatus('pending')
                  setPage(1)
                }}
              >
                {t('Review pending')}
              </Button>
            </div>
          )}

          {canManage && configQuery.data && (
            <DistributionRiskSettings
              key={[
                configQuery.data.enabled,
                configQuery.data.started_at,
                configQuery.data.ip_floor,
                configQuery.data.user_agent_floor,
                configQuery.data.session_floor,
                configQuery.data.requests_per_minute_floor,
                configQuery.data.concurrency_floor,
                configQuery.data.min_requests,
                configQuery.data.min_active_days,
              ].join(':')}
              config={configQuery.data}
              isSaving={updateMutation.isPending}
              onSave={(update) => updateMutation.mutate(update)}
            />
          )}
          {canManage && configQuery.isError && (
            <p className='text-destructive text-sm'>
              {configQuery.error.message ||
                t('Failed to load detection settings')}
            </p>
          )}

          {canRead ? (
            <section className='space-y-3'>
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <h3 className='text-sm font-semibold'>{t('Risk events')}</h3>
                <Tabs
                  value={status}
                  onValueChange={(value) => {
                    if (value === 'pending' || value === 'all') {
                      setStatus(value)
                      setPage(1)
                    }
                  }}
                >
                  <TabsList>
                    <TabsTrigger value='pending'>{t('Pending')}</TabsTrigger>
                    <TabsTrigger value='all'>{t('All events')}</TabsTrigger>
                  </TabsList>
                </Tabs>
              </div>
              {eventsQuery.isError ? (
                <p className='text-destructive py-6 text-sm'>
                  {eventsQuery.error.message || t('Failed to load risk events')}
                </p>
              ) : (
                <div className='overflow-x-auto rounded-md border'>
                  <Table className='min-w-[900px]' withContainer={false}>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Observed at')}</TableHead>
                        <TableHead>{t('User ID')}</TableHead>
                        <TableHead>{t('Scope')}</TableHead>
                        <TableHead>{t('Tokens')}</TableHead>
                        <TableHead>{t('Score')}</TableHead>
                        <TableHead>{t('Risk')}</TableHead>
                        <TableHead>{t('Status')}</TableHead>
                        <TableHead className='w-28'>{t('Details')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {eventsQuery.isLoading && (
                        <TableRow>
                          <TableCell
                            colSpan={8}
                            className='text-muted-foreground py-8 text-center'
                          >
                            {t('Loading...')}
                          </TableCell>
                        </TableRow>
                      )}
                      {!eventsQuery.isLoading &&
                        eventsQuery.data?.items.map((event) => (
                          <TableRow key={event.id}>
                            <TableCell>
                              {formatTimestampToDate(event.created_at)}
                            </TableCell>
                            <TableCell className='font-mono'>
                              {event.user_id}
                            </TableCell>
                            <TableCell>
                              {event.scope_kind === 'user'
                                ? t('User account')
                                : t('Token')}{' '}
                              {event.scope_id}
                            </TableCell>
                            <TableCell className='max-w-56 truncate font-mono'>
                              {event.token_ids.join(', ')}
                            </TableCell>
                            <TableCell className='font-semibold'>
                              {event.score}
                            </TableCell>
                            <TableCell>
                              <Badge
                                variant={
                                  event.level === 'high'
                                    ? 'destructive'
                                    : 'warning'
                                }
                              >
                                {event.level === 'high'
                                  ? t('High')
                                  : t('Medium')}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              {statusLabel(event.status, t)}
                            </TableCell>
                            <TableCell>
                              <Button
                                size='sm'
                                variant='outline'
                                onClick={() => setEventID(event.id)}
                              >
                                <Eye />
                                {t('Details')}
                              </Button>
                            </TableCell>
                          </TableRow>
                        ))}
                      {!eventsQuery.isLoading &&
                        !eventsQuery.data?.items.length && (
                          <TableRow>
                            <TableCell
                              colSpan={8}
                              className='text-muted-foreground py-8 text-center'
                            >
                              {t('No risk events')}
                            </TableCell>
                          </TableRow>
                        )}
                    </TableBody>
                  </Table>
                </div>
              )}
              <div className='flex items-center justify-end gap-2'>
                <span className='text-muted-foreground mr-2 text-xs'>
                  {t('Page {{page}} of {{total}}', { page, total: totalPages })}
                </span>
                <Button
                  aria-label={t('Previous page')}
                  disabled={page <= 1 || eventsQuery.isFetching}
                  onClick={() => setPage((current) => Math.max(1, current - 1))}
                  size='icon-sm'
                  variant='outline'
                >
                  <ChevronLeft />
                </Button>
                <Button
                  aria-label={t('Next page')}
                  disabled={page >= totalPages || eventsQuery.isFetching}
                  onClick={() =>
                    setPage((current) => Math.min(totalPages, current + 1))
                  }
                  size='icon-sm'
                  variant='outline'
                >
                  <ChevronRight />
                </Button>
              </div>
            </section>
          ) : null}
        </div>
        <DistributionRiskEventDialog
          key={eventID ?? 'closed'}
          eventID={eventID}
          canManage={canManage}
          onOpenChange={(open) => {
            if (!open) setEventID(null)
          }}
          onReviewed={() => {
            setPage(1)
            void queryClient.invalidateQueries({
              queryKey: ['distribution-risk', 'events'],
            })
          }}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function statusLabel(
  status: 'pending' | 'confirmed' | 'false_positive',
  t: (key: string) => string
) {
  if (status === 'false_positive') return t('False positive')
  if (status === 'confirmed') return t('Confirmed')
  return t('Pending')
}
