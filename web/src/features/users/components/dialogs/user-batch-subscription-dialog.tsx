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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  batchAssignUserSubscriptions,
  batchResetUserSubscriptions,
  batchRevokeUserSubscriptions,
  getAdminPlans,
} from '@/features/subscriptions/api'
import type { UserBatchSubscriptionSkip } from '@/features/subscriptions/types'

type BatchSubscriptionOperation =
  | 'assign'
  | 'invalidate'
  | 'delete'
  | 'reset'

type UserBatchSubscriptionDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  userIds: number[]
  onSuccess?: () => void
}

export function UserBatchSubscriptionDialog(
  props: UserBatchSubscriptionDialogProps
) {
  const { t } = useTranslation()
  const [operation, setOperation] =
    useState<BatchSubscriptionOperation>('assign')
  const [selectedPlanId, setSelectedPlanId] = useState('')
  const [sourceNote, setSourceNote] = useState('')
  const [advanceResetTime, setAdvanceResetTime] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [skipped, setSkipped] = useState<UserBatchSubscriptionSkip[]>([])
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false)

  const plansQuery = useQuery({
    queryKey: ['admin-subscription-plans'],
    queryFn: getAdminPlans,
    enabled: props.open,
    staleTime: 60 * 1000,
  })
  const assignablePlans = useMemo(
    () =>
      (plansQuery.data?.success === true ? (plansQuery.data.data ?? []) : [])
        .filter((record) => record.plan.enabled)
        .map((record) => ({
          value: String(record.plan.id),
          label: `${record.plan.title} ($${Number(record.plan.price_amount || 0).toFixed(2)})`,
        })),
    [plansQuery.data]
  )

  const resetState = () => {
    setOperation('assign')
    setSelectedPlanId('')
    setSourceNote('')
    setAdvanceResetTime(true)
    setSkipped([])
    setConfirmDeleteOpen(false)
  }

  const handleOpenChange = (open: boolean) => {
    if (!open) resetState()
    props.onOpenChange(open)
  }

  const finish = (
    result: { updated: number; skipped: UserBatchSubscriptionSkip[] },
    successMessage: string
  ) => {
    setSkipped(result.skipped)
    if (result.skipped.length === 0) {
      toast.success(successMessage)
      handleOpenChange(false)
    } else {
      toast.warning(
        t('{{count}} users updated, {{skipped}} skipped', {
          count: result.updated,
          skipped: result.skipped.length,
        })
      )
    }
    props.onSuccess?.()
  }

  const handleSubmit = async () => {
    const planId = Number(selectedPlanId)
    if (!planId) {
      toast.error(t('Please select a subscription plan'))
      return
    }
    if (operation === 'delete' && !confirmDeleteOpen) {
      setConfirmDeleteOpen(true)
      return
    }
    setConfirmDeleteOpen(false)
    setSubmitting(true)
    try {
      if (operation === 'assign') {
        const result = await batchAssignUserSubscriptions({
          user_ids: props.userIds,
          plan_id: planId,
          source_note: sourceNote.trim(),
        })
        if (!result.success || !result.data) {
          toast.error(result.message || t('Operation failed'))
          return
        }
        finish(
          result.data,
          t('{{count}} users updated', { count: result.data.updated })
        )
        return
      }
      if (operation === 'reset') {
        const result = await batchResetUserSubscriptions({
          user_ids: props.userIds,
          plan_id: planId,
          advance_reset_time: advanceResetTime,
        })
        if (!result.success || !result.data) {
          toast.error(result.message || t('Operation failed'))
          return
        }
        finish(
          result.data,
          t('{{count}} users updated', { count: result.data.updated })
        )
        return
      }
      const result = await batchRevokeUserSubscriptions({
        user_ids: props.userIds,
        plan_id: planId,
        action: operation === 'delete' ? 'delete' : 'invalidate',
      })
      if (!result.success || !result.data) {
        toast.error(result.message || t('Operation failed'))
        return
      }
      finish(
        result.data,
        t('{{count}} users updated', { count: result.data.updated })
      )
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={handleOpenChange}
        title={t('Batch manage subscriptions')}
        description={t(
          'Apply one subscription operation to {{count}} selected user(s). Users without a matching active subscription are skipped and reported.',
          { count: props.userIds.length }
        )}
        contentHeight='auto'
        bodyClassName='space-y-3'
        footer={
          <>
            <Button variant='outline' onClick={() => handleOpenChange(false)}>
              {t('Cancel')}
            </Button>
            <Button
              variant={operation === 'delete' ? 'destructive' : 'default'}
              onClick={handleSubmit}
              disabled={submitting}
            >
              {t('Apply')}
            </Button>
          </>
        }
      >
        <div className='space-y-3'>
          <div className='space-y-1'>
            <Label htmlFor='batch-subscription-operation'>
              {t('Operation')}
            </Label>
            <Select
              value={operation}
              onValueChange={(value) =>
                setOperation(value as BatchSubscriptionOperation)
              }
            >
              <SelectTrigger id='batch-subscription-operation' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='assign'>{t('Assign subscription')}</SelectItem>
                <SelectItem value='invalidate'>
                  {t('Invalidate subscription')}
                </SelectItem>
                <SelectItem value='delete'>
                  {t('Delete subscription')}
                </SelectItem>
                <SelectItem value='reset'>
                  {t('Reset subscription quota')}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className='space-y-1'>
            <Label>{t('Subscription plan')}</Label>
            <Combobox
              options={assignablePlans}
              value={selectedPlanId}
              onValueChange={(value) => setSelectedPlanId(value ?? '')}
              placeholder={t('Select subscription plan')}
            />
          </div>

          {operation === 'assign' && (
            <div className='space-y-1'>
              <Label htmlFor='batch-subscription-note'>
                {t('Administrator assignment note (optional)')}
              </Label>
              <Input
                id='batch-subscription-note'
                value={sourceNote}
                onChange={(event) => setSourceNote(event.target.value)}
                maxLength={255}
              />
            </div>
          )}

          {operation === 'invalidate' && (
            <p className='text-muted-foreground text-sm'>
              {t('Cancels the subscriptions but keeps their records.')}
            </p>
          )}
          {operation === 'delete' && (
            <p className='text-muted-foreground text-sm'>
              {t('Permanently removes the subscription records.')}
            </p>
          )}
          {operation === 'reset' && (
            <div className='flex items-center justify-between gap-3'>
              <Label htmlFor='batch-subscription-advance-reset'>
                {t('Advance next reset time')}
              </Label>
              <Switch
                id='batch-subscription-advance-reset'
                checked={advanceResetTime}
                onCheckedChange={setAdvanceResetTime}
              />
            </div>
          )}

          {skipped.length > 0 && (
            <div className='space-y-1 rounded-md border border-amber-300 p-3 text-xs'>
              <div className='font-medium'>{t('Skipped users')}</div>
              {skipped.map((entry) => (
                <div key={entry.id}>
                  #{entry.id} {entry.username || ''} — {entry.reason}
                </div>
              ))}
            </div>
          )}
        </div>
      </Dialog>

      <ConfirmDialog
        open={confirmDeleteOpen}
        onOpenChange={setConfirmDeleteOpen}
        title={t('Confirm batch subscription deletion')}
        desc={t(
          'This permanently deletes every active subscription of the selected plan for {{count}} selected user(s). Continue?',
          { count: props.userIds.length }
        )}
        handleConfirm={handleSubmit}
        destructive
      />
    </>
  )
}
