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
import type { ColumnDef } from '@tanstack/react-table'
import { ChevronDown, ChevronRight, Eye, Loader2 } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { TruncatedCell } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatTimestampToDate } from '@/lib/format'

import { getPromptAuditProtocolName, isMergedPromptAuditRow } from '../lib'
import type { PromptAuditEvent } from '../types'

function decisionBadgeVariant(decision: string) {
  if (decision === 'block' || decision === 'unavailable') return 'destructive'
  if (decision === 'flag') return 'warning'
  if (decision === 'pass') return 'secondary'
  return 'outline'
}

function statusBadgeVariant(status: string) {
  if (status === 'failed') return 'destructive'
  if (status === 'retry' || status === 'processing') return 'warning'
  if (status === 'done') return 'secondary'
  return 'outline'
}

/** The chevron of a merged row, spinning while its requests are being fetched. */
function expandIcon(loading: boolean, expanded: boolean) {
  if (loading) return <Loader2 className='animate-spin' />
  return expanded ? <ChevronDown /> : <ChevronRight />
}

/**
 * The repeat summary of a row the collapsed listing actually merged. A group of
 * one is just a request: expanding it would show the very row that is already
 * there, so it keeps reading like any other row.
 */
function mergedRepeat(event: PromptAuditEvent) {
  return isMergedPromptAuditRow(event) ? event.repeat : undefined
}

/**
 * A timestamp split into its day and its clock reading. A merged row names the
 * day once and then shows both ends of its span, so the two halves are needed
 * apart: a span that stayed within one day reads as a pair of clock readings,
 * and only a span that crossed midnight repeats the day it ended on, which a
 * bare clock reading would leave ambiguous.
 */
function timestampParts(timestamp?: number) {
  const [date = '-', time = ''] = formatTimestampToDate(timestamp).split(' ')
  return { date, time }
}

export function usePromptAuditColumns(options: {
  canDelete: boolean
  onOpen: (eventID: number) => void
  /** The listing merges requests that submitted the same text. */
  collapsed?: boolean
  /** Collapsed rows whose requests are still being fetched. */
  loadingGroupIDs?: Set<number>
}): ColumnDef<PromptAuditEvent>[] {
  const { t } = useTranslation()
  const { canDelete, onOpen } = options
  const collapsed = options.collapsed ?? false
  const loadingGroupIDs = options.loadingGroupIDs

  return useMemo(() => {
    const columns: ColumnDef<PromptAuditEvent>[] = []
    if (canDelete) {
      columns.push({
        id: 'select',
        size: 44,
        enableHiding: false,
        header: ({ table }) => (
          <Checkbox
            checked={table.getIsAllPageRowsSelected()}
            indeterminate={table.getIsSomePageRowsSelected()}
            onCheckedChange={(value) =>
              table.toggleAllPageRowsSelected(Boolean(value))
            }
            aria-label={t('Select current page')}
          />
        ),
        cell: ({ row }) =>
          // A group's child request is not a row the operator deletes as part of
          // a batch, so it carries no checkbox.
          row.getCanSelect() ? (
            <Checkbox
              checked={row.getIsSelected()}
              onCheckedChange={(value) => row.toggleSelected(Boolean(value))}
              aria-label={t('Select audit record')}
            />
          ) : null,
        meta: { mobileHidden: true },
      })
    }

    columns.push(
      {
        accessorKey: 'created_at',
        header: t('Time'),
        size: 150,
        cell: ({ row }) => {
          const event = row.original
          const repeat = mergedRepeat(event)
          const first = timestampParts(repeat?.first_at)
          const last = timestampParts(repeat?.last_at)
          return (
            <div className='font-mono text-xs tabular-nums'>
              {/* A request the group holds is drawn as one of a run: the row
                  anchors a rule that drops through this column, and the
                  request's own timestamp sits on it as a dot. Both marks stay
                  hidden until a table row is there to hold them (the records
                  table turns them on for the rows it nests), because a card has
                  no row for the rule to cross. */}
              {row.depth > 0 && (
                <span
                  aria-hidden='true'
                  className='prompt-audit-timeline bg-muted-foreground/50 absolute top-0 -bottom-px hidden w-px'
                >
                  <span className='bg-muted-foreground/60 absolute top-1/2 -left-[2.5px] size-1.5 -translate-y-1/2 rounded-full' />
                </span>
              )}
              <span className='prompt-audit-timeline-text flex items-center gap-1.5'>
                <span>
                  {repeat
                    ? first.date
                    : formatTimestampToDate(event.created_at)}
                </span>
              </span>
              {/* A merged row stands for requests spread over time: one day, and
                  both ends of the span ruled apart on the line below. */}
              {repeat && (
                <span className='flex items-center gap-1.5'>
                  <span>{first.time}</span>
                  <span
                    aria-hidden='true'
                    className='bg-muted-foreground/45 h-px w-3'
                  />
                  <span>
                    {last.date === first.date
                      ? last.time
                      : `${last.date} ${last.time}`}
                  </span>
                </span>
              )}
            </div>
          )
        },
        meta: { label: t('Time'), mobileOrder: 3 },
      },
      {
        id: 'result',
        header: t('Result'),
        size: 190,
        cell: ({ row }) => {
          const event = row.original
          const repeat = mergedRepeat(event)
          const isExpanded = row.getIsExpanded()
          // A merged row stands for several requests the audit node read as the
          // same text, so the count says what the whole group decided — a group
          // can hold a block, an unavailable retry, and an allow.
          const repeatSummary = repeat
            ? t('{{count}} requests submitted the same text', {
                count: repeat.count,
              })
            : ''
          const isGroupLoading = loadingGroupIDs?.has(event.id) === true
          return (
            <div className='min-w-0'>
              <div className='flex flex-wrap items-center gap-1'>
                {repeat && (
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <Badge
                          variant='outline'
                          className='cursor-help tabular-nums'
                          role='img'
                          aria-label={repeatSummary}
                          tabIndex={0}
                        >
                          ×{repeat.count}
                        </Badge>
                      }
                    />
                    <TooltipContent>
                      {/* The tooltip lays its children out in a row, so the text
                          and the table of values are wrapped in one block: given
                          to the flex row separately they were squeezed until the
                          timestamps broke in half. */}
                      <div className='space-y-1.5'>
                        <p>{repeatSummary}</p>
                        <dl className='grid grid-cols-[auto_auto] justify-start gap-x-3 gap-y-0.5'>
                          <dt className='text-muted-foreground'>
                            {t('Start')}
                          </dt>
                          <dd className='font-mono whitespace-nowrap tabular-nums'>
                            {formatTimestampToDate(repeat.first_at)}
                          </dd>
                          <dt className='text-muted-foreground'>{t('End')}</dt>
                          <dd className='font-mono whitespace-nowrap tabular-nums'>
                            {formatTimestampToDate(repeat.last_at)}
                          </dd>
                          {repeat.blocks > 0 && (
                            <>
                              <dt className='text-muted-foreground'>
                                {t('Blocked')}
                              </dt>
                              <dd className='tabular-nums'>{repeat.blocks}</dd>
                            </>
                          )}
                          {repeat.unavailable > 0 && (
                            <>
                              <dt className='text-muted-foreground'>
                                {t('Unavailable')}
                              </dt>
                              <dd className='tabular-nums'>
                                {repeat.unavailable}
                              </dd>
                            </>
                          )}
                        </dl>
                      </div>
                    </TooltipContent>
                  </Tooltip>
                )}
                <Badge variant={decisionBadgeVariant(event.decision)}>
                  {t(event.decision || 'pending')}
                </Badge>
                <Badge variant={statusBadgeVariant(event.status)}>
                  {t(event.status)}
                </Badge>
                <Badge variant='outline'>
                  {event.inspection_type === 'wordlist'
                    ? t('Wordlist')
                    : t('Model audit')}
                </Badge>
                <Badge variant='outline'>
                  {event.direction === 'output'
                    ? t('Generated output')
                    : t('Request input')}
                </Badge>
                {/* Only a merged row holds a group, so only it can be
                    expanded; the count above says how many requests that
                    reveals. */}
                {collapsed && row.depth === 0 && repeat && (
                  <Button
                    variant={isExpanded ? 'secondary' : 'outline'}
                    size='icon-xs'
                    onClick={row.getToggleExpandedHandler()}
                    aria-label={isExpanded ? t('Collapse') : t('Expand')}
                    aria-expanded={isExpanded}
                  >
                    {expandIcon(isGroupLoading, isExpanded)}
                  </Button>
                )}
              </div>
              {event.categories.length > 0 && (
                <p className='text-muted-foreground mt-1 max-w-48 truncate text-xs'>
                  {event.categories.map((category) => t(category)).join(', ')}
                </p>
              )}
            </div>
          )
        },
        meta: { label: t('Result'), mobileBadge: true },
      },
      {
        id: 'identity',
        header: t('Identity'),
        size: 190,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='min-w-0'>
              <TruncatedCell className='max-w-52 font-medium'>
                {event.username || `#${event.user_id}`}
              </TruncatedCell>
              <TruncatedCell className='text-muted-foreground max-w-52 text-xs'>
                {t('Group')}: {event.group || '—'}
              </TruncatedCell>
              <TruncatedCell className='text-muted-foreground max-w-52 text-xs'>
                {t('Key')}: {event.token_name || `#${event.token_id}`}
              </TruncatedCell>
            </div>
          )
        },
        meta: { label: t('Identity'), mobileOrder: 1 },
      },
      {
        id: 'request',
        header: t('Request'),
        size: 230,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='min-w-0'>
              <TruncatedCell className='max-w-56 font-medium'>
                {event.model || '—'}
              </TruncatedCell>
              <TruncatedCell className='text-muted-foreground max-w-56 text-xs'>
                {t('Protocol')}: {getPromptAuditProtocolName(event.protocol)}
              </TruncatedCell>
              <TruncatedCell className='text-muted-foreground max-w-56 text-xs'>
                {t('Audit model')}: {event.endpoint_model || '—'}
              </TruncatedCell>
            </div>
          )
        },
        meta: { label: t('Request'), mobileTitle: true },
      },
      {
        id: 'client',
        header: t('Client'),
        size: 200,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='min-w-0'>
              <span className='font-mono text-xs'>{event.ip || '—'}</span>
              <TruncatedCell className='text-muted-foreground max-w-52 text-xs'>
                {event.user_agent || '—'}
              </TruncatedCell>
            </div>
          )
        },
        // Hideable: the reply headers are useful for investigation, but not for
        // every operator scanning the list.
        meta: { label: t('Client'), mobileHidden: true },
      },
      {
        id: 'prompt',
        header: t('Prompt'),
        size: 340,
        cell: ({ row }) => {
          const event = row.original
          return (
            <div className='max-w-96 min-w-48'>
              <p className='line-clamp-2 break-words whitespace-normal'>
                {event.redacted_preview || '—'}
              </p>
            </div>
          )
        },
        meta: { label: t('Prompt'), mobileOrder: 2 },
      },
      {
        id: 'actions',
        header: t('Details'),
        size: 72,
        enableHiding: false,
        cell: ({ row }) => (
          <div className='text-right'>
            <Button
              variant='ghost'
              size='icon-sm'
              aria-label={t('View details')}
              onClick={() => onOpen(row.original.id)}
            >
              <Eye />
            </Button>
          </div>
        ),
        meta: { label: t('Details'), mobileOrder: 4 },
      }
    )

    return columns
  }, [canDelete, collapsed, loadingGroupIDs, onOpen, t])
}
