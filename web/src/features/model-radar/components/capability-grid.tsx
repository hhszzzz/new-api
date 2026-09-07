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
import { GridViewIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import {
  getIqTone,
  groupConfigurations,
  IQ_TEXT_CLASSES,
  matrixEfforts,
  type ModelRadarIconRegistry,
} from '../lib/model-radar'
import type { ModelRadarConfiguration, ModelRadarHistoryFrame } from '../types'
import { ConfigurationDetails } from './configuration-details'
import { ModelBadge } from './model-badge'

export function CapabilityGrid(props: {
  configurations: ModelRadarConfiguration[]
  history: ModelRadarHistoryFrame[]
  iconRegistry?: ModelRadarIconRegistry
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<{
    model: string
    effort: string
  } | null>(null)
  const groups = groupConfigurations(props.configurations)
  const efforts = matrixEfforts(props.configurations)
  const selectedConfiguration = selected
    ? (props.configurations.find(
        (item) =>
          item.model === selected.model && item.effort === selected.effort
      ) ?? null)
    : null

  if (selected && !selectedConfiguration) setSelected(null)

  return (
    <section
      aria-labelledby='capability-grid-title'
      className='border-border/70 mt-2 border-t pt-4'
    >
      <header className='mb-2 flex items-center gap-2'>
        <HugeiconsIcon
          icon={GridViewIcon}
          className='text-primary size-4'
          strokeWidth={2}
          aria-hidden='true'
        />
        <h2 id='capability-grid-title' className='text-base font-semibold'>
          {t('Model tiers')}
        </h2>
      </header>
      <div
        className='bg-background text-muted-foreground sticky top-16 z-10 mb-1 hidden grid-cols-[repeat(var(--effort-count),minmax(0,1fr))] gap-1.5 py-1 text-center text-xs font-medium md:grid lg:pl-48'
        style={{ '--effort-count': efforts.length } as CSSProperties}
        aria-hidden='true'
      >
        {efforts.map((effort) => (
          <span key={effort} className='capitalize'>
            {effort}
          </span>
        ))}
      </div>
      <div className='space-y-2 lg:space-y-1'>
        {groups.map((group) => {
          const bestIq = Math.max(
            ...group.configurations.map((item) => item.iq)
          )
          return (
            <section
              key={group.model}
              aria-label={group.model}
              className='min-w-0 lg:grid lg:grid-cols-[11rem_minmax(0,1fr)] lg:items-center lg:gap-4'
            >
              <header className='mb-1.5 flex items-center gap-2 lg:mb-0'>
                <ModelBadge
                  color={group.color}
                  model={group.model}
                  iconRegistry={props.iconRegistry}
                />
                <h3
                  className='min-w-0 text-sm font-semibold break-words lg:truncate'
                  title={group.model}
                >
                  {group.model}
                </h3>
              </header>
              <div
                className='grid grid-cols-2 gap-1.5 md:grid-cols-[repeat(var(--effort-count),minmax(0,1fr))]'
                style={{ '--effort-count': efforts.length } as CSSProperties}
              >
                {efforts.map((effort) => {
                  const configuration = group.configurations.find(
                    (item) => item.effort.trim().toLowerCase() === effort
                  )
                  if (!configuration) {
                    return (
                      <div
                        key={effort}
                        aria-hidden='true'
                        className='hidden md:block'
                      />
                    )
                  }
                  return (
                    <TierCard
                      key={effort}
                      configuration={configuration}
                      isBest={configuration.iq === bestIq}
                      onSelect={() =>
                        setSelected({
                          model: configuration.model,
                          effort: configuration.effort,
                        })
                      }
                    />
                  )
                })}
              </div>
            </section>
          )
        })}
      </div>
      <ConfigurationDetails
        configuration={selectedConfiguration}
        history={props.history}
        open={selectedConfiguration !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </section>
  )
}

function TierCard(props: {
  configuration: ModelRadarConfiguration
  isBest: boolean
  onSelect: () => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const configuration = props.configuration
  return (
    <button
      type='button'
      className={cn(
        'bg-card hover:bg-muted/40 focus-visible:ring-ring flex min-h-11 min-w-0 flex-col justify-center gap-0.5 rounded-md border px-1.5 py-1 text-center transition-colors outline-none focus-visible:ring-2 focus-visible:ring-offset-2',
        props.isBest && 'ring-primary/40 ring-1'
      )}
      aria-label={t('View details for {{model}} {{effort}}', {
        model: configuration.model,
        effort: configuration.effort,
      })}
      onClick={props.onSelect}
    >
      <span className='text-muted-foreground text-[10px] leading-none font-medium break-all capitalize md:hidden'>
        {configuration.effort}
      </span>
      <span
        className={cn(
          'text-base leading-5 font-semibold tabular-nums',
          IQ_TEXT_CLASSES[getIqTone(configuration.iq)]
        )}
      >
        {configuration.iq.toFixed(1)}
      </span>
      <span className='text-muted-foreground flex flex-wrap items-center justify-center gap-x-1 text-[10px] leading-[14px] tabular-nums'>
        <span title={t('Passed / valid samples')}>
          {format.integer(configuration.passed)}/
          {format.integer(configuration.valid_tasks)}
        </span>
        <span className='whitespace-nowrap' title={t('Average duration')}>
          {configuration.average_minutes == null ? (
            '—'
          ) : (
            <>
              {format.decimal(configuration.average_minutes)} {t('min')}
            </>
          )}
        </span>
      </span>
    </button>
  )
}
