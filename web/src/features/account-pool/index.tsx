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
import { Link } from '@tanstack/react-router'
import { AlertTriangle, Database, Settings2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  getAccountPool,
  getAccountPoolApiError,
  refreshAccountPool,
} from './api'
import { AccountPoolTable } from './components/account-pool-table'
import { formatAccountPoolCountdown } from './lib/quota'
import {
  getAccountPoolRefetchInterval,
  shouldRefreshAccountPoolOnVisibility,
} from './lib/refresh'

const ACCOUNT_POOL_QUERY_KEY = ['account-pool'] as const

function AccountPoolUnavailable(props: { notConfigured: boolean }) {
  const { t } = useTranslation()
  const isRoot = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  return (
    <div className='bg-card flex min-h-72 items-center justify-center rounded-xl border p-6'>
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            {props.notConfigured ? (
              <Settings2 className='size-6' />
            ) : (
              <Database className='size-6' />
            )}
          </EmptyMedia>
          <EmptyTitle>
            {props.notConfigured
              ? t('Account pool is not configured')
              : t('Account pool is temporarily unavailable')}
          </EmptyTitle>
          <EmptyDescription>
            {props.notConfigured
              ? t(
                  'The CLIProxyAPI management connection must be configured by the deployment administrator.'
                )
              : t(
                  'No quota snapshot is available yet. Try again after the upstream service recovers.'
                )}
          </EmptyDescription>
        </EmptyHeader>
        {props.notConfigured && isRoot ? (
          <EmptyContent>
            <Button
              variant='outline'
              render={
                <Link
                  to='/system-settings/operations/$section'
                  params={{ section: 'account-pool' }}
                />
              }
            >
              {t('Open account pool settings')}
            </Button>
          </EmptyContent>
        ) : null}
      </Empty>
    </div>
  )
}

export function AccountPool() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [now, setNow] = useState(() => Date.now())
  const accountPoolQuery = useQuery({
    queryKey: ACCOUNT_POOL_QUERY_KEY,
    queryFn: getAccountPool,
    retry: false,
    refetchInterval: (query) => getAccountPoolRefetchInterval(query.state.data),
    refetchIntervalInBackground: false,
  })
  const snapshot = accountPoolQuery.data?.data
  const refetchAccountPool = accountPoolQuery.refetch

  useEffect(() => {
    let interval: number | undefined
    const updateClock = () => {
      if (document.visibilityState !== 'visible') {
        if (interval !== undefined) window.clearInterval(interval)
        interval = undefined
        return
      }
      setNow(Date.now())
      if (interval === undefined) {
        interval = window.setInterval(() => setNow(Date.now()), 1000)
      }
    }
    updateClock()
    document.addEventListener('visibilitychange', updateClock)
    return () => {
      document.removeEventListener('visibilitychange', updateClock)
      if (interval !== undefined) window.clearInterval(interval)
    }
  }, [])

  useEffect(() => {
    const handleVisibility = () => {
      if (document.visibilityState !== 'visible') return
      const nextRefreshAt = snapshot?.next_refresh_at
      if (!nextRefreshAt) return
      if (shouldRefreshAccountPoolOnVisibility(nextRefreshAt)) {
        void refetchAccountPool()
      }
    }
    document.addEventListener('visibilitychange', handleVisibility)
    return () =>
      document.removeEventListener('visibilitychange', handleVisibility)
  }, [refetchAccountPool, snapshot?.next_refresh_at])

  const refreshMutation = useMutation({
    mutationFn: refreshAccountPool,
    onSuccess: (response) => {
      queryClient.setQueryData(ACCOUNT_POOL_QUERY_KEY, response)
      toast.success(t('Account pool refreshed'))
    },
    onError: (error) => {
      const apiError = getAccountPoolApiError(error)
      if (apiError.code === 'account_pool_refresh_cooldown') {
        toast.error(t('Manual refresh is cooling down'))
        void refetchAccountPool()
        return
      }
      toast.error(t('Failed to refresh account pool'))
    },
  })

  const manualAvailableAt = snapshot?.manual_refresh_available_at
    ? Date.parse(snapshot.manual_refresh_available_at)
    : 0
  const manualCooldown = Math.max(0, manualAvailableAt - now)
  const refreshDisabled = manualCooldown > 0
  const refreshCountdown = formatAccountPoolCountdown(
    snapshot?.manual_refresh_available_at ?? null,
    now
  )
  const refreshLabel = useMemo(() => {
    if (refreshDisabled && refreshCountdown && refreshCountdown !== '0s') {
      return t('Refresh in {{time}}', { time: refreshCountdown })
    }
    return t('Refresh all')
  }, [refreshCountdown, refreshDisabled, t])

  const apiError = getAccountPoolApiError(accountPoolQuery.error)
  const hasFatalError = accountPoolQuery.isError && !snapshot
  let statusAlert = null
  if (snapshot?.partial) {
    statusAlert = (
      <Alert className='border-warning/40 bg-warning/5 text-warning'>
        <AlertTriangle aria-hidden='true' />
        <AlertTitle>{t('Some accounts could not be refreshed')}</AlertTitle>
        <AlertDescription>
          {t('Successful accounts are current; failed rows are marked stale.')}
        </AlertDescription>
      </Alert>
    )
  } else if (snapshot?.stale) {
    statusAlert = (
      <Alert className='border-warning/40 bg-warning/5 text-warning'>
        <AlertTriangle aria-hidden='true' />
        <AlertTitle>{t('Showing stale quota data')}</AlertTitle>
        <AlertDescription>
          {t(
            'The latest refresh failed or a reset boundary has passed. The last safe snapshot remains visible.'
          )}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Account Pool')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-2.5'>
          {statusAlert}

          {hasFatalError ? (
            <AccountPoolUnavailable
              notConfigured={apiError.code === 'account_pool_not_configured'}
            />
          ) : (
            <div className='min-h-0 flex-1'>
              <AccountPoolTable
                snapshot={snapshot}
                isLoading={accountPoolQuery.isLoading && !snapshot}
                isFetching={accountPoolQuery.isFetching}
                isRefreshing={refreshMutation.isPending}
                refreshDisabled={refreshDisabled}
                refreshLabel={refreshLabel}
                now={now}
                onRefresh={() => refreshMutation.mutate()}
              />
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
