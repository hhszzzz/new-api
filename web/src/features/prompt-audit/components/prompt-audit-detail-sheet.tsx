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
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  CollapsibleDetailSection,
  DetailRow,
  DetailSection,
} from '@/features/usage-logs/components/dialogs/log-detail-layout'
import { cn } from '@/lib/utils'

import { getPromptAudit, retryPromptAudit, reviewPromptAudit } from '../api'
import {
  getPromptAuditProtocolName,
  promptAuditDetectorLabel,
  promptAuditOutcome,
  promptAuditPayloadSources,
  promptAuditRequestKindLabel,
  promptAuditScoreLabel,
  promptAuditScoreRows,
} from '../lib'
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
  const scoreRows = promptAuditScoreRows(event?.scores)
  const outcome = promptAuditOutcome({
    status: event?.status ?? '',
    decision: event?.decision ?? '',
  })
  const showsFullPrompt = canViewFullPrompt && event?.full_prompt !== undefined
  const prompt = showsFullPrompt
    ? (event?.full_prompt ?? '')
    : (event?.redacted_preview ?? '')
  const payloadSources = canViewFullPrompt
    ? promptAuditPayloadSources(event?.scan_payload)
    : []
  // Which inspected source the tabs show. Held with the record it was chosen for,
  // so a key from a previous record cannot survive into the next one and leave it
  // showing no tab: the record ids simply stop matching.
  const [chosenSource, setChosenSource] = useState<{
    eventID: number | null
    key: string
  } | null>(null)
  const activeSource =
    (chosenSource?.eventID === eventID
      ? payloadSources.find((source) => source.key === chosenSource.key)
      : undefined) ?? payloadSources[0]
  const setActiveKey = (value: unknown) =>
    setChosenSource(typeof value === 'string' ? { eventID, key: value } : null)
  const PromptCopySection = showsFullPrompt
    ? CollapsibleDetailSection
    : DetailSection
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
                {/* The same single reading the records row shows, so opening a
                    record does not restate its verdict in a second vocabulary. */}
                <Badge variant={outcome.variant}>{t(outcome.key)}</Badge>
                <Badge variant='outline'>
                  {promptAuditDetectorLabel(event.inspection_type, t)}
                </Badge>
                <Badge variant='outline'>
                  {promptAuditRequestKindLabel(event.request_kind, t)}
                </Badge>
              </div>

              {/* The parts the audit submitted, one tab per source. The tabs
                  carry what the removed scope list said — which parts were
                  inspected — by being those parts, and the chosen tab's text
                  sits directly under them instead of inside a second frame,
                  which read as a card within a card. */}
              {canViewFullPrompt && (
                <DetailSection label={t('Inspected content')}>
                  {payloadSources.length === 0 ? (
                    <span className='text-muted-foreground text-xs'>
                      {t('No inspected content retained')}
                    </span>
                  ) : (
                    <Tabs value={activeSource.key} onValueChange={setActiveKey}>
                      <div className='flex items-center gap-2'>
                        {/* One row of tabs sized to their own names: the list is
                            short and its labels are fixed, so tabs stretched
                            across the sheet only added space, while letting them
                            wrap made the strip a line taller for a record that
                            carried one more source. A long list scrolls
                            sideways, and the padding keeps the active tab's
                            underline inside the scrolling box. */}
                        <div className='min-w-0 flex-1 overflow-x-auto pb-1'>
                          <TabsList
                            variant='line'
                            className='min-w-max justify-start'
                          >
                            {payloadSources.map((source) => (
                              <TabsTrigger key={source.key} value={source.key}>
                                {source.scope
                                  ? promptAuditScopeLabel(source.scope, t)
                                  : t('Unknown source')}
                              </TabsTrigger>
                            ))}
                          </TabsList>
                        </div>
                        <CopyButton
                          value={activeSource.blocks.join('\n\n')}
                          variant='ghost'
                          size='sm'
                          tooltip={t('Copy')}
                        />
                      </div>
                      {payloadSources.map((source) => (
                        <TabsContent key={source.key} value={source.key}>
                          {/* One frame per source, one block inside it per part
                              the client sent: a turn that arrived as several
                              messages reads as the blocks it was made of, told
                              apart by a hairline, rather than as one run of text
                              where the boundary is lost. */}
                          <div className='bg-background/70 mt-1 max-h-56 overflow-auto rounded-md p-2 [content-visibility:auto]'>
                            {Array.from(
                              source.blocks.entries(),
                              ([position, text]) => (
                                <div key={`${source.key}-${position}`}>
                                  {position > 0 && (
                                    <Separator className='my-2' />
                                  )}
                                  <pre className='text-xs leading-relaxed break-words whitespace-pre-wrap'>
                                    {text}
                                  </pre>
                                </div>
                              )
                            )}
                          </div>
                        </TabsContent>
                      ))}
                    </Tabs>
                  )}
                  {event.scan_payload_truncated && (
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Inspected content was truncated to the retention limit.'
                      )}
                    </p>
                  )}
                </DetailSection>
              )}

              <PromptCopySection
                label={showsFullPrompt ? t('Full prompt') : t('Prompt')}
              >
                <div className='mb-2 flex items-center justify-between gap-3'>
                  {/* The heading already says "Full prompt" when the whole
                      request is shown, so the label is only repeated for the
                      redacted preview, whose heading is just "Prompt". */}
                  {!showsFullPrompt && (
                    <span className='text-muted-foreground text-xs font-medium'>
                      {t('Redacted preview')}
                    </span>
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
                <pre className='bg-background/70 max-h-[26rem] overflow-auto rounded-md p-3 text-xs leading-relaxed break-words whitespace-pre-wrap [content-visibility:auto]'>
                  {prompt || t('No prompt text retained')}
                </pre>
                {/* Kept off the header row: the truncation applies to the text
                    below, and a badge above it competed with the verdicts for
                    attention on nearly every record. The limit itself is a
                    setting and is not repeated here, because this sheet can be
                    opened by operators who may not read that configuration. */}
                {event.full_prompt_truncated && (
                  <p className='text-muted-foreground mt-2 text-xs'>
                    {t(
                      'Only the beginning of the request was stored; the rest was dropped by the retention limit.'
                    )}
                  </p>
                )}
                {showsFullPrompt && (
                  <p className='text-muted-foreground mt-2 text-xs'>
                    {t(
                      'This is the whole request as sent, while Inspected content lists only the parts the audit submitted.'
                    )}
                  </p>
                )}
                {!canViewFullPrompt && (
                  <p className='text-muted-foreground mt-2 text-xs'>
                    {t('Your permission only allows the redacted preview.')}
                  </p>
                )}
              </PromptCopySection>

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
                  value={t(getPromptAuditProtocolName(event.protocol)) || '—'}
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
                {/* Only a TypeSafe node returns probabilities, so a label-based
                    verdict shows nothing here rather than an empty section. */}
                {scoreRows.length > 0 && (
                  <div className='pt-2'>
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
