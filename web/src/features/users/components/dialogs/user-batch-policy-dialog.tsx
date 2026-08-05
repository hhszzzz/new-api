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

import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getPricing } from '@/features/pricing/api'

import { batchUpdateUserPolicy } from '../../api'
import type {
  UserBatchListMode,
  UserBatchPolicyPayload,
  UserBatchRateLimitMode,
  UserBatchRateLimitsOp,
  UserBatchSkip,
} from '../../types'

type UserBatchPolicyDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  userIds: number[]
  onSuccess?: () => void
}

type ListSectionState = {
  mode: 'keep' | UserBatchListMode
  models: string[]
  enabled: 'keep' | 'on' | 'off'
}

const EMPTY_LIST_SECTION: ListSectionState = {
  mode: 'keep',
  models: [],
  enabled: 'keep',
}

type RateLimitState = {
  mode: UserBatchRateLimitMode
  value: string
}

type RateLimitsState = {
  rpm_limit: RateLimitState
  concurrency_limit: RateLimitState
  stream_tps_limit: RateLimitState
}

const EMPTY_RATE_LIMITS: RateLimitsState = {
  rpm_limit: { mode: 'keep', value: '' },
  concurrency_limit: { mode: 'keep', value: '' },
  stream_tps_limit: { mode: 'keep', value: '' },
}

function RateLimitField(props: {
  id: string
  label: string
  state: RateLimitState
  onChange: (state: RateLimitState) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='grid grid-cols-2 gap-2'>
      <div className='space-y-1'>
        <Label htmlFor={`${props.id}-mode`}>{props.label}</Label>
        <Select
          value={props.state.mode}
          onValueChange={(value) =>
            props.onChange({
              ...props.state,
              mode: value as UserBatchRateLimitMode,
            })
          }
        >
          <SelectTrigger id={`${props.id}-mode`} className='w-full'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='keep'>{t('Keep unchanged')}</SelectItem>
            <SelectItem value='clear'>{t('Clear override')}</SelectItem>
            <SelectItem value='custom'>{t('Custom limit')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      {props.state.mode === 'custom' && (
        <div className='space-y-1'>
          <Label htmlFor={`${props.id}-value`}>{t('Limit value')}</Label>
          <Input
            id={`${props.id}-value`}
            type='number'
            min={1}
            max={2_147_483_647}
            value={props.state.value}
            onChange={(event) =>
              props.onChange({ ...props.state, value: event.target.value })
            }
          />
        </div>
      )}
    </div>
  )
}

function listSectionToPayload(
  section: ListSectionState
): UserBatchPolicyPayload['model_limits'] {
  if (section.mode === 'keep' && section.enabled === 'keep') {
    return undefined
  }
  return {
    // The backend requires a valid list mode even when only the switch
    // changes; append with no models is a no-op on the list itself.
    mode: section.mode === 'keep' ? 'append' : section.mode,
    models: section.mode === 'keep' ? [] : section.models,
    enabled: section.enabled === 'keep' ? undefined : section.enabled === 'on',
  }
}

function ListSectionFields(props: {
  title: string
  idPrefix: string
  section: ListSectionState
  onChange: (section: ListSectionState) => void
  modelOptions: Array<{ value: string; label: string }>
  enableLabelOn: string
  enableLabelOff: string
}) {
  const { t } = useTranslation()
  const { section, onChange } = props
  return (
    <div className='space-y-2 rounded-md border p-3'>
      <div className='text-sm font-medium'>{props.title}</div>
      <div className='grid grid-cols-2 gap-2'>
        <div className='space-y-1'>
          <Label htmlFor={`${props.idPrefix}-mode`}>{t('List change')}</Label>
          <Select
            value={section.mode}
            onValueChange={(mode) =>
              onChange({ ...section, mode: mode as ListSectionState['mode'] })
            }
          >
            <SelectTrigger id={`${props.idPrefix}-mode`} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='keep'>{t('Keep unchanged')}</SelectItem>
              <SelectItem value='append'>{t('Append models')}</SelectItem>
              <SelectItem value='remove'>{t('Remove models')}</SelectItem>
              <SelectItem value='replace'>{t('Replace list')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='space-y-1'>
          <Label htmlFor={`${props.idPrefix}-enabled`}>{t('Switch')}</Label>
          <Select
            value={section.enabled}
            onValueChange={(enabled) =>
              onChange({
                ...section,
                enabled: enabled as ListSectionState['enabled'],
              })
            }
          >
            <SelectTrigger id={`${props.idPrefix}-enabled`} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='keep'>{t('Keep unchanged')}</SelectItem>
              <SelectItem value='on'>{props.enableLabelOn}</SelectItem>
              <SelectItem value='off'>{props.enableLabelOff}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      {section.mode !== 'keep' && (
        <MultiSelect
          id={`${props.idPrefix}-models`}
          options={props.modelOptions}
          selected={section.models}
          onChange={(models) => onChange({ ...section, models })}
          placeholder={t('Select models')}
          maxVisibleChips={5}
        />
      )}
    </div>
  )
}

export function UserBatchPolicyDialog(props: UserBatchPolicyDialogProps) {
  const { t } = useTranslation()
  const [limits, setLimits] = useState<ListSectionState>(EMPTY_LIST_SECTION)
  const [blocklist, setBlocklist] =
    useState<ListSectionState>(EMPTY_LIST_SECTION)
  const [checkinMode, setCheckinMode] = useState<
    'keep' | 'global' | 'allow' | 'deny'
  >('keep')
  const [checkinQuotaMode, setCheckinQuotaMode] = useState<
    'keep' | 'global' | 'custom'
  >('keep')
  const [checkinMin, setCheckinMin] = useState('')
  const [checkinMax, setCheckinMax] = useState('')
  const [quotaCapMode, setQuotaCapMode] = useState<
    'keep' | 'unlimited' | 'custom'
  >('keep')
  const [quotaCapValue, setQuotaCapValue] = useState('')
  const [rateLimits, setRateLimits] =
    useState<RateLimitsState>(EMPTY_RATE_LIMITS)
  const [submitting, setSubmitting] = useState(false)
  const [skipped, setSkipped] = useState<UserBatchSkip[]>([])

  const { data: pricingData } = useQuery({
    queryKey: ['pricing'],
    queryFn: getPricing,
    enabled: props.open,
    staleTime: 5 * 60 * 1000,
  })
  const modelOptions = useMemo(() => {
    const values = new Set([
      ...(pricingData?.data || []).map((model) => model.model_name),
      ...limits.models,
      ...blocklist.models,
    ])
    return [...values]
      .sort((left, right) => left.localeCompare(right))
      .map((model) => ({ value: model, label: model }))
  }, [pricingData?.data, limits.models, blocklist.models])

  const resetState = () => {
    setLimits(EMPTY_LIST_SECTION)
    setBlocklist(EMPTY_LIST_SECTION)
    setCheckinMode('keep')
    setCheckinQuotaMode('keep')
    setCheckinMin('')
    setCheckinMax('')
    setQuotaCapMode('keep')
    setQuotaCapValue('')
    setRateLimits(EMPTY_RATE_LIMITS)
    setSkipped([])
  }

  const handleOpenChange = (open: boolean) => {
    if (!open) resetState()
    props.onOpenChange(open)
  }

  const buildPayload = (): UserBatchPolicyPayload | string => {
    const payload: UserBatchPolicyPayload = { user_ids: props.userIds }
    payload.model_limits = listSectionToPayload(limits)
    payload.model_blocklist = listSectionToPayload(blocklist)
    if (checkinMode !== 'keep' || checkinQuotaMode !== 'keep') {
      if (checkinQuotaMode === 'custom') {
        const min = Number(checkinMin)
        const max = Number(checkinMax)
        if (
          checkinMin === '' ||
          checkinMax === '' ||
          !Number.isInteger(min) ||
          !Number.isInteger(max)
        ) {
          return t('Custom check-in reward requires both min and max values')
        }
        payload.checkin = {
          mode: checkinMode,
          quota_mode: 'custom',
          min_quota: min,
          max_quota: max,
        }
      } else {
        payload.checkin = {
          mode: checkinMode,
          quota_mode: checkinQuotaMode,
        }
      }
    }
    if (quotaCapMode !== 'keep') {
      if (quotaCapMode === 'custom') {
        const value = Number(quotaCapValue)
        if (quotaCapValue === '' || !Number.isInteger(value)) {
          return t('Custom balance cap requires a value')
        }
        payload.quota_cap = { mode: 'custom', value }
      } else {
        payload.quota_cap = { mode: 'unlimited' }
      }
    }
    const rateLimitPayload: UserBatchRateLimitsOp = {}
    for (const key of [
      'rpm_limit',
      'concurrency_limit',
      'stream_tps_limit',
    ] as const) {
      const limit = rateLimits[key]
      if (limit.mode === 'keep') continue
      if (limit.mode === 'clear') {
        rateLimitPayload[key] = { mode: 'clear' }
        continue
      }
      const value = Number(limit.value)
      if (
        limit.value === '' ||
        !Number.isInteger(value) ||
        value < 1 ||
        value > 2_147_483_647
      ) {
        return t('Custom request limits must be integers from 1 to 2147483647')
      }
      rateLimitPayload[key] = { mode: 'custom', value }
    }
    if (Object.keys(rateLimitPayload).length > 0) {
      payload.rate_limits = rateLimitPayload
    }
    if (
      !payload.model_limits &&
      !payload.model_blocklist &&
      !payload.checkin &&
      !payload.quota_cap &&
      !payload.rate_limits
    ) {
      return t('No changes selected')
    }
    return payload
  }

  const handleSubmit = async () => {
    const payload = buildPayload()
    if (typeof payload === 'string') {
      toast.error(payload)
      return
    }
    setSubmitting(true)
    try {
      const result = await batchUpdateUserPolicy(payload)
      if (!result.success || !result.data) {
        toast.error(result.message || t('Operation failed'))
        return
      }
      const { updated, skipped: skippedUsers } = result.data
      setSkipped(skippedUsers)
      if (skippedUsers.length === 0) {
        toast.success(t('{{count}} users updated', { count: updated }))
        handleOpenChange(false)
        props.onSuccess?.()
      } else {
        toast.warning(
          t('{{count}} users updated, {{skipped}} skipped', {
            count: updated,
            skipped: skippedUsers.length,
          })
        )
        props.onSuccess?.()
      }
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Batch update user policy')}
      description={t(
        'Apply the selected changes to {{count}} selected user(s). Sections left as "Keep unchanged" are untouched.',
        { count: props.userIds.length }
      )}
      contentHeight='auto'
      bodyClassName='space-y-3'
      footer={
        <>
          <Button variant='outline' onClick={() => handleOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSubmit} disabled={submitting}>
            {t('Apply')}
          </Button>
        </>
      }
    >
      <div className='space-y-3'>
        <ListSectionFields
          title={t('Allowed Models')}
          idPrefix='batch-limits'
          section={limits}
          onChange={setLimits}
          modelOptions={modelOptions}
          enableLabelOn={t('Enable limit')}
          enableLabelOff={t('Disable limit')}
        />
        <ListSectionFields
          title={t('Blocked Models')}
          idPrefix='batch-blocklist'
          section={blocklist}
          onChange={setBlocklist}
          modelOptions={modelOptions}
          enableLabelOn={t('Enable blocklist')}
          enableLabelOff={t('Disable blocklist')}
        />

        <div className='space-y-2 rounded-md border p-3'>
          <div className='text-sm font-medium'>{t('Check-in')}</div>
          <div className='grid grid-cols-2 gap-2'>
            <div className='space-y-1'>
              <Label htmlFor='batch-checkin-mode'>
                {t('Check-in permission')}
              </Label>
              <Select
                value={checkinMode}
                onValueChange={(value) =>
                  setCheckinMode(value as typeof checkinMode)
                }
              >
                <SelectTrigger id='batch-checkin-mode' className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='keep'>{t('Keep unchanged')}</SelectItem>
                  <SelectItem value='global'>
                    {t('Follow global setting')}
                  </SelectItem>
                  <SelectItem value='allow'>{t('Allow check-in')}</SelectItem>
                  <SelectItem value='deny'>{t('Deny check-in')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1'>
              <Label htmlFor='batch-checkin-quota'>
                {t('Check-in reward')}
              </Label>
              <Select
                value={checkinQuotaMode}
                onValueChange={(value) =>
                  setCheckinQuotaMode(value as typeof checkinQuotaMode)
                }
              >
                <SelectTrigger id='batch-checkin-quota' className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='keep'>{t('Keep unchanged')}</SelectItem>
                  <SelectItem value='global'>
                    {t('Follow global setting')}
                  </SelectItem>
                  <SelectItem value='custom'>{t('Custom range')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          {checkinQuotaMode === 'custom' && (
            <div className='grid grid-cols-2 gap-2'>
              <Input
                type='number'
                min={0}
                value={checkinMin}
                onChange={(e) => setCheckinMin(e.target.value)}
                placeholder={t('Min check-in reward')}
                aria-label={t('Min check-in reward')}
              />
              <Input
                type='number'
                min={0}
                value={checkinMax}
                onChange={(e) => setCheckinMax(e.target.value)}
                placeholder={t('Max check-in reward')}
                aria-label={t('Max check-in reward')}
              />
            </div>
          )}
        </div>

        <div className='space-y-2 rounded-md border p-3'>
          <div className='text-sm font-medium'>
            {t('Balance cap (quota units)')}
          </div>
          <div className='grid grid-cols-2 gap-2'>
            <Select
              value={quotaCapMode}
              onValueChange={(value) =>
                setQuotaCapMode(value as typeof quotaCapMode)
              }
            >
              <SelectTrigger
                className='w-full'
                aria-label={t('Balance cap (quota units)')}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='keep'>{t('Keep unchanged')}</SelectItem>
                <SelectItem value='unlimited'>{t('No cap')}</SelectItem>
                <SelectItem value='custom'>{t('Custom cap')}</SelectItem>
              </SelectContent>
            </Select>
            {quotaCapMode === 'custom' && (
              <Input
                type='number'
                min={0}
                value={quotaCapValue}
                onChange={(e) => setQuotaCapValue(e.target.value)}
                placeholder={t('Balance cap (quota units)')}
                aria-label={t('Balance cap (quota units)')}
              />
            )}
          </div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Gift credits (check-in, redemption codes, invite transfers) cannot push the balance above this cap. Paid top-ups are unaffected.'
            )}
          </p>
        </div>

        <div className='space-y-3 rounded-md border p-3'>
          <div>
            <div className='text-sm font-medium'>{t('Request Limits')}</div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Keep unchanged preserves each user value; clear removes the user override.'
              )}
            </p>
          </div>
          <RateLimitField
            id='batch-rpm-limit'
            label={t('Requests per minute')}
            state={rateLimits.rpm_limit}
            onChange={(state) =>
              setRateLimits((current) => ({ ...current, rpm_limit: state }))
            }
          />
          <RateLimitField
            id='batch-concurrency-limit'
            label={t('Concurrent requests')}
            state={rateLimits.concurrency_limit}
            onChange={(state) =>
              setRateLimits((current) => ({
                ...current,
                concurrency_limit: state,
              }))
            }
          />
          <RateLimitField
            id='batch-stream-tps-limit'
            label={t('Streaming tokens per second')}
            state={rateLimits.stream_tps_limit}
            onChange={(state) =>
              setRateLimits((current) => ({
                ...current,
                stream_tps_limit: state,
              }))
            }
          />
        </div>

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
  )
}
