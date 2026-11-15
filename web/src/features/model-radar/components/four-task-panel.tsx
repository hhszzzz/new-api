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
import { Tick02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { RadarAutoEffortSetting } from '@/features/profile/types'
import { cn } from '@/lib/utils'

import { isModelAutoEffortEnabled } from '../hooks/use-radar-auto-effort'
import { useRadarFormatters } from '../hooks/use-radar-formatters'
import {
  FOUR_TASK_DIMENSIONS,
  fourTaskComponentStatusLabelKey,
  fourTaskStatusLabelKey,
  type FourTaskDimension,
} from '../lib/four-task'
import {
  fourTaskEffortCandidates,
  getIqTone,
  IQ_TEXT_CLASSES,
  isRadarAutoEffortAllowed,
  matchRadarModelToUserModels,
  MODEL_COLORS,
  pickAutoEffort,
  resolveRadarModel,
  type ModelRadarIconRegistry,
} from '../lib/model-radar'
import type {
  ModelRadarFourTask,
  ModelRadarFourTaskCell,
  ModelRadarFourTaskComponent,
  ModelRadarSettings,
} from '../types'
import { FourTaskCellDetails } from './four-task-cell-details'
import { ModelBadge } from './model-badge'

/** Per-model auto-effort controls the board renders next to each model name. */
export type FourTaskAutoEffortState = {
  setting: RadarAutoEffortSetting
  isSaving: boolean
  /** Models the signed-in user may call. */
  userModels: string[]
  onToggle: (model: string, enabled: boolean) => void
}

const FOUR_TASK_EFFORT_ORDER = [
  'ultra',
  'max',
  'xhigh',
  'high',
  'medium',
  'low',
] as const

// The station titles the model names as GPT-6.1 Sol / GPT-5.6 Luna.
function formatModelName(model: string): string {
  return model
    .replace(/^gpt-/i, 'GPT-')
    .replaceAll(/-([a-z0-9])/g, (_, char: string) => ` ${char.toUpperCase()}`)
}

function fourTaskScoreTone(
  score: number,
  dimension: FourTaskDimension
): keyof typeof IQ_TEXT_CLASSES {
  return getIqTone(dimension === 'total' ? score / 1.5 : score)
}

// One row of the grid is one model; efforts the round does not grade for that
// model stay hidden so every column keeps its place.
function fourTaskModelRows(board: ModelRadarFourTask) {
  const byModel = new Map<string, ModelRadarFourTaskCell[]>()
  for (const cell of board.cells) {
    const cells = byModel.get(cell.model)
    if (cells) {
      cells.push(cell)
    } else {
      byModel.set(cell.model, [cell])
    }
  }
  for (const cells of byModel.values()) {
    cells.sort(
      (left, right) =>
        FOUR_TASK_EFFORT_ORDER.indexOf(
          left.effort as (typeof FOUR_TASK_EFFORT_ORDER)[number]
        ) -
        FOUR_TASK_EFFORT_ORDER.indexOf(
          right.effort as (typeof FOUR_TASK_EFFORT_ORDER)[number]
        )
    )
  }
  const models =
    board.model_order.length > 0
      ? board.model_order.filter((model) => byModel.has(model))
      : [...byModel.keys()]
  return models.map((model, index) => ({
    model,
    displayName: board.model_names[model] ?? formatModelName(model),
    cells: byModel.get(model) ?? [],
    color: MODEL_COLORS[index % MODEL_COLORS.length],
  }))
}

type FourTaskDimensionScore = {
  score: number | null
  graded: number
  required: number
  status: string
  rank: number | null
}

function fourTaskDimensionScore(
  cell: ModelRadarFourTaskCell,
  dimension: FourTaskDimension
): FourTaskDimensionScore {
  if (dimension === 'total') {
    return {
      score: cell.score,
      graded: cell.graded_tasks,
      required: cell.required_tasks,
      status: fourTaskStatusLabelKey(cell),
      rank: cell.rank,
    }
  }
  const component: ModelRadarFourTaskComponent | undefined =
    cell.components[dimension]
  if (!component) {
    return {
      score: null,
      graded: 0,
      required: 1,
      status: 'Missing',
      rank: null,
    }
  }
  return {
    score: component.score,
    graded: component.score === null ? 0 : 1,
    required: 1,
    status: fourTaskComponentStatusLabelKey(component.score),
    rank: null,
  }
}

// Mirrors the gateway's per-model auto-effort state so the board can show the
// switch and preview which tier the next request would use.
function resolveRowAutoEffort(
  row: { model: string; cells: ModelRadarFourTaskCell[] },
  settings: ModelRadarSettings | undefined,
  autoEffort: FourTaskAutoEffortState | undefined
) {
  if (!autoEffort) return null
  const pick = pickAutoEffort(
    fourTaskEffortCandidates(row.cells),
    autoEffort.setting.policy ?? 'highest_iq',
    autoEffort.setting.min_iq_delta ?? 5
  )
  return {
    enabled: isModelAutoEffortEnabled(autoEffort.setting, row.model),
    // The switch is only offered when the administrator allowed the model and
    // the signed-in user can actually call it; anything else is hidden.
    available:
      isRadarAutoEffortAllowed(settings, row.model) &&
      matchRadarModelToUserModels(
        row.model,
        settings?.models[row.model]?.aliases,
        autoEffort.userModels
      ),
    targetEffort: pick?.effort ?? null,
    isSaving: autoEffort.isSaving,
    onToggle: (enabled: boolean) => autoEffort.onToggle(row.model, enabled),
  }
}

export function FourTaskPanel(props: {
  board: ModelRadarFourTask
  settings?: ModelRadarSettings
  iconRegistry?: ModelRadarIconRegistry
  autoEffort?: FourTaskAutoEffortState
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const [dimension, setDimension] = useState<FourTaskDimension>('total')
  const [detail, setDetail] = useState<{
    cell: ModelRadarFourTaskCell
    displayName: string
  } | null>(null)
  const rows = useMemo(() => fourTaskModelRows(props.board), [props.board])
  const board = props.board

  return (
    <section aria-label={t('Four-task score')} className='mt-5'>
      <div
        role='group'
        aria-label={t('Score dimension')}
        className='flex flex-wrap gap-1.5'
      >
        {FOUR_TASK_DIMENSIONS.map((item) => (
          <button
            key={item.key}
            type='button'
            aria-pressed={dimension === item.key}
            onClick={() => setDimension(item.key)}
            className={cn(
              'focus-visible:ring-ring rounded-md border px-2.5 py-1 text-xs font-medium transition-colors outline-none focus-visible:ring-2',
              dimension === item.key
                ? 'bg-primary text-primary-foreground border-primary'
                : 'bg-background hover:bg-muted/50'
            )}
          >
            {t(item.label)}
          </button>
        ))}
      </div>

      <div
        className='bg-background text-muted-foreground sticky top-16 z-10 mt-3 mb-1 hidden grid-cols-[repeat(var(--four-task-effort-count),minmax(0,1fr))] gap-1.5 py-1 text-center text-xs font-medium md:grid lg:pl-[16rem]'
        style={
          {
            '--four-task-effort-count': FOUR_TASK_EFFORT_ORDER.length,
          } as CSSProperties
        }
        aria-hidden='true'
      >
        {FOUR_TASK_EFFORT_ORDER.map((effort) => (
          <span key={effort} className='capitalize'>
            {effort}
          </span>
        ))}
      </div>

      <div className='space-y-2 pt-1'>
        {rows.map((row) => {
          const resolved = resolveRadarModel(
            row.model,
            props.settings,
            props.iconRegistry
          )
          const displayName =
            resolved.displayName === row.model
              ? row.displayName
              : resolved.displayName
          const auto = resolveRowAutoEffort(
            row,
            props.settings,
            props.autoEffort
          )
          const activeEffort =
            auto?.enabled && auto.available ? auto.targetEffort : null
          const rowBestScore = Math.max(
            ...row.cells.map(
              (cell) =>
                fourTaskDimensionScore(cell, dimension).score ??
                Number.NEGATIVE_INFINITY
            )
          )
          return (
            <section
              key={row.model}
              aria-label={displayName}
              className='min-w-0 lg:grid lg:grid-cols-[15rem_minmax(0,1fr)] lg:items-center lg:gap-4'
            >
              <header className='mb-1.5 flex items-center gap-2 lg:mb-0'>
                <span className='shrink-0'>
                  <ModelBadge
                    color={row.color}
                    model={row.model}
                    settings={props.settings}
                    iconRegistry={props.iconRegistry}
                  />
                </span>
                <h3
                  className='min-w-0 flex-1 truncate text-sm font-semibold'
                  title={row.model}
                >
                  {displayName}
                </h3>
                {auto?.available ? (
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Switch
                            className='ml-auto'
                            checked={auto.enabled}
                            disabled={auto.isSaving}
                            onCheckedChange={auto.onToggle}
                            aria-label={t(
                              'Automatically choose the reasoning tier for {{model}}',
                              { model: displayName }
                            )}
                          />
                        }
                      />
                      <TooltipContent side='top'>
                        {t(
                          auto.enabled
                            ? 'Disable automatic reasoning tier'
                            : 'Enable automatic reasoning tier'
                        )}
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                ) : null}
              </header>
              <div
                className='grid grid-cols-2 gap-1.5 md:grid-cols-[repeat(var(--four-task-effort-count),minmax(0,1fr))]'
                style={
                  {
                    '--four-task-effort-count': FOUR_TASK_EFFORT_ORDER.length,
                  } as CSSProperties
                }
              >
                {FOUR_TASK_EFFORT_ORDER.map((effort) => {
                  const cell = row.cells.find(
                    (item) => item.effort.trim().toLowerCase() === effort
                  )
                  if (!cell) {
                    return (
                      <div
                        key={effort}
                        aria-hidden='true'
                        className='hidden md:block'
                      />
                    )
                  }
                  const view = fourTaskDimensionScore(cell, dimension)
                  const autoSelected =
                    activeEffort !== null &&
                    cell.effort.trim().toLowerCase() === activeEffort
                  const best =
                    activeEffort !== null
                      ? autoSelected
                      : view.score !== null && view.score === rowBestScore
                  return (
                    <FourTaskCell
                      key={effort}
                      cell={cell}
                      dimension={dimension}
                      testedAt={board.test_completed_at}
                      best={best}
                      autoSelected={autoSelected}
                      onOpen={() => setDetail({ cell, displayName })}
                    />
                  )
                })}
              </div>
            </section>
          )
        })}
      </div>

      <footer className='text-muted-foreground mt-3 flex flex-col gap-0.5 text-xs'>
        <span>
          {t(
            '{{graded}}/{{expected}} graded · {{totals}} totals · {{ungraded}} missing',
            {
              graded: format.integer(board.graded),
              expected: format.integer(board.expected),
              totals: format.integer(board.valid_totals),
              ungraded: format.integer(board.ungraded),
            }
          )}
        </span>
        <span>
          {t('Tested {{time}}', {
            time: format.dateTime(board.test_completed_at),
          })}
        </span>
      </footer>

      <FourTaskCellDetails
        cell={detail?.cell ?? null}
        board={board}
        displayName={detail?.displayName ?? ''}
        open={detail !== null}
        onOpenChange={(open) => {
          if (!open) setDetail(null)
        }}
      />
    </section>
  )
}

function FourTaskCell(props: {
  cell: ModelRadarFourTaskCell
  dimension: FourTaskDimension
  testedAt: number
  /** Highest score of the row in the selected dimension. */
  best: boolean
  /** Tier the gateway would select for this model right now. */
  autoSelected: boolean
  onOpen: () => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const cell = props.cell
  const view = fourTaskDimensionScore(cell, props.dimension)
  const missingComponents = FOUR_TASK_DIMENSIONS.slice(1)
    .map((item) => cell.components[item.key])
    .filter((component) => component && component.score === null)
  const detail = [
    t('Graded tasks {{graded}}/{{required}}', {
      graded: format.integer(view.graded),
      required: format.integer(view.required),
    }),
    view.rank === null ? null : t('Rank #{{rank}}', { rank: view.rank }),
    `${t('Cost (USD)')}: ${format.usd(cell.cost_usd) ?? '—'}`,
    `${t('Runtime (min)')}: ${format.decimal(cell.duration_minutes) ?? '—'}`,
    t('Tested {{time}}', { time: format.dateTime(props.testedAt) }),
    missingComponents.length > 0
      ? `${t('Missing')}: ${missingComponents
          .map((component) => component?.missing_reason)
          .filter(Boolean)
          .join(', ')}`
      : null,
  ]
    .filter(Boolean)
    .join(' · ')

  return (
    <button
      type='button'
      onClick={props.onOpen}
      aria-haspopup='dialog'
      aria-label={t('View details for {{model}} {{effort}}', {
        model: cell.model,
        effort: cell.effort,
      })}
      className={cn(
        'bg-card relative flex min-h-11 min-w-0 flex-col justify-center gap-0.5 rounded-md border px-1.5 py-1 text-center transition-colors hover:bg-muted/40 focus-visible:ring-ring focus-visible:ring-2 focus-visible:outline-none',
        props.best && 'ring-primary/40 ring-1',
        props.autoSelected && 'border-primary/60 bg-primary/5'
      )}
      title={detail}
    >
      {props.autoSelected ? (
        <>
          <span className='sr-only'>{t('Current')}</span>
          <span
            aria-hidden='true'
            className='bg-primary text-primary-foreground absolute top-1 right-1 flex size-3.5 items-center justify-center rounded-full'
          >
            <HugeiconsIcon
              icon={Tick02Icon}
              strokeWidth={3}
              className='size-2.5'
            />
          </span>
        </>
      ) : null}
      <span className='text-muted-foreground text-[10px] leading-none font-medium break-all capitalize md:hidden'>
        {cell.effort}
      </span>
      <span
        className={cn(
          'text-base leading-5 font-semibold tabular-nums',
          view.score == null
            ? 'text-muted-foreground'
            : IQ_TEXT_CLASSES[fourTaskScoreTone(view.score, props.dimension)]
        )}
      >
        {view.score == null ? '—' : format.decimal(view.score)}
      </span>
      <span className='text-muted-foreground mx-auto flex w-full max-w-32 flex-wrap items-center justify-between gap-x-1 text-[10px] leading-[14px] tabular-nums'>
        <span>
          {view.required > 0
            ? `${format.integer(view.graded)}/${format.integer(view.required)}`
            : '—'}
        </span>
        <span className='whitespace-nowrap'>{t(view.status)}</span>
      </span>
      {props.dimension === 'total' ? (
        <span className='text-muted-foreground mx-auto flex w-full max-w-32 flex-wrap items-center justify-between gap-x-1 text-[10px] leading-[14px] tabular-nums'>
          <span>{format.usd(cell.cost_usd) ?? '—'}</span>
          <span className='whitespace-nowrap'>
            {cell.duration_minutes == null ? (
              '—'
            ) : (
              <>
                {format.decimal(cell.duration_minutes)} {t('min')}
              </>
            )}
          </span>
        </span>
      ) : null}
    </button>
  )
}
