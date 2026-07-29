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
  ArrowUpRight01Icon,
  CloudDownloadIcon,
  RefreshIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import {
  getRunningSystemVersion,
  getSystemUpdateInfo,
  getSystemUpdateTriggerState,
  startSystemUpdate,
} from '@/features/system-settings/api'
import { formatTimestamp, formatTimestampToDate } from '@/lib/format'

import { SettingsSection } from '../components/settings-section'

type UpdateCheckerSectionProps = {
  currentVersion?: string | null
  startTime?: number | null
}

const updateReconnectTimeout = 5 * 60 * 1000

export function UpdateCheckerSection(props: UpdateCheckerSectionProps) {
  const { t } = useTranslation()
  const [updateDialogOpen, setUpdateDialogOpen] = useState(false)
  const [targetVersion, setTargetVersion] = useState<string | null>(null)
  const updateStartedAtRef = useRef(0)

  const updateQuery = useQuery({
    queryKey: ['system-update'],
    queryFn: getSystemUpdateInfo,
    enabled: false,
    retry: false,
  })
  const updateMutation = useMutation({ mutationFn: startSystemUpdate })
  const refetchUpdateInfo = updateQuery.refetch

  useEffect(() => {
    if (!targetVersion) return

    let cancelled = false
    let timeoutId = 0
    const pollVersion = async () => {
      try {
        const triggerState = await getSystemUpdateTriggerState()
        if (triggerState?.status === 'failed') {
          setTargetVersion(null)
          toast.error(t('Failed to trigger system update'))
          void refetchUpdateInfo()
          return
        }
      } catch {
        // The state endpoint is also briefly unavailable during the restart.
      }

      try {
        const runningVersion = await getRunningSystemVersion()
        if (runningVersion?.trim() === targetVersion.trim()) {
          window.location.reload()
          return
        }
      } catch {
        // A short connection failure is expected while the container restarts.
      }

      if (
        cancelled ||
        Date.now() - updateStartedAtRef.current >= updateReconnectTimeout
      ) {
        if (!cancelled) {
          setTargetVersion(null)
          toast.error(
            t(
              'The update has not completed yet. Check the updater logs before trying again.'
            )
          )
          void refetchUpdateInfo()
        }
        return
      }
      timeoutId = window.setTimeout(pollVersion, 2000)
    }

    timeoutId = window.setTimeout(pollVersion, 2000)
    return () => {
      cancelled = true
      window.clearTimeout(timeoutId)
    }
  }, [refetchUpdateInfo, targetVersion, t])

  const uptime = props.startTime
    ? formatTimestamp(props.startTime)
    : t('Unknown')
  const version = props.currentVersion || t('Unknown')
  const updateInfo = updateQuery.data
  const publishedAtTimestamp = updateInfo?.published_at
    ? new Date(updateInfo.published_at).getTime()
    : Number.NaN
  const publishedAt = Number.isFinite(publishedAtTimestamp)
    ? formatTimestampToDate(publishedAtTimestamp, 'milliseconds')
    : t('Unknown')
  const updating = updateMutation.isPending || targetVersion !== null

  const handleCheckUpdates = async () => {
    const result = await refetchUpdateInfo()
    if (result.error || !result.data) {
      toast.error(t('Failed to check GHCR image updates'))
      return
    }
    if (!result.data.update_available) {
      toast.success(
        t('You are running the latest version ({{version}}).', {
          version: result.data.latest_version,
        })
      )
    }
  }

  const handleStartUpdate = async () => {
    setUpdateDialogOpen(false)
    try {
      const result = await updateMutation.mutateAsync()
      if (!result.started) {
        toast.success(
          t('You are running the latest version ({{version}}).', {
            version: result.update.latest_version,
          })
        )
        await refetchUpdateInfo()
        return
      }

      updateStartedAtRef.current = Date.now()
      setTargetVersion(result.update.latest_version)
      toast.success(
        t(
          'Update started. This page will reconnect after the service restarts.'
        )
      )
    } catch (error) {
      const message =
        error instanceof Error
          ? t(error.message)
          : t('Failed to trigger system update')
      toast.error(message)
      await refetchUpdateInfo()
    }
  }

  return (
    <SettingsSection title={t('System maintenance')}>
      <div className='flex flex-col gap-6'>
        <div className='grid gap-4 md:grid-cols-2'>
          <div className='rounded-lg border p-4'>
            <div className='text-muted-foreground text-sm'>
              {t('Current version')}
            </div>
            <div className='text-lg font-semibold'>{version}</div>
          </div>
          <div className='rounded-lg border p-4'>
            <div className='text-muted-foreground text-sm'>
              {t('Uptime since')}
            </div>
            <div className='text-lg font-semibold'>{uptime}</div>
          </div>
        </div>

        <div className='flex flex-col gap-4 rounded-lg border p-4'>
          <div className='flex flex-wrap items-start justify-between gap-3'>
            <div className='flex flex-col gap-1'>
              <h4 className='font-medium'>{t('GHCR image updates')}</h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Checks the latest successful GHCR build and installs it on this instance.'
                )}
              </p>
            </div>
            {updateInfo && (
              <Badge
                variant={updateInfo.update_available ? 'warning' : 'secondary'}
              >
                {updateInfo.update_available
                  ? t('Update available')
                  : t('Up to date')}
              </Badge>
            )}
          </div>

          {updateInfo && (
            <div className='grid gap-4 md:grid-cols-2'>
              <div className='bg-muted/40 rounded-lg p-3'>
                <div className='text-muted-foreground text-xs'>
                  {t('Latest image version')}
                </div>
                <div className='font-mono text-sm font-medium'>
                  {updateInfo.latest_version}
                </div>
              </div>
              <div className='bg-muted/40 rounded-lg p-3'>
                <div className='text-muted-foreground text-xs'>
                  {t('Published')}
                </div>
                <div className='text-sm font-medium'>{publishedAt}</div>
              </div>
              {updateInfo.title && (
                <div className='text-muted-foreground text-sm md:col-span-2'>
                  {updateInfo.title}
                </div>
              )}
            </div>
          )}

          {updateInfo?.update_available && !updateInfo.update_enabled && (
            <Alert>
              <AlertTitle>
                {t('One-click update is not configured on this deployment.')}
              </AlertTitle>
              <AlertDescription>
                {t(
                  'Configure the update trigger before using one-click updates.'
                )}
              </AlertDescription>
            </Alert>
          )}

          {targetVersion && (
            <Alert>
              <Spinner />
              <AlertTitle>{t('Updating...')}</AlertTitle>
              <AlertDescription>
                {t('Waiting for the updated service to start...')}
              </AlertDescription>
            </Alert>
          )}

          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              onClick={handleCheckUpdates}
              disabled={updateQuery.isFetching || updating}
            >
              {updateQuery.isFetching ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon
                  icon={RefreshIcon}
                  strokeWidth={2}
                  data-icon='inline-start'
                  aria-hidden='true'
                />
              )}
              {updateQuery.isFetching
                ? t('Checking updates...')
                : t('Check for updates')}
            </Button>

            {updateInfo?.workflow_url && (
              <Button
                variant='outline'
                nativeButton={false}
                render={
                  <a
                    href={updateInfo.workflow_url}
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                <HugeiconsIcon
                  icon={ArrowUpRight01Icon}
                  strokeWidth={2}
                  data-icon='inline-start'
                  aria-hidden='true'
                />
                {t('View build')}
              </Button>
            )}

            {updateInfo?.update_available && updateInfo.update_enabled && (
              <AlertDialog
                open={updateDialogOpen}
                onOpenChange={setUpdateDialogOpen}
              >
                <AlertDialogTrigger
                  render={<Button type='button' disabled={updating} />}
                >
                  <HugeiconsIcon
                    icon={CloudDownloadIcon}
                    strokeWidth={2}
                    data-icon='inline-start'
                    aria-hidden='true'
                  />
                  {t('Update now')}
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>
                      {t('Install {{version}}?', {
                        version: updateInfo.latest_version,
                      })}
                    </AlertDialogTitle>
                    <AlertDialogDescription>
                      {t(
                        'The application container will restart briefly. Database and Redis services will not be restarted.'
                      )}
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel disabled={updateMutation.isPending}>
                      {t('Cancel')}
                    </AlertDialogCancel>
                    <AlertDialogAction
                      onClick={handleStartUpdate}
                      disabled={updateMutation.isPending}
                    >
                      {updateMutation.isPending && (
                        <Spinner data-icon='inline-start' />
                      )}
                      {t('Confirm update')}
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            )}
          </div>
        </div>
      </div>
    </SettingsSection>
  )
}
