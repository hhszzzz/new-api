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
import { CrownIcon, GridViewIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import { useRadarFormatters } from '../hooks/use-radar-formatters'
import {
  getConfigurationStation,
  getIqTone,
  getStationLabel,
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
  showStation?: boolean
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
      className='border-border/70 mt-2 border-t pt-6'
    >
      <header className='mb-5 flex items-center gap-2'>
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
      <div className='space-y-4'>
        {groups.map((group) => {
          const bestIq = Math.max(
            ...group.configurations.map((item) => item.iq)
          )
          const stations = [
            ...new Set(
              group.configurations.map(getConfigurationStation).filter(Boolean)
            ),
          ]
          return (
            <section
              key={group.model}
              aria-label={group.model}
              className='min-w-0'
            >
              <header className='mb-2 flex flex-wrap items-center gap-2'>
                <ModelBadge
                  color={group.color}
                  model={group.model}
                  iconRegistry={props.iconRegistry}
                />
                <h3 className='min-w-0 text-sm font-semibold break-all'>
                  {group.model}
                </h3>
                {props.showStation ? (
                  <div className='ml-auto flex flex-wrap gap-1'>
                    {stations.map((station) => (
                      <Badge key={station} variant='outline'>
                        {getStationLabel(station)}
                      </Badge>
                    ))}
                  </div>
                ) : null}
              </header>
              <div
                className='grid grid-cols-2 gap-2 md:grid-cols-[repeat(var(--effort-count),minmax(0,1fr))]'
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
        'bg-card hover:bg-muted/40 focus-visible:ring-ring grid min-h-24 min-w-0 grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)] overflow-hidden rounded-lg border text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-offset-2',
        props.isBest && 'ring-primary/40 ring-1'
      )}
      aria-label={t('View details for {{model}} {{effort}}', {
        model: configuration.model,
        effort: configuration.effort,
      })}
      onClick={props.onSelect}
    >
      <span className='flex min-w-0 flex-col justify-between gap-1.5 px-2.5 py-2'>
        <span className='flex flex-wrap items-center justify-between gap-1'>
          <span className='flex min-w-0 items-center gap-1 text-xs font-medium'>
            <span className='break-all capitalize'>{configuration.effort}</span>
            {props.isBest ? (
              <HugeiconsIcon
                icon={CrownIcon}
                className='text-primary size-3 shrink-0'
                strokeWidth={2}
                aria-hidden='true'
              />
            ) : null}
          </span>
          {configuration.runs_24h != null ? (
            <span
              className='bg-muted text-muted-foreground rounded px-1 py-0.5 text-[9px] tabular-nums'
              aria-label={t('{{count}} runs in 24h', {
                count: configuration.runs_24h,
              })}
              title={t('{{count}} runs in 24h', {
                count: configuration.runs_24h,
              })}
            >
              {format.integer(configuration.runs_24h)}
            </span>
          ) : null}
        </span>
        <span
          className={cn(
            'block text-center text-3xl leading-tight font-semibold tracking-tight tabular-nums',
            IQ_TEXT_CLASSES[getIqTone(configuration.iq)]
          )}
        >
          {configuration.iq.toFixed(1)}
        </span>
        <span
          className='text-muted-foreground block text-center text-[10px] tabular-nums'
          title={t('Passed / valid samples')}
        >
          {format.integer(configuration.passed)}/
          {format.integer(configuration.valid_tasks)}
        </span>
      </span>
      <span className='grid min-w-0 grid-rows-2 border-l text-center text-xs font-medium tabular-nums'>
        <span
          className='flex min-w-0 items-center justify-center px-1 py-2 break-all'
          title={t('Average cost')}
        >
          {format.usdShort(configuration.average_price_usd) ?? '—'}
        </span>
        <span
          className='text-muted-foreground flex min-w-0 items-center justify-center border-t px-1 py-2 text-[11px]'
          title={t('Average duration')}
        >
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
