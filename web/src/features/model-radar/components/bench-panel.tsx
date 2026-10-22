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
import type { TFunction } from 'i18next'
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
import { benchStatusLabelKey } from '../lib/bench'
import {
  benchEffortCandidates,
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
  ModelRadarBench,
  ModelRadarBenchCell,
  ModelRadarBenchSourceScore,
  ModelRadarSettings,
} from '../types'
import { BenchCellDetails } from './bench-cell-details'
import { ModelBadge } from './model-badge'

/** Per-model auto-effort controls the bench renders next to each model name. */
export type BenchAutoEffortState = {
  setting: RadarAutoEffortSetting
  isSaving: boolean
  /** Models the signed-in user may call. */
  userModels: string[]
  onToggle: (model: string, enabled: boolean) => void
}

const BENCH_DIMENSIONS = [
  { key: 'total', label: 'Total' },
  { key: 'deepswe', label: 'Software engineering' },
  { key: 'pompeii', label: 'Visual-spatial reasoning' },
  { key: 'tb4', label: 'Terminal & systems' },
  { key: 'science', label: 'Scientific research' },
] as const

type BenchDimension = (typeof BENCH_DIMENSIONS)[number]['key']

const BENCH_EFFORT_ORDER = [
  'ultra',
  'max',
  'xhigh',
  'high',
  'medium',
  'low',
] as const

// The dashboard titles the model names as GPT-6.1 Sol / GPT-5.6 Luna.
function formatBenchModelName(model: string): string {
  return model
    .replace(/^gpt-/i, 'GPT-')
    .replaceAll(/-([a-z0-9])/g, (_, char: string) => ` ${char.toUpperCase()}`)
}

function benchScoreTone(score: number): keyof typeof IQ_TEXT_CLASSES {
  return getIqTone((score / 150) * 100)
}

// One row of the grid is one model; efforts the binding does not grade for that
// model stay hidden so every column keeps its place.
function benchModelRows(bench: ModelRadarBench) {
  const models: string[] = []
  const byModel = new Map<string, ModelRadarBenchCell[]>()
  for (const cell of bench.cells) {
    const cells = byModel.get(cell.model)
    if (cells) {
      cells.push(cell)
    } else {
      byModel.set(cell.model, [cell])
      models.push(cell.model)
    }
  }
  for (const cells of byModel.values()) {
    cells.sort(
      (left, right) =>
        BENCH_EFFORT_ORDER.indexOf(
          left.effort as (typeof BENCH_EFFORT_ORDER)[number]
        ) -
        BENCH_EFFORT_ORDER.indexOf(
          right.effort as (typeof BENCH_EFFORT_ORDER)[number]
        )
    )
  }
  return models.map((model, index) => ({
    model,
    cells: byModel.get(model) ?? [],
    color: MODEL_COLORS[index % MODEL_COLORS.length],
  }))
}

type BenchDimensionScore = {
  score: number | null
  coverage: number
  requiredTasks: number
  status: string
}

function benchDimensionScore(
  cell: ModelRadarBenchCell,
  dimension: BenchDimension
): BenchDimensionScore {
  if (dimension === 'total') {
    return {
      score: cell.score,
      coverage: cell.coverage,
      requiredTasks: cell.required_tasks,
      status: cell.score_status,
    }
  }
  const row: ModelRadarBenchSourceScore | undefined =
    cell.source_scores[dimension]
  if (!row) {
    return { score: null, coverage: 0, requiredTasks: 0, status: 'missing' }
  }
  let status = 'provisional'
  if (row.coverage === 0) {
    status = 'missing_current_result'
  } else if (row.coverage === row.required_tasks) {
    status = 'complete'
  }
  return {
    score: row.score,
    coverage: row.coverage,
    requiredTasks: row.required_tasks,
    status,
  }
}

// Mirrors the gateway's per-model auto-effort state so the bench can show the
// switch and preview which tier the next request would use.
function resolveRowAutoEffort(
  row: { model: string; cells: ModelRadarBenchCell[] },
  settings: ModelRadarSettings | undefined,
  autoEffort: BenchAutoEffortState | undefined
) {
  if (!autoEffort) return null
  const pick = pickAutoEffort(
    benchEffortCandidates(row.cells),
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

export function BenchPanel(props: {
  bench: ModelRadarBench
  settings?: ModelRadarSettings
  iconRegistry?: ModelRadarIconRegistry
  autoEffort?: BenchAutoEffortState
}) {
  const { t } = useTranslation()
  const [dimension, setDimension] = useState<BenchDimension>('total')
  const [detail, setDetail] = useState<{
    cell: ModelRadarBenchCell
    displayName: string
  } | null>(null)
  const rows = useMemo(() => benchModelRows(props.bench), [props.bench])
  const bench = props.bench
  const partial = bench.loaded_count < bench.total_count
  const failedCells = useMemo(
    () => new Set(bench.failed_cells),
    [bench.failed_cells]
  )

  return (
    <section aria-label={t('RadarBench score')} className='mt-5'>
      <div
        role='group'
        aria-label={t('Score dimension')}
        className='flex flex-wrap gap-1.5'
      >
        {BENCH_DIMENSIONS.map((item) => (
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
        className='bg-background text-muted-foreground sticky top-16 z-10 mt-3 mb-1 hidden grid-cols-[repeat(var(--bench-effort-count),minmax(0,1fr))] gap-1.5 py-1 text-center text-xs font-medium md:grid lg:pl-[16rem]'
        style={
          { '--bench-effort-count': BENCH_EFFORT_ORDER.length } as CSSProperties
        }
        aria-hidden='true'
      >
        {BENCH_EFFORT_ORDER.map((effort) => (
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
              ? formatBenchModelName(row.model)
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
                benchDimensionScore(cell, dimension).score ??
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
                className='grid grid-cols-2 gap-1.5 md:grid-cols-[repeat(var(--bench-effort-count),minmax(0,1fr))]'
                style={
                  {
                    '--bench-effort-count': BENCH_EFFORT_ORDER.length,
                  } as CSSProperties
                }
              >
                {BENCH_EFFORT_ORDER.map((effort) => {
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
                  const view = benchDimensionScore(cell, dimension)
                  const autoSelected =
                    activeEffort !== null &&
                    cell.effort.trim().toLowerCase() === activeEffort
                  const best =
                    activeEffort !== null
                      ? autoSelected
                      : view.score !== null && view.score === rowBestScore
                  return (
                    <BenchCell
                      key={effort}
                      cell={cell}
                      dimension={dimension}
                      carried={failedCells.has(`${cell.model}|${cell.effort}`)}
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
        {partial ? (
          <span>
            {t(
              'This round read {{loaded}} of {{total}} configurations; failed tiers keep their previous values.',
              { loaded: bench.loaded_count, total: bench.total_count }
            )}
          </span>
        ) : null}
      </footer>

      <BenchCellDetails
        cell={detail?.cell ?? null}
        displayName={detail?.displayName ?? ''}
        open={detail !== null}
        onOpenChange={(open) => {
          if (!open) setDetail(null)
        }}
      />
    </section>
  )
}

function BenchCell(props: {
  cell: ModelRadarBenchCell
  dimension: BenchDimension
  /** A failed tier keeps the previous round's values. */
  carried: boolean
  /** Highest score of the row in the selected dimension. */
  best: boolean
  /** Tier the gateway would select for this model right now. */
  autoSelected: boolean
  onOpen: () => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const cell = props.cell
  const view = benchDimensionScore(cell, props.dimension)
  const carried = props.carried && cell.read_at > 0
  const detail = [
    `${t('Covered tasks / required tasks')}: ${format.integer(view.coverage)}/${format.integer(view.requiredTasks)}`,
    cell.graded_runs_count == null
      ? null
      : t('{{count}} graded runs', {
          count: format.integer(cell.graded_runs_count),
        }),
    cell.last_graded_day > 0
      ? t('Latest graded day {{date}}', {
          date: format.utcDay(cell.last_graded_day),
        })
      : null,
    benchMetricTitle(t, format, t('Mean API equivalent'), cell.price, false),
    benchMetricTitle(t, format, t('Mean duration'), cell.duration, true),
    cell.statistics_state === 'invalid_source_statistics'
      ? t('Invalid source statistics')
      : null,
    carried
      ? t('Keeping the previous read from {{time}}', {
          time: format.dateTime(cell.read_at),
        })
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
        carried && 'border-dashed',
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
            : IQ_TEXT_CLASSES[benchScoreTone(view.score)]
        )}
      >
        {view.score == null ? '—' : view.score.toFixed(1)}
      </span>
      <span className='text-muted-foreground mx-auto flex w-full max-w-32 flex-wrap items-center justify-between gap-x-1 text-[10px] leading-[14px] tabular-nums'>
        <span>
          {view.requiredTasks > 0
            ? `${format.integer(view.coverage)}/${format.integer(view.requiredTasks)}`
            : '—'}
        </span>
        <span className='whitespace-nowrap'>
          {t(benchStatusLabelKey(view.status))}
        </span>
      </span>
      {props.dimension === 'total' ? (
        <span className='text-muted-foreground mx-auto flex w-full max-w-32 flex-wrap items-center justify-between gap-x-1 text-[10px] leading-[14px] tabular-nums'>
          <span>
            {cell.price.mean == null ? '—' : format.usd(cell.price.mean)}
          </span>
          <span className='whitespace-nowrap'>
            {cell.duration.mean == null ? (
              '—'
            ) : (
              <>
                {format.decimal(cell.duration.mean)} {t('min')}
              </>
            )}
          </span>
        </span>
      ) : null}
    </button>
  )
}

function benchMetricTitle(
  t: TFunction,
  format: ReturnType<typeof useRadarFormatters>,
  label: string,
  stat: {
    mean: number | null
    known_samples: number | null
    total_samples: number | null
  },
  isDuration: boolean
): string {
  if (stat.known_samples == null || stat.total_samples == null) {
    return `${label}: ${t('Not provided by this source')}`
  }
  const known = t('Known samples {{known}}/{{total}}', {
    known: format.integer(stat.known_samples),
    total: format.integer(stat.total_samples),
  })
  let observed = t('No known values')
  if (stat.mean != null) {
    observed = isDuration
      ? `${format.decimal(stat.mean)} ${t('min')}`
      : (format.usd(stat.mean) ?? '')
  }
  const parts = [`${label}: ${observed}`, known]
  if (stat.mean != null && stat.known_samples < stat.total_samples) {
    parts.push(t('Partial sample mean'))
  }
  if (!isDuration && stat.mean != null) {
    parts.push(t('Not a subscription charge'))
  }
  return parts.join(' · ')
}
