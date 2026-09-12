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
import { GridViewIcon, Tick02Icon } from '@hugeicons/core-free-icons'
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
import type {
  RadarAutoEffortPolicy,
  RadarAutoEffortSetting,
} from '@/features/profile/types'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { isModelAutoEffortEnabled } from '../hooks/use-radar-auto-effort'
import { useRadarFormatters } from '../hooks/use-radar-formatters'
import {
  gatewayEffortCandidates,
  getIqTone,
  groupConfigurations,
  IQ_TEXT_CLASSES,
  isAutoEffortExcludedRadarTier,
  isRadarAutoEffortAllowed,
  matchRadarModelToUserModels,
  matrixEfforts,
  listVendors,
  OTHER_VENDOR,
  pickAutoEffort,
  type ModelRadarGroup,
  type ModelRadarIconRegistry,
} from '../lib/model-radar'
import type {
  ModelRadarConfiguration,
  ModelRadarHistoryFrame,
  ModelRadarSettings,
} from '../types'
import { ConfigurationDetails } from './configuration-details'
import { ModelBadge } from './model-badge'

/** Per-model auto-effort state the grid renders next to each model name. */
export type ModelAutoEffortState = {
  enabled: boolean
  /** False when the user cannot call the model or the radar does not cover it. */
  available: boolean
  /** Tier the next request would be switched to, or null when unknown. */
  targetEffort: string | null
  targetIQ: number | null
  isSaving: boolean
  onToggle: (enabled: boolean) => void
}

export type AutoEffortGridState = {
  setting: RadarAutoEffortSetting
  isSaving: boolean
  /** Models the signed-in user may call. */
  userModels: string[]
  onToggle: (model: string, enabled: boolean) => void
}

function resolveModelAutoEffort(
  group: ModelRadarGroup,
  settings: ModelRadarSettings | undefined,
  autoEffort: AutoEffortGridState
): ModelAutoEffortState {
  const policy = (autoEffort.setting.policy ??
    'highest_iq') as RadarAutoEffortPolicy
  const minIQDelta = autoEffort.setting.min_iq_delta ?? 5
  const pick = pickAutoEffort(
    gatewayEffortCandidates(group.configurations),
    policy,
    minIQDelta
  )
  return {
    enabled: isModelAutoEffortEnabled(autoEffort.setting, group.model),
    // The switch is only offered when the administrator allowed the model and
    // the signed-in user can actually call it; anything else is hidden.
    available:
      isRadarAutoEffortAllowed(settings, group.model) &&
      matchRadarModelToUserModels(
        group.model,
        settings?.models[group.model]?.aliases,
        autoEffort.userModels
      ),
    targetEffort: pick?.effort ?? null,
    targetIQ: pick?.iq ?? null,
    isSaving: autoEffort.isSaving,
    onToggle: (enabled) => autoEffort.onToggle(group.model, enabled),
  }
}

export function CapabilityGrid(props: {
  configurations: ModelRadarConfiguration[]
  history: ModelRadarHistoryFrame[]
  iconRegistry?: ModelRadarIconRegistry
  settings?: ModelRadarSettings
  groupByVendor?: boolean
  autoEffort?: AutoEffortGridState
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<{
    model: string
    effort: string
  } | null>(null)
  const groups = groupConfigurations(props.configurations, props.settings)
  const vendors = listVendors(
    props.configurations,
    props.settings,
    props.iconRegistry
  )
  const efforts = matrixEfforts(props.configurations)
  const autoEffortByModel = useMemo(() => {
    if (!props.autoEffort) return null
    const autoEffort = props.autoEffort
    return new Map(
      groups.map((group) => [
        group.model,
        resolveModelAutoEffort(group, props.settings, autoEffort),
      ])
    )
  }, [groups, props.autoEffort, props.settings])
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
        className='bg-background text-muted-foreground sticky top-16 z-10 mb-1 hidden grid-cols-[repeat(var(--effort-count),minmax(0,1fr))] gap-1.5 py-1 text-center text-xs font-medium md:grid lg:pl-[17rem]'
        style={{ '--effort-count': efforts.length } as CSSProperties}
        aria-hidden='true'
      >
        {efforts.map((effort) => (
          <span key={effort} className='capitalize'>
            {effort}
          </span>
        ))}
      </div>
      <div className='space-y-2 pt-1.5 lg:space-y-2'>
        {props.groupByVendor
          ? vendors.map((vendor) => (
              <section
                key={vendor.key}
                aria-label={
                  vendor.key === OTHER_VENDOR ? t('Other') : vendor.label
                }
                className='space-y-2 pb-4'
              >
                <h3 className='text-muted-foreground flex items-center gap-2 pt-3 pb-1 text-sm font-medium'>
                  {vendor.icon ? (
                    <span aria-hidden='true'>
                      {getLobeIcon(vendor.icon, 16)}
                    </span>
                  ) : null}
                  {vendor.key === OTHER_VENDOR ? t('Other') : vendor.label}
                  <span className='text-xs'>
                    {t('{{count}} models', { count: vendor.modelCount })}
                  </span>
                </h3>
                {groups
                  .filter((group) => group.vendor === vendor.key)
                  .map((group) => (
                    <ModelRow
                      key={group.model}
                      group={group}
                      efforts={efforts}
                      settings={props.settings}
                      iconRegistry={props.iconRegistry}
                      autoEffort={autoEffortByModel?.get(group.model)}
                      onSelect={setSelected}
                      nested
                    />
                  ))}
              </section>
            ))
          : groups.map((group) => (
              <ModelRow
                key={group.model}
                group={group}
                efforts={efforts}
                settings={props.settings}
                iconRegistry={props.iconRegistry}
                autoEffort={autoEffortByModel?.get(group.model)}
                onSelect={setSelected}
              />
            ))}
      </div>
      <ConfigurationDetails
        configuration={selectedConfiguration}
        history={props.history}
        settings={props.settings}
        open={selectedConfiguration !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </section>
  )
}

function ModelRow(props: {
  group: ModelRadarGroup
  efforts: string[]
  settings?: ModelRadarSettings
  iconRegistry?: ModelRadarIconRegistry
  autoEffort?: ModelAutoEffortState
  onSelect: (configuration: ModelRadarConfiguration) => void
  nested?: boolean
}) {
  const { t } = useTranslation()
  const group = props.group
  const bestIq = Math.max(...group.configurations.map((item) => item.iq))
  const auto = props.autoEffort
  const showSwitch = Boolean(auto?.available)
  const activeEffort =
    auto?.enabled && auto.available ? auto.targetEffort : null
  const Heading = props.nested ? 'h4' : 'h3'
  return (
    <section
      aria-label={group.displayName}
      className='min-w-0 lg:grid lg:grid-cols-[16rem_minmax(0,1fr)] lg:items-center lg:gap-4'
    >
      <header className='mb-1.5 flex items-center gap-2 lg:mb-0'>
        <span className='shrink-0'>
          <ModelBadge
            color={group.color}
            model={group.model}
            settings={props.settings}
            iconRegistry={props.iconRegistry}
          />
        </span>
        <Heading
          className='min-w-0 flex-1 truncate text-sm font-semibold'
          title={group.model}
        >
          {group.displayName}
        </Heading>
        {showSwitch ? (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Switch
                    className='ml-auto'
                    checked={Boolean(auto?.enabled)}
                    disabled={Boolean(auto?.isSaving)}
                    onCheckedChange={auto?.onToggle}
                    aria-label={t(
                      'Automatically choose the reasoning tier for {{model}}',
                      { model: group.displayName }
                    )}
                  />
                }
              />
              <TooltipContent side='top'>
                {t(
                  auto?.enabled
                    ? 'Disable automatic reasoning tier'
                    : 'Enable automatic reasoning tier'
                )}
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        ) : null}
      </header>
      <div
        className='grid grid-cols-2 gap-1.5 md:grid-cols-[repeat(var(--effort-count),minmax(0,1fr))]'
        style={{ '--effort-count': props.efforts.length } as CSSProperties}
      >
        {props.efforts.map((effort) => {
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
          const matchesActive =
            activeEffort !== null &&
            configuration.effort.trim().toLowerCase() === activeEffort
          const isBest =
            activeEffort !== null ? matchesActive : configuration.iq === bestIq
          return (
            <TierCard
              key={effort}
              configuration={configuration}
              isBest={isBest}
              autoSelected={matchesActive}
              onSelect={() => props.onSelect(configuration)}
            />
          )
        })}
      </div>
    </section>
  )
}

function TierCard(props: {
  configuration: ModelRadarConfiguration
  isBest: boolean
  autoSelected: boolean
  onSelect: () => void
}) {
  const { t } = useTranslation()
  const format = useRadarFormatters()
  const configuration = props.configuration
  const card = (
    <button
      type='button'
      className={cn(
        'bg-card hover:bg-muted/40 focus-visible:ring-ring relative flex min-h-11 min-w-0 flex-col justify-center gap-0.5 rounded-md border px-1.5 py-1 text-center transition-colors outline-none focus-visible:ring-2 focus-visible:ring-offset-2',
        props.isBest && 'ring-primary/40 ring-1',
        props.autoSelected && 'border-primary/60 bg-primary/5'
      )}
      aria-label={t('View details for {{model}} {{effort}}', {
        model: configuration.model,
        effort: configuration.effort,
      })}
      onClick={props.onSelect}
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
      <span className='text-muted-foreground mx-auto flex w-full max-w-32 flex-wrap items-center justify-between gap-x-1 text-[10px] leading-[14px] tabular-nums'>
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
  // Ultra is the one tier the automatic reasoning tier must never move a request
  // onto, so hovering its card explains why it stays where the client put it.
  if (!isAutoEffortExcludedRadarTier(configuration.effort)) return card
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger render={card} />
        <TooltipContent side='top'>
          {t(
            'Ultra is excluded from the automatic reasoning tier: it is never switched to automatically.'
          )}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
