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

import {
  getConversionDiagnostics,
  getProtocolFlow,
  getProtocolName,
  getProtocolTranslationTarget,
} from '../../lib/protocol-conversion'
import type { LogOtherData } from '../../types'
import {
  CollapsibleDetailSection,
  DetailRow,
  DetailSection,
} from './log-detail-layout'

const stateModeLabels: Record<string, string> = {
  native_responses: 'Upstream native session',
  strict_append: 'Send new messages only',
  replay: 'Resend full conversation history',
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
  const warningLabel = props.logType === 2 ? t('Adjusted') : t('Warning')
  const stateMode = props.other?.admin_info?.protocol_state_mode
  const compactionMode = props.other?.admin_info?.compaction_mode
  const clientValue = getProtocolName(flow.request) || t('Not recorded')
  const upstreamValue = getProtocolName(flow.upstream) || t('Not recorded')
  const pathValue = flow.path || t('Not recorded')
  const copyValue = [
    `${t('Request path')}: ${pathValue}`,
    `${t('Client request')}: ${clientValue}`,
    `${t('Upstream request')}: ${upstreamValue}`,
    `${t('Request processing')}: ${outcome}`,
  ].join('\n')

  return (
    <DetailSection
      label={t('Request Conversion')}
      icon={<Route className='size-3.5' aria-hidden='true' />}
      iconTone='plain'
    >
      <div className='flex min-w-0 flex-col gap-2.5'>
        <div className='flex min-w-0 items-start justify-between gap-3'>
          <div className='min-w-0 flex-1 space-y-1'>
            <DetailRow
              label={t('Request path')}
              icon={<Route className='size-3' aria-hidden='true' />}
              value={pathValue}
              mono
            />
            <DetailRow
              label={t('Client request')}
              icon={<Monitor className='size-3' aria-hidden='true' />}
              value={clientValue}
            />
            <DetailRow
              label={t('Upstream request')}
              icon={<Cloud className='size-3' aria-hidden='true' />}
              value={upstreamValue}
            />
            <DetailRow
              label={t('Request processing')}
              icon={<ArrowDownUp className='size-3' aria-hidden='true' />}
              value={outcome}
            />
          </div>
          <CopyButton
            value={copyValue}
            tooltip={t('Copy protocol flow')}
            className='size-7 shrink-0'
            iconClassName='size-3.5'
          />
        </div>
        {(flow.converter ||
          compactionMode ||
          (stateMode && stateMode !== 'none')) && (
          <div className='space-y-1'>
            {compactionMode && (
              <DetailRow
                label={t('Compaction method')}
                value={
                  compactionMode === 'summary'
                    ? t('Model compaction')
                    : t('Native compaction')
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
          </div>
        )}
        {diagnostics.length > 0 && (
          <CollapsibleDetailSection
            label={t('Field adjustments')}
            count={diagnostics.length}
          >
            {diagnostics.map((item) => (
              <div
                key={[
                  item.code,
                  item.path,
                  item.severity,
                  item.from,
                  item.to,
                ].join(':')}
                className='flex min-w-0 flex-col gap-1'
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
              </div>
            ))}
            {props.other?.admin_info?.conversion_diagnostics_truncated && (
              <p className='text-muted-foreground border-t pt-1.5 text-xs'>
                {t(
                  'Additional diagnostics were truncated when this log was recorded.'
                )}
              </p>
            )}
          </CollapsibleDetailSection>
        )}
      </div>
    </DetailSection>
  )
}
