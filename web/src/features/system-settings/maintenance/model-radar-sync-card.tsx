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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useRadarFormatters } from '@/features/model-radar/hooks/use-radar-formatters'
import { handleServerError } from '@/lib/handle-server-error'

import { getSystemTask, triggerModelRadarSync } from '../api'
import type { ModelRadarManagement, SystemTask } from '../types'

export function ModelRadarSyncCard(props: {
  management: ModelRadarManagement
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const queryClient = useQueryClient()
  const [triggeredTaskId, setTriggeredTaskId] = useState<string | null>(null)
  const taskId = props.management.current_task?.task_id ?? triggeredTaskId
  const reportedTask = useRef<string | null>(null)
  const taskQuery = useQuery({
    queryKey: ['model-radar-sync-task', taskId],
    queryFn: async () => {
      if (!taskId) throw new Error('Missing model radar task ID')
      const response = await getSystemTask<SystemTask>(taskId)
      if (!response.success || !response.data) throw new Error(response.message)
      return response.data
    },
    enabled: Boolean(taskId),
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'succeeded' || status === 'failed' ? false : 2000
    },
  })
  useEffect(() => {
    const task = taskQuery.data
    if (
      !task ||
      (task.status !== 'succeeded' && task.status !== 'failed') ||
      reportedTask.current === task.task_id
    ) {
      return
    }
    reportedTask.current = task.task_id
    if (task.status === 'succeeded') toast.success(t('Sync finished'))
    else {
      toast.error(
        t('Sync failed: {{error}}', {
          error: task.error || t('An unknown error occurred'),
        })
      )
    }
    void queryClient.invalidateQueries({ queryKey: ['model-radar-management'] })
    void queryClient.invalidateQueries({ queryKey: ['model-radar'] })
  }, [taskQuery.data, queryClient, t])

  const sync = useMutation({
    mutationFn: triggerModelRadarSync,
    onSuccess: (result) => {
      setTriggeredTaskId(result.task.task_id)
      if (result.created) toast.success(t('Sync started'))
      else toast.info(t('A sync is already running.'))
      void queryClient.invalidateQueries({
        queryKey: ['model-radar-management'],
      })
    },
    onError: handleServerError,
  })
  const status = taskQuery.data?.status ?? props.management.current_task?.status
  const busy =
    sync.isPending ||
    (Boolean(taskId) && status !== 'succeeded' && status !== 'failed')
  const snapshot = props.management.snapshot
  return (
    <Card>
      <CardHeader className='flex flex-row flex-wrap items-center justify-between gap-3'>
        <CardTitle>{t('Sync status')}</CardTitle>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() => sync.mutate()}
          disabled={busy}
        >
          {busy ? t('Syncing...') : t('Sync now')}
        </Button>
      </CardHeader>
      <CardContent className='space-y-3'>
        {snapshot ? (
          <>
            <div className='flex flex-wrap items-center gap-2'>
              <Badge variant={snapshot.stale ? 'destructive' : 'secondary'}>
                {snapshot.stale ? t('Stale data') : t('Up to date')}
              </Badge>
              <span className='text-muted-foreground text-xs'>
                {t('{{models}} models, {{configurations}} configurations', {
                  models: snapshot.model_count,
                  configurations: snapshot.configuration_count,
                })}
              </span>
              <span className='text-muted-foreground text-xs'>
                {t('Degradation alerts')}: {snapshot.alert_count}
              </span>
            </div>
            <dl className='grid gap-3 text-sm sm:grid-cols-2'>
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('Last sync')}
                </dt>
                <dd>{format.dateTime(snapshot.fetched_at)}</dd>
              </div>
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('Source updated')}
                </dt>
                <dd>{format.dateTime(snapshot.source_updated_at)}</dd>
              </div>
            </dl>
          </>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('No snapshot yet')}
          </p>
        )}
        <p className='text-muted-foreground text-xs'>
          {t('Sync interval')}:{' '}
          {t('Every {{minutes}} minutes', {
            minutes: props.management.sync.interval_minutes,
          })}
        </p>
        {!props.management.sync.enabled ? (
          <p className='text-muted-foreground text-xs'>
            {t('Scheduled sync is disabled by MODEL_RADAR_SYNC_ENABLED.')}
          </p>
        ) : null}
        {taskQuery.isError ? (
          <div
            role='alert'
            className='flex flex-wrap items-center gap-2 text-sm'
          >
            {t('Unable to load sync status.')}
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => void taskQuery.refetch()}
            >
              {t('Retry')}
            </Button>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
