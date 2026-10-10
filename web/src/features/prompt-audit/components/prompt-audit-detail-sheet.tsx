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
import { ExternalLink, RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
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
} from '@/features/usage-logs/components/dialogs/log-detail-layout'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  getPromptAudit,
  getPromptAuditImportedEvent,
  retryPromptAudit,
  reviewPromptAudit,
} from '../api'
import {
  getPromptAuditProtocolName,
  promptAuditDetectorLabel,
  promptAuditOutcome,
  promptAuditRequestKindLabel,
  promptAuditScoreLabel,
  promptAuditScoreRows,
  promptAuditBodyText,
  downloadPromptAuditBody,
} from '../lib'
import { promptAuditScopeLabel } from '../scopes'
import type { PromptAuditEvent } from '../types'
import { PromptAuditPayloadView } from './prompt-audit-payload-view'

type PromptAuditDetailSheetProps = {
  eventID: number | null
  canViewFullPrompt: boolean
  canManage: boolean
  canDelete: boolean
  onOpenChange: (open: boolean) => void
  onDelete: (event: PromptAuditEvent) => void
  sourceID?: string
  onViewRelated?: (id: number) => void
}

/** Reading groups inside the sheet; only evidence and identity need a frame. */
const sectionClassName = 'flex min-w-0 flex-col gap-3'
/** Two columns of label/value rows on wide sheets, one on a phone. */
const rowGridClassName = 'grid gap-2 sm:grid-cols-2 sm:gap-x-6'

export function PromptAuditDetailSheet({
  eventID,
  canViewFullPrompt,
  canManage,
  canDelete,
  onOpenChange,
  onDelete,
  sourceID,
  onViewRelated,
}: PromptAuditDetailSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [reviewReason, setReviewReason] = useState('')
  const detailQuery = useQuery({
    queryKey: [
      'prompt-audit',
      sourceID ? 'imported-event' : 'event',
      sourceID,
      eventID,
    ],
    enabled: eventID !== null,
    gcTime: 0,
    queryFn: async () => {
      const result = sourceID
        ? await getPromptAuditImportedEvent(sourceID, eventID ?? 0)
        : await getPromptAudit(eventID ?? 0)
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
  const scoreRows = promptAuditScoreRows(event?.scores)
  const outcome = promptAuditOutcome({
    status: event?.status ?? '',
    decision: event?.decision ?? '',
  })
  const stored = event?.status === 'stored'
  let bodyDescription = t(
    'This is the whole request as sent, while Inspected content lists only the parts the audit submitted.'
  )
  if (stored) {
    bodyDescription = t(
      'This is the retained full text, shown without source separation.'
    )
  }
  if (event?.direction === 'output') {
    bodyDescription = t(
      'The output body contains the complete model reply. Its request is available through the related request link.'
    )
  }
  const prompt = canViewFullPrompt
    ? promptAuditBodyText(event)
    : (event?.redacted_preview ?? '')
  // An empty inspection type is a legacy row written before the detectors were
  // split; it was a model audit.
  const modelAudit =
    !event?.inspection_type || event.inspection_type === 'model'
  // An input the audit rejected never reached upstream, and the audit
  // deliberately writes neither a consume log nor an error log for it, so both
  // related-log links would lead to an empty page. Generated output is judged
  // after upstream already produced and billed it, so its logs exist whatever
  // the verdict.
  const hasRelatedLogs =
    event?.direction === 'output' ||
    (event?.action !== 'block' && event?.action !== 'unavailable')

  return (
    <Sheet
      open={eventID !== null}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <SheetContent className='w-full gap-0 sm:max-w-2xl xl:max-w-3xl'>
        <SheetHeader className='shrink-0 gap-2 border-b p-4 pr-12 sm:px-6 sm:pr-14'>
          {/* The record id is not part of the title: it is an identifier the
              operator copies, so it lives with the other identifiers under the
              technical details instead of competing with the verdict. */}
          <SheetTitle>{t('Prompt audit details')}</SheetTitle>
          <div className='flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1'>
            {event && (
              <>
                <Badge variant={outcome.variant}>{t(outcome.key)}</Badge>
                <Badge variant='outline'>
                  {event.direction === 'output'
                    ? t('Model reply')
                    : promptAuditRequestKindLabel(event.request_kind, t)}
                </Badge>
              </>
            )}
            <SheetDescription className='min-w-0 text-xs [overflow-wrap:anywhere] break-words tabular-nums'>
              {event
                ? formatTimestampToDate(event.created_at)
                : t('Loading...')}
            </SheetDescription>
          </div>
        </SheetHeader>

        <div className='bg-muted/20 min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain'>
          {detailQuery.isLoading && (
            <LoadingState message={t('Loading audit details...')} />
          )}
          {detailQuery.isError && (
            <ErrorState
              description={detailQuery.error.message}
              onRetry={() => void detailQuery.refetch()}
            />
          )}
          {event && (
            <div className='flex min-w-0 flex-col gap-5 p-4 sm:p-6'>
              {/* What the audit concluded and why; the rest is evidence. */}
              <section aria-label={t('Result')} className={sectionClassName}>
                <h3 className='text-sm font-medium'>{t('Result')}</h3>
                <div className={rowGridClassName}>
                  {/* A stored record never reached a verdict: the outcome row
                      below already says so, and an empty action or latency
                      would read as an audit that never happened. */}
                  {!stored && (
                    <DetailRow
                      label={t('Actual action')}
                      value={event.action || '—'}
                      mono
                    />
                  )}
                  {!stored &&
                    event.would_action &&
                    event.would_action !== event.action && (
                      <DetailRow
                        label={t('Suggested action')}
                        value={event.would_action}
                        mono
                      />
                    )}
                  {!stored && event.safety && (
                    <DetailRow label={t('Safety')} value={event.safety} />
                  )}
                  {!stored && event.refusal && (
                    <DetailRow
                      label={t('Refusal')}
                      value={event.refusal}
                      mono
                    />
                  )}
                  {!stored && (
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
                  )}
                  {/* Which detector ran, named exactly once: a model audit is
                      already named by the model row below it, so only the other
                      detectors are called out here. */}
                  {!stored &&
                    (modelAudit ? (
                      <DetailRow
                        label={t('Audit model')}
                        value={event.endpoint_model || '—'}
                        mono
                      />
                    ) : (
                      <DetailRow
                        label={t('Inspection method')}
                        value={promptAuditDetectorLabel(
                          event.inspection_type,
                          t
                        )}
                      />
                    ))}
                  {/* Only an output has a delivery of its own; a request input
                      was delivered by the very fact that it was answered. */}
                  {event.direction === 'output' && (
                    <DetailRow
                      label={t('Delivery status')}
                      value={event.delivery_status || '—'}
                      mono
                    />
                  )}
                  {!stored && (
                    <DetailRow
                      label={t('Latency')}
                      value={`${event.latency_ms} ms`}
                      mono
                    />
                  )}
                  {/* Coverage earns a row only when it is not complete: an input
                      audit that continues a previous response, or an output
                      audit whose stream never finished, judged part of the
                      content. Saying "complete" otherwise is a tautology. */}
                  {!event.coverage_complete && (
                    <DetailRow label={t('Coverage')} value={t('Incomplete')} />
                  )}
                </div>
                {event.direction === 'output' &&
                event.related_input_id &&
                onViewRelated ? (
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => {
                      if (event.related_input_id) {
                        onViewRelated(event.related_input_id)
                      }
                    }}
                  >
                    {t('View related request')}
                  </Button>
                ) : null}
                {event.content_state === 'archived' ||
                event.content_state === 'missing' ? (
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Body unavailable locally. Import the corresponding day package to view it.'
                    )}
                  </p>
                ) : null}
                {event.legacy_content && (
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Historical mixed snapshot preserved. Only a reliably identified reply is displayed.'
                    )}
                  </p>
                )}
                {!stored &&
                  (event.categories.length > 0 ||
                    event.unknown_categories.length > 0) && (
                    <div className='flex flex-wrap gap-1.5'>
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
                  )}
                {/* Only a TypeSafe node returns probabilities, so a label-based
                    verdict shows nothing here rather than an empty section. */}
                {!stored && scoreRows.length > 0 && (
                  <div className='border-border/60 border-t pt-3'>
                    <p className='text-muted-foreground text-xs font-medium'>
                      {t('Audit model scores')}
                    </p>
                    <div className='mt-1 flex flex-col gap-1'>
                      {scoreRows.map(([scoreKey, score]) => (
                        <div
                          key={scoreKey}
                          className='flex items-center justify-between gap-3 text-xs'
                        >
                          <span className='truncate'>
                            {t(promptAuditScoreLabel(scoreKey))}
                          </span>
                          <span className='font-mono tabular-nums'>
                            {score.toFixed(2)}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                {!stored && event.review_status && (
                  <div className='border-border/60 border-t pt-3 text-xs'>
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
              </section>

              {/* The parts the audit submitted, framed as one card; the full
                  prompt sits inside the same frame when it can be shown. A
                  stored record submitted nothing: it is titled as the stored
                  snapshot it is, so the tabs are not read as inspected text. */}
              {canViewFullPrompt ? (
                <section
                  aria-label={
                    stored ? t('Content snapshot') : t('Inspected content')
                  }
                  className='bg-background min-w-0 overflow-hidden rounded-xl border'
                >
                  <h3 className='px-4 pt-4 pb-1 text-sm font-medium'>
                    {stored ? t('Content snapshot') : t('Inspected content')}
                  </h3>
                  <div className='min-w-0'>
                    <PromptAuditPayloadView
                      key={eventID}
                      payload={event.scan_payload}
                      truncated={event.scan_payload_truncated}
                      stored={stored}
                      preferOutput={event.direction === 'output'}
                    />
                    <div className='border-t px-3 py-1'>
                      <CollapsibleDetailSection
                        label={
                          event.direction === 'output'
                            ? t('Model reply')
                            : t('Full prompt')
                        }
                        variant='plain'
                        action={
                          prompt && (
                            <CopyButton
                              value={prompt}
                              variant='ghost'
                              size='sm'
                              tooltip={`${t('Copy')} · ${event.direction === 'output' ? t('Model reply') : t('Full prompt')}`}
                            />
                          )
                        }
                      >
                        <pre
                          tabIndex={0}
                          className='focus-visible:ring-ring/50 bg-muted/30 max-h-80 overflow-y-auto overscroll-contain rounded-md p-3 font-mono text-sm leading-relaxed [overflow-wrap:anywhere] break-words whitespace-pre-wrap outline-none focus-visible:ring-2 focus-visible:ring-inset'
                        >
                          {prompt || t('No prompt text retained')}
                        </pre>
                        {/* The truncation applies to the text below; the limit
                          itself is a setting and is not repeated here, because
                          this sheet can be opened by operators who may not read
                          that configuration. */}
                        {event.full_prompt_truncated && (
                          <p className='text-muted-foreground mt-2 text-xs'>
                            {t(
                              'Only the beginning of the request was stored; the rest was dropped by the retention limit.'
                            )}
                          </p>
                        )}
                        <p className='text-muted-foreground mt-2 text-xs'>
                          {bodyDescription}
                        </p>
                      </CollapsibleDetailSection>
                      {prompt && (
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => downloadPromptAuditBody(event)}
                        >
                          {t('Export body')}
                        </Button>
                      )}
                    </div>
                  </div>
                </section>
              ) : (
                <section aria-label={t('Prompt')} className={sectionClassName}>
                  <div className='flex items-center justify-between gap-3'>
                    <h3 className='text-sm font-medium'>{t('Prompt')}</h3>
                    {prompt && (
                      <CopyButton
                        value={prompt}
                        variant='ghost'
                        size='sm'
                        tooltip={t('Copy')}
                      />
                    )}
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t('Redacted preview')}
                  </p>
                  <pre
                    tabIndex={0}
                    className='focus-visible:ring-ring/50 bg-muted/30 max-h-80 overflow-y-auto overscroll-contain rounded-md border p-3 font-mono text-sm leading-relaxed [overflow-wrap:anywhere] break-words whitespace-pre-wrap outline-none focus-visible:ring-2 focus-visible:ring-inset'
                  >
                    {prompt || t('No prompt text retained')}
                  </pre>
                  {event.full_prompt_truncated && (
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Only the beginning of the request was stored; the rest was dropped by the retention limit.'
                      )}
                    </p>
                  )}
                  {!canViewFullPrompt && (
                    <p className='text-muted-foreground text-xs'>
                      {t('Your permission only allows the redacted preview.')}
                    </p>
                  )}
                </section>
              )}

              {/* Who sent it, framed as one card; the request context below
                  stays flat. */}
              <section
                aria-label={t('Caller')}
                className='bg-background min-w-0 overflow-hidden rounded-xl border'
              >
                <h3 className='border-b px-4 py-3 text-sm font-medium'>
                  {t('Caller')}
                </h3>
                <div className='grid min-w-0 gap-3 p-4 sm:grid-cols-2 sm:gap-x-6'>
                  <DetailRow
                    label={t('Username')}
                    value={event.username || '—'}
                  />
                  <DetailRow
                    label={t('User ID')}
                    value={String(event.user_id)}
                    mono
                  />
                  <DetailRow
                    label={t('Token')}
                    value={event.token_name || String(event.token_id || '—')}
                  />
                  <DetailRow label={t('Group')} value={event.group || '—'} />
                </div>
              </section>

              {/* The request the verdict is about; the caller card above covers
                  who sent it, the disclosures below hold the machine fields. */}
              <div className='bg-background min-w-0 overflow-hidden rounded-xl border'>
                <section
                  aria-label={t('Context')}
                  className={cn(sectionClassName, 'p-4')}
                >
                  <h3 className='text-sm font-medium'>{t('Context')}</h3>
                  {/* The requested model and the stage the verdict applies to:
                      everything else here is either transport metadata or a
                      property of the inspection, and lives further down. */}
                  <div className={rowGridClassName}>
                    {/* The requested model belongs with the rest of the request,
                        not beside the audit model it is easily mistaken for. */}
                    <DetailRow label={t('Model')} value={event.model || '—'} />
                    <DetailRow
                      label={stored ? t('Direction') : t('Audit stage')}
                      value={
                        event.direction === 'output'
                          ? t('Generated output')
                          : t('Request input')
                      }
                    />
                  </div>
                </section>

                <div className='border-t px-3 py-1'>
                  <CollapsibleDetailSection label={t('Client')} variant='plain'>
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
                </div>

                <div className='border-t px-3 py-1'>
                  <CollapsibleDetailSection
                    label={t('Technical details')}
                    variant='plain'
                  >
                    <DetailRow
                      label={t('Prompt hash')}
                      value={event.prompt_hash}
                      mono
                    />
                    {/* The identifiers this record and its logs are looked up by.
                        They are kept together here rather than in the header,
                        which repeats the record in a second vocabulary without
                        telling the reader what either id is. */}
                    <DetailRow
                      label={t('Record ID')}
                      value={
                        <span className='inline-flex min-w-0 items-center gap-1'>
                          <span className='min-w-0 break-all'>{event.id}</span>
                          <CopyButton
                            value={String(event.id)}
                            size='sm'
                            className='size-6'
                            tooltip={`${t('Copy')} · ${t('Record ID')}`}
                          />
                        </span>
                      }
                      mono
                    />
                    <DetailRow
                      label={t('Request ID')}
                      value={
                        <span className='inline-flex min-w-0 items-center gap-1'>
                          <span className='min-w-0 break-all'>
                            {event.request_id || '—'}
                          </span>
                          {event.request_id && (
                            <CopyButton
                              value={event.request_id}
                              size='sm'
                              className='size-6'
                              tooltip={`${t('Copy')} · ${t('Request ID')}`}
                            />
                          )}
                        </span>
                      }
                      mono
                    />
                    {/* Two audit nodes can share a model, so the configured node
                        is recorded next to its model rather than replaced by it. */}
                    <DetailRow
                      label={t('Endpoint')}
                      value={event.endpoint_id || '—'}
                      mono
                    />
                    {/* The wire protocol and the delivery outcome are transport
                        facts: they explain a failure, they do not change the
                        verdict, so they stay with the rest of the mechanics. */}
                    <DetailRow
                      label={t('Protocol')}
                      value={
                        t(getPromptAuditProtocolName(event.protocol)) || '—'
                      }
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
                    <DetailRow
                      label={t('Delivery status')}
                      value={event.delivery_status || '—'}
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
                      value={formatTimestampToDate(event.completed_at)}
                      mono
                    />
                    <DetailRow
                      label={t('Error code')}
                      value={event.error_code || '—'}
                      mono
                    />
                  </CollapsibleDetailSection>
                </div>
              </div>

              {canManage &&
                (event.status === 'done' || event.status === 'failed') && (
                  <section
                    aria-label={t('Administrator review')}
                    className={sectionClassName}
                  >
                    <h3 className='text-sm font-medium'>
                      {t('Administrator review')}
                    </h3>
                    <Textarea
                      value={reviewReason}
                      maxLength={512}
                      placeholder={t('Optional review reason')}
                      onChange={(change) =>
                        setReviewReason(change.target.value)
                      }
                    />
                    <div className='flex flex-wrap gap-2'>
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
                      <p className='text-muted-foreground text-xs'>
                        {t('Current review')}: {event.human_review}
                        {event.human_review_reason
                          ? ` · ${event.human_review_reason}`
                          : ''}
                      </p>
                    )}
                  </section>
                )}

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
                  Boolean(
                    event.content_state && event.content_state !== 'hot'
                  ) ||
                  event.scan_payload_truncated ||
                  (!event.scan_payload &&
                    (event.full_prompt_truncated ||
                      !event.full_prompt_available))
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
