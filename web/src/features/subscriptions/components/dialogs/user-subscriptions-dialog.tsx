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
import { Ban, Pause, Play, Plus, RotateCcw, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  DataTableRowActionMenu,
  StaticDataTable,
} from '@/components/data-table'
import {
  sideDrawerContentClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getAdminPlans,
  getUserSubscriptions,
  createUserSubscription,
  invalidateUserSubscription,
  pauseUserSubscription,
  resumeUserSubscription,
  deleteUserSubscription,
  resetUserSubscriptionsByPlan,
} from '../../api'
import { USAGE_WINDOW_LABELS } from '../../constants'
import {
  formatTimestamp,
  getSubscriptionState,
  getUsageMeters,
} from '../../lib'
import type { PlanRecord, UserSubscriptionRecord } from '../../types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username?: string } | null
  onSuccess?: () => void
}

function isSubscriptionActive(
  subscription: UserSubscriptionRecord['subscription'],
  nowSeconds: number
) {
  return getSubscriptionState(subscription, nowSeconds) === 'active'
}

function SubscriptionStatusBadge(props: {
  sub: UserSubscriptionRecord['subscription']
  nowSeconds: number
  t: (key: string) => string
}) {
  const state = getSubscriptionState(props.sub, props.nowSeconds)
  if (state === 'active') {
    return (
      <StatusBadge
        label={props.t('Active')}
        variant='success'
        copyable={false}
      />
    )
  }
  if (state === 'paused') {
    return (
      <StatusBadge
        label={props.t('Paused')}
        variant='warning'
        copyable={false}
      />
    )
  }
  if (state === 'cancelled') {
    return (
      <StatusBadge
        label={props.t('Invalidated')}
        variant='neutral'
        copyable={false}
      />
    )
  }
  return (
    <StatusBadge
      label={props.t('Expired')}
      variant='neutral'
      copyable={false}
    />
  )
}

function SubscriptionUsageCell(props: {
  sub: UserSubscriptionRecord['subscription']
  nowSeconds: number
}) {
  const { t } = useTranslation()
  const meters = getUsageMeters(props.sub, props.nowSeconds)
  if (meters.length === 0) {
    return <span className='text-muted-foreground'>{t('Unlimited')}</span>
  }
  return (
    <div className='space-y-0.5 text-xs'>
      {meters.map((meter) => (
        <div key={meter.key} className='whitespace-nowrap'>
          <span className='text-muted-foreground'>
            {t(USAGE_WINDOW_LABELS[meter.key])}
          </span>{' '}
          <span className='tabular-nums'>
            {formatQuota(meter.used)}/{formatQuota(meter.amount)}
          </span>
        </div>
      ))}
    </div>
  )
}

export function UserSubscriptionsDialog(props: Props) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [subs, setSubs] = useState<UserSubscriptionRecord[]>([])
  // Snapshot taken whenever the list loads, so status checks stay stable
  // across re-renders instead of drifting with the wall clock.
  const [nowSeconds, setNowSeconds] = useState(() => Date.now() / 1000)
  const [selectedPlanId, setSelectedPlanId] = useState<string>('')
  const [sourceNote, setSourceNote] = useState('')
  const [pendingAssignment, setPendingAssignment] = useState<{
    planId: number
    planTitle: string
    sourceNote: string
  } | null>(null)
  const [resetting, setResetting] = useState(false)
  const [advanceResetTime, setAdvanceResetTime] = useState(true)
  const [resetAction, setResetAction] = useState<{
    planId: number
    planTitle: string
  } | null>(null)
  const [confirmAction, setConfirmAction] = useState<{
    type: 'invalidate' | 'delete' | 'pause'
    subId: number
  } | null>(null)
  const [resumingId, setResumingId] = useState<number | null>(null)

  const planTitleMap = useMemo(() => {
    const map = new Map<number, string>()
    plans.forEach((p) => {
      if (p.plan.id) map.set(p.plan.id, p.plan.title || `#${p.plan.id}`)
    })
    return map
  }, [plans])
  const assignablePlans = useMemo(
    () => plans.filter((record) => record.plan.enabled),
    [plans]
  )

  const userId = props.user?.id

  const loadData = useCallback(() => {
    if (!userId) return Promise.resolve()
    return Promise.all([getAdminPlans(), getUserSubscriptions(userId)])
      .then(([plansRes, subsRes]) => {
        if (plansRes.success) {
          setPlans(plansRes.data || [])
        } else {
          handleServerError(plansRes)
        }
        if (subsRes.success) {
          setSubs(subsRes.data || [])
          setNowSeconds(Date.now() / 1000)
        } else {
          handleServerError(subsRes)
        }
      })
      .catch((error: unknown) => {
        handleServerError(error, t('Loading failed'))
      })
      .finally(() => {
        setLoading(false)
      })
  }, [userId, t])

  const refreshData = useCallback(async () => {
    setLoading(true)
    await loadData()
  }, [loadData])

  const openUserKey = `${props.open ? 'open' : 'closed'}:${userId ?? ''}`
  const [prevOpenUserKey, setPrevOpenUserKey] = useState<string | null>(null)
  if (prevOpenUserKey !== openUserKey) {
    setPrevOpenUserKey(openUserKey)
    if (props.open && userId) {
      setSelectedPlanId('')
      setSourceNote('')
      setLoading(true)
    }
  }

  useEffect(() => {
    if (props.open && userId) {
      void loadData()
    }
  }, [props.open, userId, loadData])

  const createAssignment = async (assignment: {
    planId: number
    sourceNote: string
  }) => {
    if (!props.user?.id) return
    setCreating(true)
    try {
      const res = await createUserSubscription(props.user.id, {
        plan_id: assignment.planId,
        source_note: assignment.sourceNote,
      })
      if (res.success) {
        toast.success(res.data?.message || t('Added successfully'))
        setSelectedPlanId('')
        setSourceNote('')
        await refreshData()
        props.onSuccess?.()
      } else {
        handleServerError(res)
      }
    } catch (error) {
      handleServerError(error, t('Request failed'))
    } finally {
      setCreating(false)
    }
  }

  const handleCreate = async () => {
    if (!props.user?.id || !selectedPlanId) {
      toast.error(t('Please select a subscription plan'))
      return
    }
    const note = sourceNote.trim()
    const planId = Number(selectedPlanId)
    const planTitle = planTitleMap.get(planId) || `#${planId}`
    const hasActiveDuplicate = subs.some(
      (record) =>
        record.subscription.plan_id === planId &&
        isSubscriptionActive(record.subscription, nowSeconds)
    )
    if (hasActiveDuplicate) {
      setPendingAssignment({ planId, planTitle, sourceNote: note })
      return
    }
    await createAssignment({ planId, sourceNote: note })
  }

  const handleConfirmAction = async () => {
    if (!confirmAction) return
    try {
      if (confirmAction.type === 'invalidate') {
        const res = await invalidateUserSubscription(confirmAction.subId)
        if (res.success) {
          toast.success(res.data?.message || t('Has been invalidated'))
          await refreshData()
          props.onSuccess?.()
        } else {
          handleServerError(res)
        }
      } else if (confirmAction.type === 'pause') {
        const res = await pauseUserSubscription(confirmAction.subId)
        if (res.success) {
          toast.success(res.data?.message || t('Subscription paused'))
          await refreshData()
          props.onSuccess?.()
        } else {
          handleServerError(res)
        }
      } else {
        const res = await deleteUserSubscription(confirmAction.subId)
        if (res.success) {
          toast.success(t('Deleted'))
          await refreshData()
          props.onSuccess?.()
        } else {
          handleServerError(res)
        }
      }
    } catch (error) {
      handleServerError(error, t('Operation failed'))
    } finally {
      setConfirmAction(null)
    }
  }

  const handleResume = async (subId: number) => {
    setResumingId(subId)
    try {
      const res = await resumeUserSubscription(subId)
      if (res.success) {
        toast.success(res.data?.message || t('Subscription resumed'))
        await refreshData()
        props.onSuccess?.()
      } else {
        handleServerError(res)
      }
    } catch (error) {
      handleServerError(error, t('Operation failed'))
    } finally {
      setResumingId(null)
    }
  }

  const handleResetConfirm = async () => {
    if (!props.user?.id || !resetAction) return
    setResetting(true)
    try {
      const res = await resetUserSubscriptionsByPlan(props.user.id, {
        plan_id: resetAction.planId,
        advance_reset_time: advanceResetTime,
      })
      if (res.success) {
        toast.success(
          t('Reset {{count}} active subscriptions', {
            count: res.data?.reset_count || 0,
          })
        )
        await refreshData()
        props.onSuccess?.()
      } else {
        handleServerError(res)
      }
    } catch (error) {
      handleServerError(error, t('Operation failed'))
    } finally {
      setResetting(false)
      setResetAction(null)
    }
  }

  const confirmActionCopy = {
    invalidate: {
      title: t('Confirm invalidate'),
      desc: t(
        'Invalidating ends this subscription now and cannot be undone. Use Pause if the user should get it back later. Continue?'
      ),
      confirm: t('Invalidate'),
    },
    pause: {
      title: t('Pause subscription'),
      desc: t(
        'While paused, the subscription stops funding requests and its group is released. Resuming restores the group and extends the end time by the paused duration.'
      ),
      confirm: t('Pause'),
    },
    delete: {
      title: t('Confirm delete'),
      desc: t(
        'Deleting will permanently remove this subscription record (including benefit details). Continue?'
      ),
      confirm: t('Delete'),
    },
  }

  return (
    <>
      <Sheet open={props.open} onOpenChange={props.onOpenChange}>
        <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>{t('User Subscription Management')}</SheetTitle>
            <SheetDescription>
              {props.user?.username || '-'} (ID: {props.user?.id || '-'})
            </SheetDescription>
          </SheetHeader>

          <div className={sideDrawerFormClassName()}>
            <div className='flex flex-col gap-2 sm:flex-row'>
              <Combobox
                options={assignablePlans.map((p) => ({
                  value: String(p.plan.id),
                  label: `${p.plan.title} ($${Number(p.plan.price_amount || 0).toFixed(2)})`,
                }))}
                value={selectedPlanId}
                onValueChange={(v) => v !== null && setSelectedPlanId(v)}
                className='flex-1'
                placeholder={t('Select subscription plan')}
                aria-label={t('Subscription plan')}
              />
              <Input
                value={sourceNote}
                onChange={(event) => setSourceNote(event.target.value)}
                maxLength={255}
                placeholder={t('Administrator assignment note (optional)')}
                aria-label={t('Administrator assignment note (optional)')}
                className='sm:max-w-64'
              />
              <Button
                onClick={handleCreate}
                disabled={creating || !selectedPlanId}
              >
                <Plus className='mr-1 h-4 w-4' />
                {t('Add subscription')}
              </Button>
            </div>

            <StaticDataTable
              data={loading ? [] : subs}
              getRowKey={(record) => record.subscription.id}
              emptyClassName={loading ? 'py-8' : 'text-muted-foreground py-8'}
              emptyContent={
                loading ? t('Loading...') : t('No subscription records')
              }
              columns={[
                {
                  id: 'id',
                  header: t('ID'),
                  cell: (record) => <TableId value={record.subscription.id} />,
                },
                {
                  id: 'plan',
                  header: t('Plan'),
                  cell: (record) => {
                    const sub = record.subscription

                    return (
                      <div>
                        <div className='font-medium'>
                          {planTitleMap.get(sub.plan_id) || `#${sub.plan_id}`}
                        </div>
                        <div className='text-muted-foreground text-sm'>
                          {t('Source')}:{' '}
                          {sub.source === 'admin'
                            ? t('Administrator assignment')
                            : sub.source || '-'}
                          {sub.source_note ? (
                            <span
                              className='block truncate'
                              title={sub.source_note}
                            >
                              {sub.source_note}
                            </span>
                          ) : null}
                        </div>
                      </div>
                    )
                  },
                },
                {
                  id: 'status',
                  header: t('Status'),
                  cell: (record) => (
                    <SubscriptionStatusBadge
                      sub={record.subscription}
                      nowSeconds={nowSeconds}
                      t={t}
                    />
                  ),
                },
                {
                  id: 'validity',
                  header: t('Validity'),
                  cell: (record) => {
                    const sub = record.subscription

                    return (
                      <div className='text-sm'>
                        <div>
                          {t('Start')}: {formatTimestamp(sub.start_time)}
                        </div>
                        <div>
                          {t('End')}: {formatTimestamp(sub.end_time)}
                        </div>
                      </div>
                    )
                  },
                },
                {
                  id: 'quota',
                  header: t('Quota'),
                  cell: (record) => (
                    <SubscriptionUsageCell
                      sub={record.subscription}
                      nowSeconds={nowSeconds}
                    />
                  ),
                },
                {
                  id: 'actions',
                  header: t('Actions'),
                  className: 'text-right',
                  cellClassName: 'text-right',
                  cell: (record) => {
                    const sub = record.subscription
                    const isActive = isSubscriptionActive(sub, nowSeconds)
                    const isPaused = sub.status === 'paused'

                    return (
                      <DataTableRowActionMenu ariaLabel={t('Actions')}>
                        {isPaused ? (
                          <DropdownMenuItem
                            disabled={resumingId === sub.id}
                            onClick={() => void handleResume(sub.id)}
                          >
                            {t('Resume')}
                            <DropdownMenuShortcut>
                              <Play size={16} />
                            </DropdownMenuShortcut>
                          </DropdownMenuItem>
                        ) : (
                          <DropdownMenuItem
                            disabled={!isActive}
                            onClick={() =>
                              setConfirmAction({ type: 'pause', subId: sub.id })
                            }
                          >
                            {t('Pause')}
                            <DropdownMenuShortcut>
                              <Pause size={16} />
                            </DropdownMenuShortcut>
                          </DropdownMenuItem>
                        )}
                        <DropdownMenuItem
                          disabled={!isActive}
                          onClick={() => {
                            setAdvanceResetTime(true)
                            setResetAction({
                              planId: sub.plan_id,
                              planTitle:
                                planTitleMap.get(sub.plan_id) ||
                                `#${sub.plan_id}`,
                            })
                          }}
                        >
                          {t('Reset quota')}
                          <DropdownMenuShortcut>
                            <RotateCcw size={16} />
                          </DropdownMenuShortcut>
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          disabled={!isActive && !isPaused}
                          onClick={() =>
                            setConfirmAction({
                              type: 'invalidate',
                              subId: sub.id,
                            })
                          }
                        >
                          {t('Invalidate')}
                          <DropdownMenuShortcut>
                            <Ban size={16} />
                          </DropdownMenuShortcut>
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          variant='destructive'
                          onClick={() =>
                            setConfirmAction({
                              type: 'delete',
                              subId: sub.id,
                            })
                          }
                        >
                          {t('Delete')}
                          <DropdownMenuShortcut>
                            <Trash2 size={16} />
                          </DropdownMenuShortcut>
                        </DropdownMenuItem>
                      </DataTableRowActionMenu>
                    )
                  },
                },
              ]}
            />
          </div>
        </SheetContent>
      </Sheet>

      {confirmAction && (
        <ConfirmDialog
          open
          onOpenChange={(v) => !v && setConfirmAction(null)}
          title={confirmActionCopy[confirmAction.type].title}
          desc={confirmActionCopy[confirmAction.type].desc}
          confirmText={confirmActionCopy[confirmAction.type].confirm}
          handleConfirm={handleConfirmAction}
          destructive={confirmAction.type === 'delete'}
        />
      )}

      {pendingAssignment && (
        <ConfirmDialog
          open
          onOpenChange={(open) => !open && setPendingAssignment(null)}
          title={t('Add another subscription')}
          desc={t(
            'This user already has an active subscription for {{plan}}. Add another one?',
            { plan: pendingAssignment.planTitle }
          )}
          confirmText={t('Add subscription')}
          handleConfirm={async () => {
            const assignment = pendingAssignment
            setPendingAssignment(null)
            await createAssignment(assignment)
          }}
          isLoading={creating}
        />
      )}

      {resetAction && (
        <ConfirmDialog
          open
          onOpenChange={(v) => !v && setResetAction(null)}
          title={t('Reset subscription quota')}
          desc={t('Reset active {{plan}} subscriptions for this user?', {
            plan: resetAction.planTitle,
          })}
          confirmText={t('Reset quota')}
          handleConfirm={handleResetConfirm}
          isLoading={resetting}
        >
          <label className='flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm'>
            <span>{t('Advance next reset time')}</span>
            <Switch
              checked={advanceResetTime}
              onCheckedChange={(checked) => setAdvanceResetTime(!!checked)}
              aria-label={t('Advance next reset time')}
            />
          </label>
        </ConfirmDialog>
      )}
    </>
  )
}
