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
import { ArrowDownUp, Cloud, Monitor, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { IconBadge } from '@/components/ui/icon-badge'

import {
  getConversionDiagnostics,
  getProtocolFlow,
  getProtocolName,
  getProtocolTranslationTarget,
  hasProtocolFieldAdjustments,
} from '../../lib/protocol-conversion'
import type { LogOtherData } from '../../types'
import {
  CollapsibleDetailSection,
  DetailRow,
  DetailSection,
} from './log-detail-layout'

const stateModeLabels: Record<string, string> = {
  native_responses: 'Native Responses session state',
  replay: 'Replayed conversation state',
  strict_append: 'Strict append session state',
}

export function ProtocolConversionDetails(props: {
  other: LogOtherData | null
  logType: number
  isAdminView: boolean
}) {
  const { t } = useTranslation()
  if (!props.isAdminView || props.logType === 6) return null
  const flow = getProtocolFlow(props.other)
  const diagnostics = getConversionDiagnostics(props.other)
  if (
    !flow.path &&
    !flow.request &&
    !flow.upstream &&
    !flow.converter &&
    diagnostics.length === 0
  ) {
    return null
  }

  const adjusted = hasProtocolFieldAdjustments(props.logType, props.other)
  const translationTarget = getProtocolTranslationTarget(
    props.logType,
    props.other
  )
  const blocked =
    props.logType === 5 && diagnostics.some((item) => item.severity === 'error')
  const hasProtocolFlow = Boolean(
    flow.converter || (flow.request && flow.upstream)
  )
  let outcome = hasProtocolFlow ? t('Request Conversion') : t('Not recorded')
  if (translationTarget) {
    outcome = t('{{protocol}} translation', { protocol: translationTarget })
  }
  if (flow.native) outcome = t('Native format')
  if (blocked) outcome = t('Conversion blocked')
  const outcomeVariant = blocked ? 'danger' : 'neutral'
  const warningLabel = props.logType === 2 ? t('Adjusted') : t('Warning')
  const stateMode = props.other?.admin_info?.protocol_state_mode
  const compactionMode = props.other?.admin_info?.compaction_mode
  const steps = [
    {
      label: t('Client request'),
      value: getProtocolName(flow.request) || t('Not recorded'),
      icon: Monitor,
    },
    { label: t('Request processing'), value: outcome, icon: ArrowDownUp },
    {
      label: t('Upstream request'),
      value: getProtocolName(flow.upstream) || t('Not recorded'),
      icon: Cloud,
    },
  ]
  const copyValue = steps
    .map((step) => `${step.label}: ${step.value}`)
    .join('\n')

  return (
    <DetailSection
      label={t('Request Conversion')}
      icon={<Route className='size-3.5' aria-hidden='true' />}
      iconTone='info'
    >
      <div className='flex min-w-0 flex-col gap-4 p-1.5'>
        <div className='flex min-w-0 items-start justify-between gap-3'>
          <div className='flex min-w-0 flex-col gap-1'>
            <span className='text-muted-foreground text-xs'>
              {t('Request path')}
            </span>
            <code className='text-xs break-all'>
              {flow.path || t('Not recorded')}
            </code>
          </div>
          <CopyButton
            value={copyValue}
            tooltip={t('Copy protocol flow')}
            className='size-7'
            iconClassName='size-3.5'
          />
        </div>
        <ol aria-label={t('Protocol flow')} className='flex min-w-0 flex-col'>
          {steps.map((step, index) => (
            <li
              key={step.label}
              className='relative min-w-0 pb-5 pl-10 last:pb-0'
            >
              {index < steps.length - 1 && (
                <span
                  className='border-border absolute top-7 bottom-0 left-3 border-l'
                  aria-hidden='true'
                />
              )}
              <span className='absolute top-0 left-0'>
                <IconBadge tone={index === 1 ? 'info' : 'neutral'} size='sm'>
                  <step.icon className='size-3.5' aria-hidden='true' />
                </IconBadge>
              </span>
              <div className='flex min-w-0 flex-col gap-1'>
                <span className='text-muted-foreground text-xs'>
                  {step.label}
                </span>
                {index === 1 ? (
                  <StatusBadge
                    label={step.value}
                    variant={adjusted ? 'warning' : outcomeVariant}
                    size='sm'
                    copyable={false}
                    className='self-start'
                  />
                ) : (
                  <span className='text-sm font-medium break-all'>
                    {step.value}
                  </span>
                )}
              </div>
            </li>
          ))}
        </ol>
        {diagnostics.length > 0 && (
          <section
            aria-label={t('Field adjustments')}
            className='bg-background min-w-0 rounded-lg border'
          >
            <div className='flex flex-wrap items-center justify-between gap-2 border-b px-3 py-2'>
              <h4 className='text-xs font-semibold'>
                {t('Field adjustments')}
              </h4>
              <StatusBadge
                label={t('Admin only')}
                variant='neutral'
                type='text'
                size='sm'
                copyable={false}
              />
            </div>
            <ul className='divide-y'>
              {diagnostics.map((item) => (
                <li
                  key={[
                    item.code,
                    item.path,
                    item.severity,
                    item.from,
                    item.to,
                  ].join(':')}
                  className='flex min-w-0 flex-col gap-1.5 px-3 py-3'
                >
                  <div className='flex min-w-0 flex-wrap items-start justify-between gap-2'>
                    <code className='min-w-0 text-xs break-all'>
                      {item.path || item.code}
                    </code>
                    <StatusBadge
                      label={
                        item.severity === 'error' ? t('Blocked') : warningLabel
                      }
                      variant={item.severity === 'error' ? 'danger' : 'warning'}
                      type='text'
                      size='sm'
                      copyable={false}
                    />
                  </div>
                  <p className='text-muted-foreground text-xs leading-relaxed [overflow-wrap:anywhere]'>
                    {props.logType === 2 &&
                    item.code === 'omitted_presentation_metadata'
                      ? t(
                          'The target protocol does not support this display metadata; it was omitted.'
                        )
                      : item.message}
                  </p>
                </li>
              ))}
            </ul>
            {props.other?.admin_info?.conversion_diagnostics_truncated && (
              <p className='text-muted-foreground border-t px-3 py-2 text-xs'>
                {t(
                  'Additional diagnostics were truncated when this log was recorded.'
                )}
              </p>
            )}
          </section>
        )}
        {(flow.converter ||
          compactionMode ||
          (stateMode && stateMode !== 'none')) && (
          <CollapsibleDetailSection label={t('Technical details')}>
            {compactionMode && (
              <DetailRow
                label={t('Compaction method')}
                value={
                  compactionMode === 'summary'
                    ? t('Model summary')
                    : t('Native compact')
                }
              />
            )}
            {flow.converter && (
              <DetailRow
                label={t('Protocol Converter')}
                value={flow.converter}
                mono
              />
            )}
            {stateMode && stateMode !== 'none' && (
              <DetailRow
                label={t('Protocol State Mode')}
                value={t(stateModeLabels[stateMode] || stateMode)}
              />
            )}
          </CollapsibleDetailSection>
        )}
      </div>
    </DetailSection>
  )
}
