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
import {
  Cancel01Icon,
  GridViewIcon,
  InformationCircleIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import { groupConfigurations } from '../lib/model-radar'
import type { ModelRadarConfiguration } from '../types'

export function CapabilityMatrix(props: {
  configurations: ModelRadarConfiguration[]
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const [selected, setSelected] = useState<ModelRadarConfiguration | null>(null)
  const groups = groupConfigurations(props.configurations)

  return (
    <section
      aria-labelledby='capability-matrix-title'
      className='border-border/70 border-t py-6'
    >
      <header className='mb-4'>
        <div className='flex items-center gap-2'>
          <HugeiconsIcon
            icon={GridViewIcon}
            className='text-primary size-4'
            strokeWidth={2}
            aria-hidden='true'
          />
          <h2 id='capability-matrix-title' className='text-base font-semibold'>
            {t('Capability matrix')}
          </h2>
        </div>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('Compare IQ across every model and reasoning effort.')}
        </p>
      </header>

      <div className='flex flex-col gap-5'>
        {groups.map((group) => (
          <article key={group.model} aria-label={group.model}>
            <div className='mb-2 flex min-w-0 items-center gap-2'>
              <span
                className='size-2.5 shrink-0 rounded-full'
                style={{ backgroundColor: group.color }}
                aria-hidden='true'
              />
              <h3 className='truncate text-sm font-semibold'>{group.model}</h3>
              <span className='text-muted-foreground text-xs tabular-nums'>
                {t('{{count}} configurations', {
                  count: group.configurations.length,
                })}
              </span>
            </div>
            <div className='grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6'>
              {group.configurations.map((configuration) => (
                <button
                  key={`${configuration.model}:${configuration.effort}`}
                  type='button'
                  className='bg-card hover:bg-accent/40 focus-visible:ring-ring min-h-24 min-w-0 rounded-lg border p-3 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-offset-2'
                  style={{ borderTopColor: group.color, borderTopWidth: 2 }}
                  aria-label={t('View details for {{model}} {{effort}}', {
                    model: configuration.model,
                    effort: configuration.effort,
                  })}
                  onClick={() => setSelected(configuration)}
                >
                  <span className='text-muted-foreground block truncate text-[11px] font-medium uppercase'>
                    {configuration.effort}
                  </span>
                  <span className='mt-1.5 block text-xl font-semibold tabular-nums'>
                    {format.decimal(configuration.iq)}
                    <span className='text-muted-foreground ml-1 text-[10px] font-normal'>
                      IQ
                    </span>
                  </span>
                  <span className='text-muted-foreground mt-1 block text-[11px] tabular-nums'>
                    {t('{{passed}} / {{total}} passed', {
                      passed: format.integer(configuration.passed),
                      total: format.integer(configuration.valid_tasks),
                    })}
                  </span>
                </button>
              ))}
            </div>
          </article>
        ))}
      </div>

      <ConfigurationDetails
        configuration={selected}
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </section>
  )
}

function ConfigurationDetails(props: {
  configuration: ModelRadarConfiguration | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const configuration = props.configuration
  if (!configuration) return null

  const unavailable = t('Not available')
  const metrics = [
    [t('IQ'), format.decimal(configuration.iq)],
    [
      t('Passed / valid tasks'),
      `${format.integer(configuration.passed)} / ${format.integer(configuration.valid_tasks)}`,
    ],
    [t('Total runs'), format.integer(configuration.total_runs)],
    [t('Average cost'), format.usd(configuration.average_price_usd)],
    [t('Cost samples'), format.integer(configuration.price_samples)],
    [t('Average duration'), format.decimal(configuration.average_minutes)],
    [t('Duration samples'), format.integer(configuration.duration_samples)],
    [
      t('Average Agent steps'),
      format.decimal(configuration.average_agent_steps),
    ],
    [
      t('Agent step samples'),
      format.integer(configuration.agent_steps_samples),
    ],
    [t('Average tokens'), format.integer(configuration.average_total_tokens)],
    [t('Token samples'), format.integer(configuration.token_samples)],
    [t('Cache hit rate'), format.percent(configuration.cache_hit_rate)],
    [t('Cache samples'), format.integer(configuration.cache_token_samples)],
    [
      t('Combined cost index'),
      format.decimal(configuration.combined_cost_index),
    ],
    [
      t('Incomplete cost samples'),
      format.integer(configuration.incomplete_cost_samples),
    ],
    [t('Latest graded'), format.dateTime(configuration.latest_graded_at)],
  ]

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className='max-h-[min(88vh,760px)] overflow-y-auto sm:max-w-2xl'
      >
        <DialogHeader className='pr-10'>
          <DialogTitle className='break-words'>
            {configuration.model}{' '}
            <span className='text-muted-foreground ml-2 text-sm font-normal uppercase'>
              {configuration.effort}
            </span>
          </DialogTitle>
          <DialogDescription>
            {t(
              'Complete capability and efficiency metrics for this configuration.'
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogClose
          render={
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              className='absolute top-2 right-2'
              aria-label={t('Close')}
            />
          }
        >
          <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} />
        </DialogClose>

        <dl className='grid grid-cols-1 overflow-hidden rounded-lg border sm:grid-cols-2'>
          {metrics.map(([label, value]) => (
            <div
              key={label}
              className='border-border/70 flex items-center justify-between gap-4 border-b px-3 py-2.5 last:border-b-0 sm:odd:border-r'
            >
              <dt className='text-muted-foreground text-xs'>{label}</dt>
              <dd className='text-right text-sm font-medium tabular-nums'>
                {value ?? unavailable}
              </dd>
            </div>
          ))}
        </dl>

        <div className='bg-muted/40 flex items-start gap-2 rounded-lg p-3'>
          <HugeiconsIcon
            icon={InformationCircleIcon}
            className='text-muted-foreground mt-0.5 size-4 shrink-0'
            strokeWidth={2}
            aria-hidden='true'
          />
          <p className='text-muted-foreground text-xs leading-relaxed'>
            {t(
              'IQ is the latest valid pass rate per task multiplied by 150. The combined cost index is provided by the source after normalizing weighted price and duration to 100.'
            )}
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
