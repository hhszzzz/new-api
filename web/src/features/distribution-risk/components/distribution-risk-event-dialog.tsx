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
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { formatTimestampToDate } from '@/lib/format'

import { getDistributionRiskEvent, reviewDistributionRiskEvent } from '../api'
import type { DistributionRiskEvent, DistributionRiskEvidence } from '../types'

type Props = {
  eventID: number | null
  canManage: boolean
  onOpenChange: (open: boolean) => void
  onReviewed: () => void
}

export function DistributionRiskEventDialog(props: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [reason, setReason] = useState('')
  const [pendingDecision, setPendingDecision] = useState<
    'confirmed' | 'false_positive' | null
  >(null)
  const detailQuery = useQuery({
    queryKey: ['distribution-risk', 'event', props.eventID],
    queryFn: () => getDistributionRiskEvent(props.eventID as number),
    enabled: props.eventID !== null,
  })
  const reviewMutation = useMutation({
    mutationFn: (decision: 'confirmed' | 'false_positive') =>
      reviewDistributionRiskEvent(
        props.eventID as number,
        decision,
        reason.trim()
      ),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['distribution-risk'] })
      toast.success(t('Distribution risk event reviewed'))
      setPendingDecision(null)
      setReason('')
      props.onOpenChange(false)
      props.onReviewed()
    },
    onError: (error) => toast.error(error.message || t('Review failed')),
  })
  const event = detailQuery.data
  const isOpen = props.eventID !== null
  const canReview = props.canManage && event?.status === 'pending'
  const evidence: DistributionRiskEvidence = {
    ips: event?.evidence?.ips ?? [],
    user_agents: event?.evidence?.user_agents ?? [],
    session_hashes: event?.evidence?.session_hashes ?? [],
  }
  const hasEvidence = Object.values(evidence).some((values) => !!values?.length)

  return (
    <>
      <Dialog open={isOpen} onOpenChange={props.onOpenChange}>
        <DialogContent className='sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle className='flex items-center gap-2'>
              <span>
                {t('Risk event')} #{event?.id ?? props.eventID}
              </span>
              {event && (
                <CopyButton
                  value={String(event.id)}
                  className='size-6'
                  iconClassName='size-3.5'
                  tooltip={t('Copy {{field}}', { field: t('Risk event ID') })}
                  aria-label={t('Copy {{field}}', {
                    field: t('Risk event ID'),
                  })}
                />
              )}
            </DialogTitle>
            <DialogDescription>
              {t('Review the evidence and record a reason for your decision.')}
            </DialogDescription>
          </DialogHeader>
          {detailQuery.isLoading && (
            <p className='text-muted-foreground py-6 text-sm'>
              {t('Loading...')}
            </p>
          )}
          {detailQuery.isError && (
            <div
              role='alert'
              className='text-destructive flex flex-wrap items-center justify-between gap-3 py-6 text-sm'
            >
              <span>
                {detailQuery.error.message || t('Failed to load event details')}
              </span>
              <Button
                size='sm'
                variant='outline'
                onClick={() => void detailQuery.refetch()}
              >
                {t('Retry')}
              </Button>
            </div>
          )}
          {!detailQuery.isLoading && !detailQuery.isError && event && (
            <EventDetail
              event={event}
              evidence={evidence}
              hasEvidence={hasEvidence}
              canReview={canReview}
              reason={reason}
              onReasonChange={setReason}
              onDecision={setPendingDecision}
              isReviewing={reviewMutation.isPending}
            />
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={pendingDecision !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDecision(null)
        }}
        title={
          pendingDecision === 'confirmed'
            ? t('Confirm distribution violation')
            : t('Release this event')
        }
        desc={
          pendingDecision === 'confirmed'
            ? t('All contributing tokens will be disabled.')
            : t(
                'The hold will be released and this signal will be suppressed for 48 hours.'
              )
        }
        destructive={pendingDecision === 'confirmed'}
        disabled={!reason.trim() || reviewMutation.isPending}
        isLoading={reviewMutation.isPending}
        confirmText={
          pendingDecision === 'confirmed'
            ? t('Disable contributing tokens')
            : t('Release hold')
        }
        handleConfirm={() => {
          if (pendingDecision) void reviewMutation.mutate(pendingDecision)
        }}
      />
    </>
  )
}

function EventDetail(props: {
  event: DistributionRiskEvent
  evidence: DistributionRiskEvidence
  hasEvidence: boolean
  canReview: boolean
  reason: string
  onReasonChange: (reason: string) => void
  onDecision: (decision: 'confirmed' | 'false_positive') => void
  isReviewing: boolean
}) {
  const { t } = useTranslation()
  const event = props.event

  return (
    <div className='max-h-[70dvh] space-y-5 overflow-y-auto pr-1'>
      <div className='grid grid-cols-2 gap-x-4 gap-y-2 text-sm sm:grid-cols-4'>
        <Fact
          label={t('User ID')}
          value={String(event.user_id)}
          copyValue={String(event.user_id)}
          copyLabel={t('Copy {{field}}', { field: t('User ID') })}
        />
        <Fact
          label={t('Scope')}
          value={`${event.scope_kind === 'user' ? t('User account') : t('Token')} ${event.scope_id}`}
          copyValue={String(event.scope_id)}
          copyLabel={t('Copy {{field}}', { field: t('Scope ID') })}
        />
        <Fact label={t('Risk score')} value={String(event.score)} />
        <Fact
          label={t('Observed at')}
          value={formatTimestampToDate(event.created_at)}
        />
        <Fact
          label={t('Contributing tokens')}
          value={event.token_ids.join(', ') || '-'}
          copyValue={event.token_ids.join(', ')}
          copyLabel={t('Copy {{field}}', { field: t('Contributing tokens') })}
        />
        <Fact label={t('Status')} value={statusLabel(event.status, t)} />
        {event.reviewed_at > 0 && (
          <Fact
            label={t('Reviewed at')}
            value={formatTimestampToDate(event.reviewed_at)}
          />
        )}
        {event.reviewer_id > 0 && (
          <Fact
            label={t('Reviewer ID')}
            value={String(event.reviewer_id)}
            copyValue={String(event.reviewer_id)}
            copyLabel={t('Copy {{field}}', { field: t('Reviewer ID') })}
          />
        )}
      </div>

      <section className='space-y-2'>
        <h3 className='text-sm font-semibold'>{t('Signal scores')}</h3>
        <div className='flex flex-wrap gap-2'>
          {Object.entries(event.category_scores ?? {}).map(
            ([category, score]) => (
              <Badge
                key={category}
                variant={score >= 30 ? 'warning' : 'outline'}
              >
                {t(categoryLabel(category))} {score}
              </Badge>
            )
          )}
        </div>
      </section>

      <section className='space-y-3'>
        <h3 className='text-sm font-semibold'>{t('Client evidence')}</h3>
        {props.hasEvidence ? (
          <div className='grid gap-4 sm:grid-cols-2'>
            <EvidenceList
              title={t('IP addresses')}
              values={props.evidence.ips ?? []}
            />
            <EvidenceList
              title={t('User agents')}
              values={props.evidence.user_agents ?? []}
            />
            <EvidenceList
              title={t('Hashed session IDs')}
              values={props.evidence.session_hashes ?? []}
            />
          </div>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('Detailed evidence has expired after 30 days.')}
          </p>
        )}
      </section>

      {event.review_reason && (
        <Fact label={t('Review reason')} value={event.review_reason} />
      )}

      {props.canReview && (
        <section className='space-y-3 border-t pt-4'>
          <label className='block space-y-1.5'>
            <span className='text-sm font-medium'>{t('Review reason')}</span>
            <Textarea
              maxLength={512}
              value={props.reason}
              onChange={(change) => props.onReasonChange(change.target.value)}
              placeholder={t('Record why you made this decision')}
            />
          </label>
          <div className='flex flex-wrap justify-end gap-2'>
            <Button
              variant='outline'
              disabled={!props.reason.trim() || props.isReviewing}
              onClick={() => props.onDecision('false_positive')}
            >
              {t('False positive')}
            </Button>
            <Button
              variant='destructive'
              disabled={!props.reason.trim() || props.isReviewing}
              onClick={() => props.onDecision('confirmed')}
            >
              {t('Confirm violation')}
            </Button>
          </div>
        </section>
      )}
    </div>
  )
}

function Fact(props: {
  label: string
  value: ReactNode
  copyValue?: string
  copyLabel?: string
}) {
  return (
    <div className='min-w-0 space-y-0.5'>
      <p className='text-muted-foreground text-xs'>{props.label}</p>
      <div className='flex min-w-0 items-start gap-1'>
        <p className='min-w-0 flex-1 text-sm font-medium break-words'>
          {props.value}
        </p>
        {props.copyValue && (
          <CopyButton
            value={props.copyValue}
            className='size-5'
            iconClassName='size-3'
            tooltip={props.copyLabel ?? props.label}
            aria-label={props.copyLabel ?? props.label}
          />
        )}
      </div>
    </div>
  )
}

function EvidenceList(props: { title: string; values: string[] }) {
  const { t } = useTranslation()
  const copyLabel = t('Copy {{field}}', { field: props.title })

  return (
    <div className='min-w-0 space-y-1'>
      <div className='flex items-center justify-between gap-2'>
        <h4 className='text-muted-foreground text-xs font-medium'>
          {props.title} ({props.values.length})
        </h4>
        {props.values.length > 0 && (
          <CopyButton
            value={props.values.join('\n')}
            className='size-6 p-1'
            tooltip={copyLabel}
            aria-label={copyLabel}
          />
        )}
      </div>
      <ul className='max-h-36 space-y-1 overflow-y-auto rounded-md border p-2 font-mono text-xs'>
        {props.values.map((value) => (
          <li className='break-all' key={value}>
            {value}
          </li>
        ))}
      </ul>
    </div>
  )
}

function statusLabel(
  status: DistributionRiskEvent['status'],
  t: (key: string) => string
) {
  if (status === 'false_positive') return t('False positive')
  if (status === 'confirmed') return t('Confirmed')
  return t('Pending')
}

function categoryLabel(category: string) {
  switch (category) {
    case 'network':
      return 'Network'
    case 'client':
      return 'Client'
    case 'session':
      return 'Session'
    default:
      return 'Activity'
  }
}
