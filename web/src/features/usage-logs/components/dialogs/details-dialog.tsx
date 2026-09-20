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
import type { TFunction } from 'i18next'
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
  Settings2,
  AlertTriangle,
  Headphones,
  Monitor,
  Cloud,
  ShieldCheck,
  UserCog,
  Info,
  LogIn,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { Label } from '@/components/ui/label'
import {
  localizedTierLabel,
  BILLING_PRICING_VARS,
} from '@/features/pricing/lib/billing-expr'
import { formatSubscriptionName } from '@/features/subscriptions/lib'
import { PolicyDecisionRecord } from '@/features/system-settings/request-policies/decision-record'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { formatLogQuota, formatTokens, formatUseTime } from '@/lib/format'
import { cn } from '@/lib/utils'

import { AuditDetailFields } from '../../audit/components/audit-detail-fields'
import type { UsageLog } from '../../data/schema'
import {
  parseLogOther,
  getParamOverrideActionLabel,
  parseAuditLine,
  getTieredBillingSummary,
  getTieredClockMultiplier,
  formatClockMultiplier,
  hasAnyCacheTokens,
  isViolationFeeLog,
  getFirstResponseTimeColor,
  getResponseTimeColor,
  resolveLogTimingMetrics,
  getReasoningEffortVariant,
  getReasoningEffortAutoPolicyLabel,
  getReasoningEffortAutoSkipLabel,
  renderAuditContent,
} from '../../lib/format'
import { getModelRouteInfo } from '../../lib/model-route'
import { buildQuotaAuditOperation } from '../../lib/quota-audit-operation'
import {
  getLogTypeConfig,
  isPerCallBilling,
  isTimingLogType,
} from '../../lib/utils'
import { USAGE_BILLING_PATH, type LogOtherData } from '../../types'
import { ResponseModelDetails } from '../model-badge'
import { PluginAuthorLink } from '../plugin-author-link'
import {
  CollapsibleDetailSection,
  DetailRow,
  DetailSection,
} from './log-detail-layout'
import { ProtocolConversionDetails } from './protocol-conversion-details'

// Maps a channel-update changed-field token (as recorded by the backend audit)
// to its i18n label key for display in the audit details.
const CHANNEL_FIELD_LABELS: Record<string, string> = {
  status: 'Status',
  models: 'Models',
  group: 'Group',
  type: 'Type',
  base_url: 'Base URL',
  key: 'Key',
}

function timingTextColorClass(
  variant: 'success' | 'warning' | 'danger'
): string {
  if (variant === 'success') return 'text-emerald-600'
  if (variant === 'warning') return 'text-amber-600'
  return 'text-rose-600'
}

function formatRatio(ratio: number | undefined): string {
  if (ratio == null) return '-'
  return ratio.toFixed(4)
}

function formatDiagnosticBytes(value: number | undefined): string {
  if (value == null || !Number.isFinite(value) || value < 0) return '-'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / 1024 / 1024).toFixed(1)} MiB`
}

function getUsageBillingPathLabel(
  t: TFunction,
  adminInfo: LogOtherData['admin_info']
): string {
  switch (adminInfo?.usage_billing_path) {
    case USAGE_BILLING_PATH.LOCAL:
      return t('Local Billing')
    case USAGE_BILLING_PATH.OPENAI:
      return t('Upstream Response (billing-usage-openai)')
    case USAGE_BILLING_PATH.OPENAI_ESTIMATED:
      return t('Upstream Response (billing-usage-openai-estimated)')
    case USAGE_BILLING_PATH.ANTHROPIC:
      return t('Upstream Response (billing-usage-anthropic)')
    case USAGE_BILLING_PATH.ANTHROPIC_ESTIMATED:
      return t('Upstream Response (billing-usage-anthropic-estimated)')
    case USAGE_BILLING_PATH.GEMINI:
      return t('Upstream Response (billing-usage-gemini)')
    case USAGE_BILLING_PATH.GEMINI_ESTIMATED:
      return t('Upstream Response (billing-usage-gemini-estimated)')
    case USAGE_BILLING_PATH.UPSTREAM:
      return t('Upstream Response')
    default:
      return adminInfo?.local_count_tokens
        ? t('Local Billing')
        : t('Upstream Response')
  }
}

function isUsageBillingPathLocal(
  adminInfo: LogOtherData['admin_info']
): boolean {
  if (adminInfo?.usage_billing_path) {
    return adminInfo.usage_billing_path === USAGE_BILLING_PATH.LOCAL
  }
  return adminInfo?.local_count_tokens === true
}

function quotaSaturationKindLabel(
  kind: 'overflow' | 'underflow' | 'nan',
  t: (key: string) => string
): string {
  if (kind === 'overflow') return t('Overflow')
  if (kind === 'underflow') return t('Underflow')
  return t('Invalid (NaN)')
}

function BillingBreakdown(props: {
  log: UsageLog
  other: LogOtherData
  isAdmin: boolean
}) {
  const { t } = useTranslation()
  const { log, other, isAdmin } = props
  const isPerCall = isPerCallBilling(other.model_price)
  const isClaude = other.claude === true
  const isTieredExpr = other.billing_mode === 'tiered_expr'
  const tieredSummary = getTieredBillingSummary(other)
  const clockMultiplier = getTieredClockMultiplier(
    other,
    tieredSummary?.priceEntries.map((entry) => entry.field)
  )

  const rows: Array<{ label: string; value: string }> = []
  const priceOpts = { digitsLarge: 4, digitsSmall: 6, abbreviate: false }
  const fmtPrice = (usd: number) => formatBillingCurrencyFromUSD(usd, priceOpts)
  const baseInputUSD = other.model_ratio != null ? other.model_ratio * 2.0 : 0

  if (isTieredExpr) {
    rows.push({
      label: t('Billing Mode'),
      value: t('Per-token dynamic billing'),
    })
    if (tieredSummary) {
      for (const entry of tieredSummary.priceEntries) {
        rows.push({
          label: t(entry.shortLabel),
          value: `${fmtPrice(entry.price)}/${entry.unit ? t(entry.unit) : 'M'}`,
        })
      }
    }
    // The prices above are the matched tier's own, so state the clock markup
    // they were billed at separately rather than folding it into them.
    if (clockMultiplier) {
      rows.push({
        label: t('{{tier}} multiplier', {
          tier: localizedTierLabel(clockMultiplier.label, t),
        }),
        value: formatClockMultiplier(clockMultiplier.entries, t),
      })
    }
  } else if (isPerCall) {
    rows.push({ label: t('Billing Mode'), value: t('Per-call') })
    if (other.model_price != null) {
      rows.push({
        label: t('Model Price'),
        value: fmtPrice(other.model_price),
      })
    }
  } else {
    rows.push({ label: t('Billing Mode'), value: t('Per-token') })
    if (other.model_ratio != null) {
      rows.push({
        label: t('Input'),
        value: `${fmtPrice(baseInputUSD)}/M`,
      })
    }
    if (other.completion_ratio != null && other.model_ratio != null) {
      rows.push({
        label: t('Output'),
        value: `${fmtPrice(baseInputUSD * other.completion_ratio)}/M`,
      })
    }
  }

  const userGR = other.user_group_ratio
  const isUserGR = userGR != null && Number.isFinite(userGR) && userGR !== -1
  const effectiveGR = isUserGR ? userGR : other.group_ratio
  if (effectiveGR != null && Number.isFinite(effectiveGR)) {
    rows.push({
      label: isUserGR ? t('User Exclusive Ratio') : t('Group Ratio'),
      value: `${formatRatio(effectiveGR)}x`,
    })
  }

  if (!isTieredExpr && isClaude && hasAnyCacheTokens(other)) {
    if (other.cache_ratio != null && other.cache_ratio !== 1) {
      rows.push({
        label: t('Cache Read'),
        value: `${fmtPrice(baseInputUSD * other.cache_ratio)}/M`,
      })
    }
    if (
      other.cache_creation_ratio != null &&
      other.cache_creation_ratio !== 1
    ) {
      rows.push({
        label: t('Cache Creation'),
        value: `${fmtPrice(baseInputUSD * other.cache_creation_ratio)}/M`,
      })
    }
    if (
      other.cache_creation_ratio_5m != null &&
      other.cache_creation_ratio_5m !== 0
    ) {
      rows.push({
        label: t('Cache Creation (5m)'),
        value: `${fmtPrice(baseInputUSD * other.cache_creation_ratio_5m)}/M`,
      })
    }
    if (
      other.cache_creation_ratio_1h != null &&
      other.cache_creation_ratio_1h !== 0
    ) {
      rows.push({
        label: t('Cache Creation (1h)'),
        value: `${fmtPrice(baseInputUSD * other.cache_creation_ratio_1h)}/M`,
      })
    }
  }

  if (!isTieredExpr) {
    if (other.audio_ratio != null && other.audio_ratio !== 1) {
      rows.push({
        label: t('Audio input'),
        value: `${fmtPrice(baseInputUSD * other.audio_ratio)}/M`,
      })
    }

    if (
      other.audio_completion_ratio != null &&
      other.audio_completion_ratio !== 1
    ) {
      rows.push({
        label: t('Audio output'),
        value: `${fmtPrice(baseInputUSD * other.audio_completion_ratio)}/M`,
      })
    }

    if (other.image_ratio != null && other.image_ratio !== 1) {
      rows.push({
        label: t('Image input'),
        value: `${fmtPrice(baseInputUSD * other.image_ratio)}/M`,
      })
    }
  }

  if (other.web_search && other.web_search_call_count) {
    rows.push({
      label: t('Web Search'),
      value: `${other.web_search_call_count}x${other.web_search_price ? ` (${fmtPrice(other.web_search_price)})` : ''}`,
    })
  }

  if (other.file_search && other.file_search_call_count) {
    rows.push({
      label: t('File Search'),
      value: `${other.file_search_call_count}x${other.file_search_price ? ` (${fmtPrice(other.file_search_price)})` : ''}`,
    })
  }

  if (other.image_generation_call && other.image_generation_call_price) {
    rows.push({
      label: t('Image Generation'),
      value: fmtPrice(other.image_generation_call_price),
    })
  }

  if (other.audio_input_seperate_price && other.audio_input_price) {
    rows.push({
      label: t('Audio Input Price'),
      value: fmtPrice(other.audio_input_price),
    })
  }

  if (isAdmin && other.admin_info) {
    rows.push({
      label: t('Billing Path'),
      value: getUsageBillingPathLabel(t, other.admin_info),
    })
  }

  const usageFacts =
    other.usage_facts != null &&
    typeof other.usage_facts === 'object' &&
    !Array.isArray(other.usage_facts)
      ? Object.entries(other.usage_facts)
      : []
  const requestRules = Array.isArray(other.request_rules)
    ? other.request_rules
    : []
  const requestRuleOccurrences = new Map<string, number>()
  const requestRuleRows = requestRules.map((rule) => {
    const baseKey = `${rule.cond}-${rule.multiplier}-${rule.matched}`
    const occurrence = requestRuleOccurrences.get(baseKey) ?? 0
    requestRuleOccurrences.set(baseKey, occurrence + 1)
    return { key: `${baseKey}-${occurrence}`, rule }
  })

  return (
    <DetailSection label={t('Billing Details')}>
      {rows.map((row) => (
        <DetailRow key={row.label} label={row.label} value={row.value} mono />
      ))}
      {usageFacts.length > 0 && (
        <>
          <Label className='text-xs font-semibold'>
            {t('Usage parameters')}
          </Label>
          {usageFacts.map(([key, value]) => (
            <DetailRow
              key={`usage-fact-${key}`}
              label={key}
              value={String(value)}
              mono
            />
          ))}
        </>
      )}
      {requestRules.length > 0 && (
        <div
          role='group'
          aria-label={t('Conditional multipliers')}
          className='space-y-1.5'
        >
          <Label className='text-xs font-semibold'>
            {t('Conditional multipliers')}
          </Label>
          {requestRuleRows.map(({ key, rule }) => (
            <DetailRow
              key={key}
              label={rule.cond}
              value={`${rule.multiplier}x${rule.matched ? ` · ${t('Matched')}` : ''}`}
              mono
            />
          ))}
        </div>
      )}
      <DetailRow
        label={t('Total Cost')}
        value={formatLogQuota(log.quota)}
        mono
      />
    </DetailSection>
  )
}

function TokenBreakdown(props: { log: UsageLog; other: LogOtherData }) {
  const { t } = useTranslation()
  const { log, other } = props

  const promptTokens = log.prompt_tokens || 0
  const completionTokens = log.completion_tokens || 0
  const cacheRead = other.cache_tokens || 0
  const cacheWrite = other.cache_creation_tokens || 0
  const cacheWrite5m = other.cache_creation_tokens_5m || 0
  const cacheWrite1h = other.cache_creation_tokens_1h || 0
  const hasTokens = promptTokens > 0 || completionTokens > 0

  if (!hasTokens) return null

  const rows: Array<{ label: string; value: string }> = []

  rows.push({ label: t('Input Tokens'), value: promptTokens.toLocaleString() })
  rows.push({
    label: t('Output Tokens'),
    value: completionTokens.toLocaleString(),
  })

  if (cacheRead > 0) {
    rows.push({
      label: t('Cache Read'),
      value: cacheRead.toLocaleString(),
    })
  }

  if (other.image_cache_tokens !== undefined) {
    rows.push({
      label: t('Image Cache'),
      value: other.image_cache_tokens.toLocaleString(),
    })
  }

  if (cacheWrite > 0 && cacheWrite5m === 0 && cacheWrite1h === 0) {
    rows.push({
      label: t('Cache Write'),
      value: cacheWrite.toLocaleString(),
    })
  }

  if (cacheWrite5m > 0) {
    rows.push({
      label: t('Cache Write (5m)'),
      value: cacheWrite5m.toLocaleString(),
    })
  }

  if (cacheWrite1h > 0) {
    rows.push({
      label: t('Cache Write (1h)'),
      value: cacheWrite1h.toLocaleString(),
    })
  }

  if (other.image && other.image_output) {
    rows.push({
      label: t('Image Tokens'),
      value: other.image_output.toLocaleString(),
    })
  }

  return (
    <DetailSection label={t('Token Breakdown')}>
      {rows.map((row) => (
        <DetailRow key={row.label} label={row.label} value={row.value} mono />
      ))}
      {other.billing_tokens && (
        <div
          role='group'
          aria-label={t('Billable token breakdown')}
          className='space-y-2'
        >
          <Label className='text-xs font-semibold'>
            {t('Billable token breakdown')}
          </Label>
          {BILLING_PRICING_VARS.map((variable) => {
            const count = other.billing_tokens?.[variable.key]
            if (count === undefined || !Number.isFinite(count)) return null
            return (
              <DetailRow
                key={variable.key}
                label={t(variable.shortLabel)}
                value={count.toLocaleString()}
                mono
              />
            )
          })}
        </div>
      )}
    </DetailSection>
  )
}

interface DetailsDialogProps {
  log: UsageLog
  isAdminView: boolean
  canViewModelRoute: boolean
  isRoot: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function DetailsDialog(props: DetailsDialogProps) {
  const { t } = useTranslation()
  const other = parseLogOther(props.log.other)
  const diagnostics = other?.diagnostics
  const timing = resolveLogTimingMetrics({
    other,
    useTimeSeconds: props.log.use_time,
    completionTokens: props.log.completion_tokens,
    isStream: props.log.is_stream,
    requestId: props.log.request_id,
  })
  const transport = timing.transport
  let transportLabel = 'HTTP'
  if (transport === 'websocket') {
    transportLabel = 'WebSocket'
  } else if (transport === 'sse') {
    transportLabel = 'SSE'
  }
  const modelRoute = getModelRouteInfo(other, props.canViewModelRoute)
  const responseModel = props.canViewModelRoute
    ? (other?.admin_info?.response_model ?? other?.response_model)
    : undefined
  const typeConfig = getLogTypeConfig(props.log.type)

  const isViolation = isViolationFeeLog(other)
  const isRefund = props.log.type === 6
  const isConsume = props.log.type === 2
  const isTopup = props.log.type === 1
  const isManage = props.log.type === 3
  const isSubscription = other?.billing_source === 'subscription'
  const hasAudioTokens = other?.ws || other?.audio
  const showTiming = isTimingLogType(props.log.type)
  const diagnosticIp = diagnostics?.ip || props.log.ip
  const showAdminIp =
    Boolean(diagnosticIp) && (showTiming || props.isAdminView || isTopup)
  const adminInfo = other?.admin_info
  const requestHeaderEntries = adminInfo?.request_headers
    ? Object.entries(adminInfo.request_headers)
    : []
  const userAgentHeader = requestHeaderEntries.find(
    ([name]) => name.toLowerCase() === 'user-agent'
  )
  const hiddenRequestHeaders = requestHeaderEntries.filter(
    ([name]) => name.toLowerCase() !== 'user-agent'
  )
  const paramOverrides = props.canViewModelRoute
    ? (adminInfo?.po ?? other?.po)
    : undefined
  const topupAuditFields =
    isTopup && props.isAdminView && adminInfo
      ? ([
          adminInfo.payment_method && {
            label: t('Order Payment Method'),
            value: adminInfo.payment_method,
          },
          adminInfo.callback_payment_method && {
            label: t('Callback Payment Method'),
            value: adminInfo.callback_payment_method,
          },
          adminInfo.caller_ip && {
            label: t('Callback Caller IP'),
            value: adminInfo.caller_ip,
          },
          adminInfo.server_ip && {
            label: t('Server IP'),
            value: adminInfo.server_ip,
          },
          adminInfo.node_name && {
            label: t('Node Name'),
            value: adminInfo.node_name,
          },
          adminInfo.version && {
            label: t('System Version'),
            value: adminInfo.version,
          },
        ].filter(Boolean) as Array<{ label: string; value: string }>)
      : []
  const showLegacyTopupWarning = isTopup && props.isAdminView && !adminInfo
  const showTopupAuditSection =
    isTopup &&
    props.isAdminView &&
    (topupAuditFields.length > 0 || showLegacyTopupWarning)
  const manageOperator = (() => {
    if (!isManage || !props.isAdminView || !adminInfo) return null
    const username = adminInfo.admin_username
    const id = adminInfo.admin_id
    const hasUsername = username != null && String(username).trim() !== ''
    const hasId = id != null && String(id).trim() !== ''
    if (!hasUsername && !hasId) return null
    if (hasUsername && hasId) return `${username} (ID: ${id})`
    if (hasUsername) return String(username)
    return `ID: ${id}`
  })()
  const authMethodLabel = (() => {
    if (!isManage || !props.isAdminView || !adminInfo?.auth_method) return ''
    if (adminInfo.auth_method === 'access_token') return t('Access Token')
    if (adminInfo.auth_method === 'session') return t('Session')
    return String(adminInfo.auth_method)
  })()

  // Top-up, audit, and login logs share the language-independent descriptor.
  const quotaOperation = isTopup
    ? buildQuotaAuditOperation(
        other?.op?.action ?? '',
        other?.op?.params ?? {},
        true,
        t
      )
    : null
  const operationText = renderAuditContent(other, t)
  const details = (isTopup ? operationText : null) ?? props.log.content ?? ''
  const auditRoute =
    isManage && props.isAdminView ? other?.audit_info : undefined
  // Channel update records which fields changed (stable field tokens); render
  // them with their localized labels for admins.
  const changedFieldTokens =
    isManage &&
    props.isAdminView &&
    Array.isArray(other?.op?.params?.changed_fields)
      ? (other.op.params.changed_fields as string[])
      : []
  const changedFieldsText = changedFieldTokens
    .map((field) => t(CHANNEL_FIELD_LABELS[field] ?? field))
    .join(', ')
  const showManageAuditSection =
    isManage &&
    props.isAdminView &&
    (operationText != null || auditRoute != null)

  // Login audit (type=7); visible to the log owner, not admin-only.
  const isLogin = props.log.type === 7
  const loginAuditFields = isLogin
    ? ([
        other?.login_method && {
          label: t('Login Method'),
          value: String(other.login_method),
        },
        props.log.ip && {
          label: t('IP Address'),
          value: props.log.ip,
          highlight: true,
        },
        other?.user_agent && {
          label: t('User Agent'),
          value: String(other.user_agent),
        },
      ].filter(Boolean) as Array<{
        label: string
        value: string
        highlight?: boolean
      }>)
    : []

  const useChannel = adminInfo?.retry_chain ?? other?.admin_info?.use_channel
  const channelChain =
    useChannel && useChannel.length > 0 ? useChannel.join(' → ') : undefined
  const reasoningEffortVariant = getReasoningEffortVariant(
    other?.reasoning_effort
  )
  const autoEffort = other?.reasoning_effort_auto
  const autoEffortApplied = Boolean(autoEffort?.applied)
  const autoEffortFrom = autoEffort?.from?.trim()
  const autoEffortTo = autoEffort?.to?.trim()
  const autoEffortPolicyLabel = getReasoningEffortAutoPolicyLabel(
    autoEffort?.policy
  )
  // A skipped decision keeps the client's tier, so it has no direction to show;
  // surface only the reasons the reader cannot infer, such as stale radar data.
  const autoEffortSkipLabel = autoEffortApplied
    ? undefined
    : getReasoningEffortAutoSkipLabel(autoEffort?.reason)
  const showAutoEffort = autoEffortApplied || Boolean(autoEffortSkipLabel)
  // Show the original tier only when it differs from the chosen one, so the row
  // reads as a direction. The final tier always renders, otherwise the row would
  // name the client's discarded tier and leave the replacement opaque.
  const showAutoEffortFrom =
    autoEffortApplied &&
    Boolean(autoEffortFrom) &&
    autoEffortFrom?.toLowerCase() !==
      (autoEffortTo || other?.reasoning_effort)?.toLowerCase()

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        <>
          {t('Log Details')}
          <StatusBadge
            label={t(typeConfig.label)}
            variant={typeConfig.color as StatusBadgeProps['variant']}
            size='sm'
            copyable={false}
          />
        </>
      }
      description={t('View the complete details for this log entry')}
      contentClassName={cn(
        'min-w-0 overflow-hidden',
        'max-sm:max-h-[calc(100dvh-1.5rem)] max-sm:w-[calc(100vw-1.5rem)] max-sm:max-w-[calc(100vw-1.5rem)] max-sm:p-4',
        'sm:max-w-xl'
      )}
      headerClassName='max-sm:gap-1'
      titleClassName='flex items-center gap-2 text-base'
      descriptionClassName='sr-only'
      contentHeight='min(72dvh, 720px)'
      bodyClassName='pr-2 sm:pr-4'
      scrollResetKey={props.log.id}
    >
      <div className='w-full max-w-full min-w-0 space-y-2.5 overflow-x-hidden py-1 sm:space-y-3'>
        {/* Overview section - key identifiers */}
        <div className='min-w-0 space-y-1'>
          {props.log.request_id && (
            <DetailRow
              label={t('Request ID')}
              value={props.log.request_id}
              mono
            />
          )}
          {props.log.upstream_request_id && (
            <DetailRow
              label={t('Upstream Request ID')}
              value={props.log.upstream_request_id}
              mono
            />
          )}

          {props.isAdminView &&
            diagnostics &&
            (diagnostics.request_size != null ||
              diagnostics.response_size != null ||
              diagnostics.upstream_request_size != null) && (
              <DetailRow
                label={t('Payload Size')}
                value={
                  <span className='flex flex-wrap gap-x-3 gap-y-0.5'>
                    {diagnostics.request_size != null && (
                      <span>
                        {t('Request')}:{' '}
                        {formatDiagnosticBytes(diagnostics.request_size)}
                      </span>
                    )}
                    {diagnostics.upstream_request_size != null && (
                      <span>
                        {t('Upstream Request')}:{' '}
                        {formatDiagnosticBytes(
                          diagnostics.upstream_request_size
                        )}
                      </span>
                    )}
                    {diagnostics.response_size != null && (
                      <span>
                        {t('Response')}:{' '}
                        {formatDiagnosticBytes(diagnostics.response_size)}
                      </span>
                    )}
                  </span>
                }
                mono
              />
            )}

          {props.isAdminView && props.log.channel > 0 && (
            <DetailRow
              label={t('Surface Channel')}
              value={
                <span>
                  {adminInfo?.surface_channel_name ||
                    props.log.channel_name ||
                    `#${props.log.channel}`}
                  {adminInfo?.aggregate_name &&
                    adminInfo.aggregate_name !==
                      adminInfo.surface_channel_name && (
                      <span className='text-muted-foreground'>
                        {' '}
                        ({t('Aggregate')}: {adminInfo.aggregate_name})
                      </span>
                    )}
                </span>
              }
              mono
            />
          )}

          {props.isAdminView &&
            (adminInfo?.actual_channel_name || props.log.channel > 0) && (
              <DetailRow
                label={t('Actual Channel')}
                value={`${adminInfo?.actual_channel_name || props.log.channel_name || '-'} #${adminInfo?.actual_channel_id || props.log.channel}`}
                mono
              />
            )}

          {channelChain && props.isAdminView && (
            <DetailRow label={t('Retry Chain')} value={channelChain} mono />
          )}

          {props.log.token_name && (
            <DetailRow label={t('Token')} value={props.log.token_name} mono />
          )}

          {(props.log.group || other?.group) && (
            <DetailRow
              label={t('Group')}
              value={props.log.group || other?.group || ''}
              mono
            />
          )}

          {showAdminIp && (
            <DetailRow
              label={t('IP Address')}
              value={diagnosticIp}
              mono
              highlight
            />
          )}

          {props.isAdminView && userAgentHeader && (
            <DetailRow label='user-agent' value={userAgentHeader[1]} mono />
          )}

          {showTiming && timing.durationSeconds > 0 && (
            <DetailRow
              label={t('Response Time')}
              value={
                <span
                  className={cn(
                    'font-medium',
                    timingTextColorClass(
                      getResponseTimeColor(
                        timing.durationSeconds,
                        props.log.completion_tokens
                      )
                    )
                  )}
                >
                  {formatUseTime(timing.durationSeconds)}
                  {timing.isStreaming &&
                    other?.frt != null &&
                    other.frt > 0 && (
                      <span
                        className={cn(
                          'font-normal',
                          timingTextColorClass(
                            getFirstResponseTimeColor(other.frt / 1000)
                          )
                        )}
                      >
                        {' '}
                        ({t('First token')}: {formatUseTime(other.frt / 1000)})
                      </span>
                    )}
                </span>
              }
            />
          )}

          {showTiming && (
            <DetailRow
              label={t('Connection Type')}
              value={transportLabel}
              mono
            />
          )}

          {other?.reasoning_effort && (
            <DetailRow
              label={t('Reasoning Effort')}
              value={
                <StatusBadge
                  label={other.reasoning_effort}
                  variant={reasoningEffortVariant}
                  size='sm'
                  type='text'
                  copyable={false}
                  className='font-mono !text-xs leading-normal'
                />
              }
            />
          )}

          {showAutoEffort && (
            <DetailRow
              label={t('Automatic reasoning effort')}
              value={
                autoEffortApplied ? (
                  <span className='flex flex-wrap items-center gap-1.5'>
                    {showAutoEffortFrom && autoEffortFrom ? (
                      <span className='flex items-center'>
                        <StatusBadge
                          label={autoEffortFrom}
                          variant={getReasoningEffortVariant(autoEffortFrom)}
                          size='sm'
                          type='text'
                          copyable={false}
                          className='font-mono !text-xs leading-normal'
                        />
                        <span className='text-muted-foreground mx-1'>→</span>
                      </span>
                    ) : null}
                    <StatusBadge
                      label={autoEffortTo || t('Auto')}
                      variant={
                        autoEffortTo
                          ? getReasoningEffortVariant(autoEffortTo)
                          : 'info'
                      }
                      size='sm'
                      type='text'
                      copyable={false}
                      className={cn(
                        '!text-xs leading-normal',
                        autoEffortTo && 'font-mono'
                      )}
                    />
                    {autoEffortPolicyLabel ? (
                      <span className='text-muted-foreground'>
                        {t(autoEffortPolicyLabel)}
                        {typeof autoEffort?.iq === 'number'
                          ? ` · IQ ${autoEffort.iq}`
                          : ''}
                      </span>
                    ) : null}
                  </span>
                ) : (
                  <span className='flex flex-wrap items-center gap-1.5'>
                    <StatusBadge
                      label={t('Not applied')}
                      variant='warning'
                      size='sm'
                      type='text'
                      copyable={false}
                      className='!text-xs leading-normal'
                    />
                    {autoEffortSkipLabel ? (
                      <span className='text-muted-foreground'>
                        {t(autoEffortSkipLabel)}
                      </span>
                    ) : null}
                  </span>
                )
              }
            />
          )}

          {other?.is_system_prompt_overwritten && (
            <DetailRow
              label={t('System Prompt')}
              value={
                <StatusBadge
                  label={t('Overwritten')}
                  variant='neutral'
                  size='sm'
                  type='text'
                  copyable={false}
                  className='font-mono !text-xs leading-normal'
                />
              }
            />
          )}
        </div>

        {props.isAdminView && adminInfo?.request_policy?.length ? (
          <CollapsibleDetailSection
            key={`request-policy-${props.log.id}-${props.open}`}
            label={t('Request policy decisions')}
            count={adminInfo.request_policy.length}
          >
            <PolicyDecisionRecord events={adminInfo.request_policy} />
          </CollapsibleDetailSection>
        ) : null}

        <ProtocolConversionDetails
          key={`conversion-${props.log.id}`}
          other={other}
          logType={props.log.type}
          isAdminView={props.isAdminView}
        />

        {/* Quota saturation marker (admin only) */}
        {props.isAdminView && other?.admin_info?.quota_saturation && (
          <DetailSection
            icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
            label={t('Quota clamped')}
            variant='danger'
          >
            <p className='mb-1 text-xs wrap-break-word'>
              {t('Quota saturation protection triggered')}
            </p>
            <DetailRow
              label={t('Kind')}
              value={quotaSaturationKindLabel(
                other.admin_info.quota_saturation.kind,
                t
              )}
            />
            <DetailRow
              label={t('Original value')}
              value={String(other.admin_info.quota_saturation.original)}
              mono
            />
            <DetailRow
              label={t('Clamped to')}
              value={String(other.admin_info.quota_saturation.clamped)}
              mono
            />
            <DetailRow
              label={t('Operation')}
              value={other.admin_info.quota_saturation.op}
              mono
            />
          </DetailSection>
        )}

        {/* Reject reason (admin only) */}
        {props.isAdminView && adminInfo?.reject_reason && (
          <DetailSection
            icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
            label={t('Reject Reason')}
            variant='danger'
          >
            <p className='text-xs wrap-break-word'>{adminInfo.reject_reason}</p>
          </DetailSection>
        )}

        {/* Violation fee info */}
        {isViolation && other && (
          <DetailSection
            icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
            label={t('Violation Fee')}
            variant='danger'
          >
            {other.violation_fee_code && (
              <DetailRow
                label={t('Violation Code')}
                value={other.violation_fee_code}
                mono
              />
            )}
            {other.violation_fee_marker && (
              <DetailRow
                label={t('Violation Marker')}
                value={other.violation_fee_marker}
              />
            )}
            <DetailRow
              label={t('Fee Amount')}
              value={formatLogQuota(other.fee_quota ?? props.log.quota)}
              mono
            />
          </DetailSection>
        )}

        {/* Refund details (type=6) */}
        {isRefund && other && (other.task_id || other.reason) && (
          <DetailSection label={t('Refund Details')}>
            {other.task_id && (
              <DetailRow label={t('Task ID')} value={other.task_id} mono />
            )}
            {other.reason && (
              <DetailRow label={t('Reason')} value={other.reason} />
            )}
          </DetailSection>
        )}

        {props.isAdminView && adminInfo?.task_plugin ? (
          <DetailSection label={t('Task Plugin')}>
            <DetailRow
              label={t('Plugin key')}
              value={adminInfo.task_plugin.key}
              mono
            />
            <DetailRow label={t('Name')} value={adminInfo.task_plugin.name} />
            {adminInfo.task_plugin.version ? (
              <DetailRow
                label={t('Version')}
                value={adminInfo.task_plugin.version}
                mono
              />
            ) : null}
            {adminInfo.task_plugin.author ? (
              <DetailRow
                label={t('Plugin author')}
                value={
                  <PluginAuthorLink
                    author={adminInfo.task_plugin.author}
                    showUrl
                  />
                }
              />
            ) : null}
          </DetailSection>
        ) : null}

        {props.isRoot && other?.root_info ? (
          <DetailSection label={t('Root Diagnostics')}>
            {other.root_info.task_plugin ? (
              <>
                <DetailRow
                  label={t('API Version')}
                  value={String(other.root_info.task_plugin.api_version)}
                  mono
                />
                <DetailRow
                  label={t('Plugin Generation')}
                  value={String(other.root_info.task_plugin.generation)}
                  mono
                />
              </>
            ) : null}
            {other.root_info.upstream_task_id ? (
              <DetailRow
                label={t('Upstream Task ID')}
                value={other.root_info.upstream_task_id}
                mono
              />
            ) : null}
            {other.root_info.node_name ? (
              <DetailRow
                label={t('Node Name')}
                value={other.root_info.node_name}
                mono
              />
            ) : null}
          </DetailSection>
        ) : null}

        {/* Top-up audit info (type=1, admin only) */}
        {showTopupAuditSection && (
          <DetailSection
            icon={<ShieldCheck className='size-3.5' aria-hidden='true' />}
            iconTone='success'
            label={t('Top-up Audit Info')}
          >
            {topupAuditFields.map((field) => (
              <DetailRow
                key={field.label}
                label={field.label}
                value={field.value}
                mono
              />
            ))}
            {showLegacyTopupWarning && (
              <div className='flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400'>
                <Info className='mt-0.5 size-3.5 shrink-0' aria-hidden='true' />
                <span>
                  {t(
                    'This historical record predates audit-info tracking and cannot be backfilled. The current instance already records server IP, callback IP, payment method, and system version for new top-ups going forward.'
                  )}
                </span>
              </div>
            )}
          </DetailSection>
        )}

        {quotaOperation && (
          <DetailSection label={t('Quota adjustment details')}>
            <AuditDetailFields fields={quotaOperation.fields} />
          </DetailSection>
        )}

        {/* Manage operator (type=3, admin only) */}
        {manageOperator && (
          <DetailRow
            label={
              <span className='flex items-center gap-1.5'>
                <UserCog
                  className='text-muted-foreground size-3.5'
                  aria-hidden='true'
                />
                {t('Operator Admin')}
              </span>
            }
            value={manageOperator}
            mono
          />
        )}

        {/* Operation audit info (type=3, admin only) */}
        {showManageAuditSection && (
          <DetailSection
            icon={<ShieldCheck className='size-3.5' aria-hidden='true' />}
            iconTone='info'
            label={t('Operation Audit Info')}
          >
            {operationText != null && (
              <DetailRow label={t('Operation')} value={operationText} />
            )}
            {authMethodLabel !== '' && (
              <DetailRow
                label={t('Authentication Method')}
                value={authMethodLabel}
              />
            )}
            {changedFieldsText !== '' && (
              <DetailRow
                label={t('Changed Fields')}
                value={changedFieldsText}
              />
            )}
            {auditRoute?.method && auditRoute?.route && (
              <DetailRow
                label={t('Request')}
                value={`${auditRoute.method} ${auditRoute.route}`}
                mono
              />
            )}
            {auditRoute?.status != null && (
              <DetailRow
                label={t('Result')}
                value={
                  auditRoute.success
                    ? `${t('Success')} (${auditRoute.status})`
                    : `${t('Failed')} (${auditRoute.status})`
                }
                mono
              />
            )}
          </DetailSection>
        )}

        {/* Login audit info (type=7) */}
        {isLogin && loginAuditFields.length > 0 && (
          <DetailSection
            icon={<LogIn className='size-3.5' aria-hidden='true' />}
            iconTone='info'
            label={t('Login Info')}
          >
            {operationText != null && (
              <DetailRow label={t('Operation')} value={operationText} />
            )}
            {loginAuditFields.map((field) => (
              <DetailRow
                key={field.label}
                label={field.label}
                value={field.value}
                mono
                highlight={field.highlight}
              />
            ))}
          </DetailSection>
        )}

        {/* Audio/WebSocket token breakdown */}
        {hasAudioTokens && other && (
          <DetailSection
            icon={<Headphones className='size-3.5' aria-hidden='true' />}
            iconTone='chart-4'
            label={t('Audio Tokens')}
          >
            {other.audio_input != null && other.audio_input > 0 && (
              <DetailRow
                label={t('Audio Input')}
                value={formatTokens(other.audio_input)}
                mono
              />
            )}
            {other.audio_output != null && other.audio_output > 0 && (
              <DetailRow
                label={t('Audio Output')}
                value={formatTokens(other.audio_output)}
                mono
              />
            )}
            {other.text_input != null && other.text_input > 0 && (
              <DetailRow
                label={t('Text Input')}
                value={formatTokens(other.text_input)}
                mono
              />
            )}
            {other.text_output != null && other.text_output > 0 && (
              <DetailRow
                label={t('Text Output')}
                value={formatTokens(other.text_output)}
                mono
              />
            )}
          </DetailSection>
        )}

        {/* Model mapping */}
        {modelRoute.actualModel ? (
          <DetailSection label={t('Model Mapping')}>
            <DetailRow
              label={t('Request Model')}
              value={props.log.model_name}
              mono
            />
            <DetailRow
              label={t('Actual Model')}
              value={modelRoute.actualModel}
              mono
            />
          </DetailSection>
        ) : null}
        {responseModel && (
          <DetailSection label={t('Response Model')}>
            <ResponseModelDetails observation={responseModel} />
          </DetailSection>
        )}

        {/* Token breakdown (for consume/error types with token data) */}
        {isDisplayableType(props.log.type) && other && (
          <TokenBreakdown log={props.log} other={other} />
        )}

        {/* Billing breakdown (consume type) */}
        {isConsume && other && !isViolation && (
          <BillingBreakdown
            log={props.log}
            other={other}
            isAdmin={props.isAdminView}
          />
        )}

        {isConsume && other?.image_count !== undefined && (
          <DetailRow
            label={t('Billable image count')}
            value={other.image_count}
          />
        )}
        {/* Admin billing mode indicator for non-consume */}
        {props.isAdminView &&
          !isConsume &&
          props.log.type !== 6 &&
          other?.admin_info && (
            <DetailRow
              label={t('Billing Path')}
              value={
                <span className='flex items-center gap-1'>
                  {isUsageBillingPathLocal(other.admin_info) ? (
                    <Monitor className='size-3 text-blue-500' />
                  ) : (
                    <Cloud className='size-3 text-emerald-500' />
                  )}
                  <span className='text-xs'>
                    {getUsageBillingPathLabel(t, other.admin_info)}
                  </span>
                </span>
              }
            />
          )}

        {/* Stream status details */}
        {other?.stream_status && other.stream_status.status !== 'ok' && (
          <DetailSection label={t('Stream Status')}>
            <DetailRow
              label={t('Status')}
              value={
                <StatusBadge
                  label={other.stream_status.status || t('Error')}
                  variant='red'
                  size='sm'
                  copyable={false}
                />
              }
            />
            {other.stream_status.end_reason && (
              <DetailRow
                label={t('End Reason')}
                value={other.stream_status.end_reason}
              />
            )}
            {(other.stream_status.error_count ?? 0) > 0 && (
              <DetailRow
                label={t('Soft Errors')}
                value={String(other.stream_status.error_count)}
              />
            )}
            {other.stream_status.end_error && (
              <DetailRow
                label={t('End Error')}
                value={other.stream_status.end_error}
              />
            )}
            {Array.isArray(other.stream_status.errors) &&
              other.stream_status.errors.length > 0 && (
                <pre className='bg-background/60 mt-1 max-h-32 overflow-y-auto rounded border p-2 font-mono text-[11px] leading-relaxed wrap-break-word whitespace-pre-wrap'>
                  {other.stream_status.errors.join('\n')}
                </pre>
              )}
          </DetailSection>
        )}

        {/* Subscription billing details */}
        {isSubscription && other && (
          <DetailSection label={t('Subscription Billing')}>
            <DetailRow
              label={t('Subscription')}
              value={formatSubscriptionName(
                { upgrade_group: other.subscription_group },
                other.subscription_plan_title,
                t
              )}
            />
            <DetailRow
              label={t('Deducted by subscription')}
              value={formatLogQuota(props.log.quota)}
              mono
            />
          </DetailSection>
        )}

        {/* Param override */}
        {Array.isArray(paramOverrides) && paramOverrides.length > 0 ? (
          <DetailSection
            icon={<Settings2 className='size-3.5' aria-hidden='true' />}
            iconTone='chart-3'
            label={`${t('Param Override')} (${paramOverrides.length})`}
          >
            {paramOverrides.filter(Boolean).map((line) => {
              const parsed = parseAuditLine(line)
              if (!parsed) return null
              return (
                <div
                  key={`${parsed.action}-${parsed.content}`}
                  className='bg-background/60 flex min-w-0 flex-col gap-1.5 rounded border p-2 sm:flex-row sm:items-start sm:gap-2'
                >
                  <StatusBadge
                    variant='neutral'
                    label={getParamOverrideActionLabel(parsed.action, t)}
                    className='shrink-0 font-medium'
                    copyable={false}
                  />
                  <span className='min-w-0 font-mono text-[11px] leading-relaxed break-all sm:wrap-break-word'>
                    {parsed.content}
                  </span>
                </div>
              )
            })}
          </DetailSection>
        ) : null}

        {/* Content */}
        {details && (
          <div className='space-y-1.5'>
            <Label className='text-xs font-semibold'>{t('Content')}</Label>
            <div className='bg-muted/30 relative min-w-0 overflow-hidden rounded-md border p-2.5'>
              <CopyButton
                value={details}
                className='absolute top-1.5 right-1.5 size-5'
                iconClassName='size-3'
              />
              <p className='min-w-0 pr-6 text-xs leading-relaxed break-all whitespace-pre-wrap sm:wrap-break-word'>
                {details}
              </p>
            </div>
          </div>
        )}

        {/* Safe request headers (admin only, collapsed at the bottom) */}
        {props.isAdminView && hiddenRequestHeaders.length > 0 && (
          <CollapsibleDetailSection
            key={`headers-${props.log.id}-${props.open}`}
            label={t('Safe Request Headers')}
            count={hiddenRequestHeaders.length}
          >
            {hiddenRequestHeaders.map(([name, value]) => (
              <DetailRow key={name} label={name} value={value} mono />
            ))}
          </CollapsibleDetailSection>
        )}
      </div>
    </Dialog>
  )
}

function isDisplayableType(type: number): boolean {
  return [0, 2, 5, 6].includes(type)
}
