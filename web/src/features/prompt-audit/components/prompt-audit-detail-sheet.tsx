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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import { cn } from '@/lib/utils'

import { getPromptAudit, retryPromptAudit } from '../api'
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

  const event = detailQuery.data
  const showsFullPrompt = canViewFullPrompt && event?.full_prompt !== undefined
  const prompt = showsFullPrompt
    ? (event?.full_prompt ?? '')
    : (event?.redacted_preview ?? '')

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
            <div className='space-y-5'>
              <div className='grid grid-cols-2 gap-x-4 gap-y-3 text-sm sm:grid-cols-3'>
                {[
                  [t('Status'), event.status],
                  [t('Decision'), event.decision || '—'],
                  [t('Safety'), event.safety || '—'],
                  [t('Mode'), event.execution_mode],
                  [t('Protocol'), event.protocol],
                  [t('Model'), event.model || '—'],
                  [t('User ID'), String(event.user_id)],
                  [
                    t('Token'),
                    event.token_name || String(event.token_id || '—'),
                  ],
                  [t('Group'), event.group || '—'],
                  [t('Audit node'), event.endpoint_id || '—'],
                  [t('Latency'), `${event.latency_ms} ms`],
                  [t('Attempts'), `${event.attempts}/${event.max_attempts}`],
                  [t('Prompt length'), String(event.prompt_length)],
                  [t('Segments'), String(event.segment_count)],
                  [t('Chunks'), String(event.chunk_count)],
                  [
                    t('Created at'),
                    dayjs.unix(event.created_at).format('YYYY-MM-DD HH:mm:ss'),
                  ],
                  [
                    t('Completed at'),
                    event.completed_at
                      ? dayjs
                          .unix(event.completed_at)
                          .format('YYYY-MM-DD HH:mm:ss')
                      : '—',
                  ],
                  [t('Error code'), event.error_code || '—'],
                ].map(([label, value]) => (
                  <div key={label} className='min-w-0'>
                    <p className='text-muted-foreground text-xs'>{label}</p>
                    <p className='mt-1 break-all'>{value}</p>
                  </div>
                ))}
              </div>

              <div>
                <p className='mb-2 text-sm font-medium'>{t('Categories')}</p>
                <div className='flex flex-wrap gap-1.5'>
                  {event.categories.length === 0 &&
                    event.unknown_categories.length === 0 && (
                      <span className='text-muted-foreground text-sm'>—</span>
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
              </div>

              <div>
                <div className='mb-2 flex items-center justify-between gap-3'>
                  <p className='text-sm font-medium'>
                    {showsFullPrompt ? t('Full prompt') : t('Redacted preview')}
                  </p>
                  {event.full_prompt_truncated && (
                    <Badge variant='warning'>
                      {t('Retained copy truncated')}
                    </Badge>
                  )}
                </div>
                <pre className='bg-muted/50 max-h-96 overflow-auto rounded-lg border p-3 text-xs break-words whitespace-pre-wrap [content-visibility:auto]'>
                  {prompt || t('No prompt text retained')}
                </pre>
                {!canViewFullPrompt && (
                  <p className='text-muted-foreground mt-2 text-xs'>
                    {t('Your permission only allows the redacted preview.')}
                  </p>
                )}
              </div>

              <div>
                <p className='text-muted-foreground mb-1 text-xs'>
                  {t('Prompt hash')}
                </p>
                <code className='block text-xs break-all'>
                  {event.prompt_hash}
                </code>
              </div>

              {event.request_id && (
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
