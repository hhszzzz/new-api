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
import dayjs from 'dayjs'
import { ExternalLink, RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import {
  CollapsibleDetailSection,
  DetailRow,
  DetailSection,
} from '@/features/usage-logs/components/dialogs/log-detail-layout'
import { cn } from '@/lib/utils'

import { getPromptAudit, retryPromptAudit, reviewPromptAudit } from '../api'
import { getPromptAuditProtocolName } from '../lib'
import { promptAuditScopeLabel } from '../scopes'
import type { PromptAuditEvent } from '../types'

type PromptAuditDetailSheetProps = {
  eventID: number | null
  canViewFullPrompt: boolean
  canManage: boolean
  canDelete: boolean
  onOpenChange: (open: boolean) => void
  onDelete: (event: PromptAuditEvent) => void
}

function getDecisionBadgeVariant(decision: string) {
  if (decision === 'block' || decision === 'unavailable') {
    return 'destructive'
  }
  if (decision === 'flag') {
    return 'warning'
  }
  return 'secondary'
}

export function PromptAuditDetailSheet({
  eventID,
  canViewFullPrompt,
  canManage,
  canDelete,
  onOpenChange,
  onDelete,
}: PromptAuditDetailSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [reviewReason, setReviewReason] = useState('')
  const detailQuery = useQuery({
    queryKey: ['prompt-audit', 'event', eventID],
    enabled: eventID !== null,
    queryFn: async () => {
      const result = await getPromptAudit(eventID ?? 0)
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load audit details'))
      }
      return result.data
    },
  })
  const retryMutation = useMutation({
    mutationFn: async (id: number) => {
      const result = await retryPromptAudit(id)
      if (!result.success) {
        throw new Error(result.message || t('Failed to retry audit task'))
      }
    },
    onSuccess: async () => {
      toast.success(t('Audit task queued for retry'))
      await queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
    },
    onError: (error) => toast.error(error.message),
  })
  const reviewMutation = useMutation({
    mutationFn: (status: 'false_positive' | 'confirmed_violation') =>
      reviewPromptAudit(eventID ?? 0, status, reviewReason),
    onSuccess: async () => {
      toast.success(t('Audit review saved'))
      await queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
    },
    onError: (error) => toast.error(error.message),
  })

  const event = detailQuery.data
  const showsFullPrompt = canViewFullPrompt && event?.full_prompt !== undefined
  const prompt = showsFullPrompt
    ? (event?.full_prompt ?? '')
    : (event?.redacted_preview ?? '')
  // A rejected request never reaches upstream, and the audit deliberately
  // writes neither a consume log nor an error log for it, so both related-log
  // links would lead to an empty page.
  const hasRelatedLogs =
    event?.action !== 'block' && event?.action !== 'unavailable'

  return (
    <Sheet
      open={eventID !== null}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <SheetContent className='w-full sm:max-w-2xl'>
        <SheetHeader className='border-b'>
          <SheetTitle>{t('Prompt audit details')}</SheetTitle>
          <SheetDescription>
            {event
              ? `#${event.id} · ${event.request_id || t('No request ID')}`
              : t('Loading...')}
          </SheetDescription>
        </SheetHeader>

        <div className='min-h-0 flex-1 overflow-y-auto px-4 pb-4'>
          {detailQuery.isLoading && (
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('Loading audit details...')}
            </p>
          )}
          {detailQuery.isError && (
            <p className='text-destructive py-8 text-center text-sm'>
              {detailQuery.error.message}
            </p>
          )}
          {event && (
            <div className='space-y-4 pt-1'>
              <div className='flex flex-wrap gap-1.5'>
                <Badge variant={getDecisionBadgeVariant(event.decision)}>
                  {t(event.decision || 'pending')}
                </Badge>
                <Badge
                  variant={
                    event.status === 'failed' ? 'destructive' : 'outline'
                  }
                >
                  {t(event.status)}
                </Badge>
                <Badge variant='outline'>
                  {event.inspection_type === 'wordlist'
                    ? t('Wordlist')
                    : t('Model audit')}
                </Badge>
              </div>

              <DetailSection label={t('Prompt')}>
                <div className='mb-2 flex items-center justify-between gap-3'>
                  <span className='text-muted-foreground text-xs font-medium'>
                    {showsFullPrompt ? t('Full prompt') : t('Redacted preview')}
                  </span>
                  <div className='flex items-center gap-1.5'>
                    {event.full_prompt_truncated && (
                      <Badge variant='warning'>
                        {t('Retained copy truncated')}
                      </Badge>
                    )}
                    {prompt && (
                      <CopyButton
                        value={prompt}
                        variant='ghost'
                        size='sm'
                        tooltip={t('Copy')}
                      />
                    )}
                  </div>
                </div>
                <pre className='bg-background/70 max-h-[26rem] overflow-auto rounded-md p-3 text-xs leading-relaxed break-words whitespace-pre-wrap [content-visibility:auto]'>
                  {prompt || t('No prompt text retained')}
                </pre>
                {!canViewFullPrompt && (
                  <p className='text-muted-foreground mt-2 text-xs'>
                    {t('Your permission only allows the redacted preview.')}
                  </p>
                )}
              </DetailSection>

              <DetailSection label={t('Context')}>
                <DetailRow
                  label={t('User ID')}
                  value={String(event.user_id)}
                  mono
                />
                <DetailRow
                  label={t('Username')}
                  value={event.username || '—'}
                />
                <DetailRow
                  label={t('Token')}
                  value={event.token_name || String(event.token_id || '—')}
                />
                <DetailRow label={t('Group')} value={event.group || '—'} />
                <DetailRow
                  label={t('Protocol')}
                  value={getPromptAuditProtocolName(event.protocol) || '—'}
                />
                <DetailRow
                  label={t('Audit stage')}
                  value={
                    event.direction === 'output'
                      ? t('Generated output')
                      : t('Request input')
                  }
                />
                <DetailRow
                  label={t('Delivery status')}
                  value={event.delivery_status || '—'}
                  mono
                />
                <DetailRow
                  label={t('Coverage')}
                  value={
                    event.coverage_complete ? t('Complete') : t('Incomplete')
                  }
                />
                <DetailRow label={t('Model')} value={event.model || '—'} mono />
                <DetailRow
                  label={t('Request ID')}
                  value={event.request_id || '—'}
                  mono
                />
                <DetailRow
                  label={t('Created at')}
                  value={dayjs
                    .unix(event.created_at)
                    .format('YYYY-MM-DD HH:mm:ss')}
                  mono
                />
              </DetailSection>

              <CollapsibleDetailSection label={t('Client')}>
                <DetailRow label={t('IP')} value={event.ip || '—'} mono />
                <DetailRow
                  label={t('User Agent')}
                  value={event.user_agent || '—'}
                  mono
                />
                <DetailRow
                  label={t('Method')}
                  value={event.method || '—'}
                  mono
                />
                <DetailRow
                  label={t('Request path')}
                  value={event.request_path || '—'}
                  mono
                />
                <DetailRow
                  label={t('Origin')}
                  value={event.origin || '—'}
                  mono
                />
                <DetailRow
                  label={t('Referer')}
                  value={event.referer || '—'}
                  mono
                />
              </CollapsibleDetailSection>

              <DetailSection label={t('Result')}>
                <DetailRow
                  label={t('Text source')}
                  value={
                    event.matched_scope
                      ? promptAuditScopeLabel(event.matched_scope, t)
                      : (event.inspected_scopes ?? [])
                          .map((scope) => promptAuditScopeLabel(scope, t))
                          .join(' · ') || '—'
                  }
                />
                <DetailRow label={t('Safety')} value={event.safety || '—'} />
                <DetailRow
                  label={t('Actual action')}
                  value={event.action || '—'}
                  mono
                />
                <DetailRow
                  label={t('Suggested action')}
                  value={event.would_action || '—'}
                  mono
                />
                <DetailRow
                  label={t('Refusal')}
                  value={event.refusal || '—'}
                  mono
                />
                <DetailRow
                  label={t('Audit model')}
                  value={event.endpoint_model || '—'}
                  mono
                />
                {/* The node's configured id, kept next to the model it runs:
                    two nodes can share a model and only the id tells them apart. */}
                <DetailRow
                  label={t('Endpoint')}
                  value={event.endpoint_id || '—'}
                  mono
                />
                <DetailRow
                  label={t('Latency')}
                  value={`${event.latency_ms} ms`}
                  mono
                />
                <div className='flex flex-wrap gap-1.5 pt-1'>
                  {event.categories.length === 0 &&
                    event.unknown_categories.length === 0 && (
                      <span className='text-muted-foreground text-xs'>
                        {t('No categories')}
                      </span>
                    )}
                  {event.categories.map((category) => (
                    <Badge key={category} variant='outline'>
                      {t(category)}
                    </Badge>
                  ))}
                  {event.unknown_categories.map((categoryHash) => (
                    <Badge key={categoryHash} variant='warning'>
                      {t('Unknown')}: {categoryHash}
                    </Badge>
                  ))}
                </div>
                {event.review_status && (
                  <div className='mt-3 rounded-md border p-3 text-xs'>
                    <p className='font-medium'>{t('Gray-area review')}</p>
                    <p className='text-muted-foreground mt-1'>
                      {event.review_status} · {event.review_decision || '—'} ·{' '}
                      {event.reviewer_endpoint_id || '—'}
                    </p>
                    {event.review_reason && (
                      <p className='mt-2'>{event.review_reason}</p>
                    )}
                  </div>
                )}
              </DetailSection>

              {canManage &&
                (event.status === 'done' || event.status === 'failed') && (
                  <DetailSection label={t('Administrator review')}>
                    <Textarea
                      value={reviewReason}
                      maxLength={512}
                      placeholder={t('Optional review reason')}
                      onChange={(change) =>
                        setReviewReason(change.target.value)
                      }
                    />
                    <div className='mt-2 flex flex-wrap gap-2'>
                      <Button
                        size='sm'
                        variant='outline'
                        disabled={reviewMutation.isPending}
                        onClick={() => reviewMutation.mutate('false_positive')}
                      >
                        {t('Mark false positive')}
                      </Button>
                      <Button
                        size='sm'
                        variant='destructive'
                        disabled={reviewMutation.isPending}
                        onClick={() =>
                          reviewMutation.mutate('confirmed_violation')
                        }
                      >
                        {t('Confirm violation')}
                      </Button>
                    </div>
                    {event.human_review && (
                      <p className='text-muted-foreground mt-2 text-xs'>
                        {t('Current review')}: {event.human_review}
                        {event.human_review_reason
                          ? ` · ${event.human_review_reason}`
                          : ''}
                      </p>
                    )}
                  </DetailSection>
                )}

              <CollapsibleDetailSection label={t('Technical details')}>
                <DetailRow
                  label={t('Prompt hash')}
                  value={event.prompt_hash}
                  mono
                />
                <DetailRow
                  label={t('Mode')}
                  value={event.execution_mode}
                  mono
                />
                <DetailRow
                  label={t('Config version')}
                  value={event.config_version || '—'}
                  mono
                />
                {event.inspection_type === 'wordlist' && (
                  <>
                    <DetailRow
                      label={t('Wordlist')}
                      value={
                        event.wordlist_id === 'manual'
                          ? t('Custom wordlist')
                          : event.wordlist_name || '—'
                      }
                    />
                    <DetailRow
                      label={t('Wordlist version')}
                      value={event.wordlist_version?.slice(0, 12) || '—'}
                      mono
                    />
                  </>
                )}
                <DetailRow
                  label={t('Attempts')}
                  value={`${event.attempts}/${event.max_attempts}`}
                  mono
                />
                <DetailRow
                  label={t('Prompt length')}
                  value={String(event.prompt_length)}
                  mono
                />
                <DetailRow
                  label={t('Segments')}
                  value={String(event.segment_count)}
                  mono
                />
                <DetailRow
                  label={t('Chunks')}
                  value={String(event.chunk_count)}
                  mono
                />
                <DetailRow
                  label={t('Completed at')}
                  value={
                    event.completed_at
                      ? dayjs
                          .unix(event.completed_at)
                          .format('YYYY-MM-DD HH:mm:ss')
                      : '—'
                  }
                  mono
                />
                <DetailRow
                  label={t('Error code')}
                  value={event.error_code || '—'}
                  mono
                />
              </CollapsibleDetailSection>

              {event.request_id && hasRelatedLogs && (
                <div className='flex flex-wrap gap-2'>
                  <a
                    href={`/usage-logs/common?requestId=${encodeURIComponent(event.request_id)}`}
                    className={cn(
                      buttonVariants({ variant: 'outline', size: 'sm' })
                    )}
                  >
                    {t('Related usage logs')}
                    <ExternalLink />
                  </a>
                  <a
                    href={`/usage-logs/common?type=5&requestId=${encodeURIComponent(event.request_id)}`}
                    className={cn(
                      buttonVariants({ variant: 'outline', size: 'sm' })
                    )}
                  >
                    {t('Related error logs')}
                    <ExternalLink />
                  </a>
                </div>
              )}
            </div>
          )}
        </div>

        {event && (canManage || canDelete) && (
          <SheetFooter className='flex-row justify-end border-t'>
            {canManage && event.status === 'failed' && (
              <Button
                variant='outline'
                disabled={
                  retryMutation.isPending ||
                  event.full_prompt_truncated ||
                  !event.full_prompt_available
                }
                onClick={() => retryMutation.mutate(event.id)}
              >
                <RefreshCw />
                {retryMutation.isPending ? t('Retrying...') : t('Retry audit')}
              </Button>
            )}
            {canDelete &&
              (event.status === 'done' || event.status === 'failed') && (
                <Button variant='destructive' onClick={() => onDelete(event)}>
                  <Trash2 />
                  {t('Delete')}
                </Button>
              )}
          </SheetFooter>
        )}
      </SheetContent>
    </Sheet>
  )
}
