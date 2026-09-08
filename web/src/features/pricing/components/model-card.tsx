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
import { ChevronRight } from 'lucide-react'
import { memo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { getLobeIcon } from '@/lib/lobe-icon'
import { resolveProviderIconKey } from '@/lib/provider-icon'
import { cn } from '@/lib/utils'

import { DEFAULT_TOKEN_UNIT } from '../constants'
import { getModelDescription } from '../lib/catalog-presentation'
import {
  getCardExamplePrice,
  getDynamicDisplayGroupRatio,
  getDynamicPriceUnitLabelKey,
  getDynamicPricingSummary,
  isUnconfiguredTaskUsageModel,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import { isTokenBasedModel } from '../lib/model-helpers'
import { formatPrice, formatRequestPrice } from '../lib/price'
import { getTaskNumberFields } from '../lib/task-expr'
import type { PricingModel, TokenUnit } from '../types'
import { ModelBillingModeBadge } from './model-billing-mode-badge'
import { ModelPerfBadge, type ModelPerfBadgeData } from './model-perf-badge'

export interface ModelCardProps {
  model: PricingModel
  onClick: () => void
  priceRate?: number
  usdExchangeRate?: number
  tokenUnit?: TokenUnit
  showRechargePrice?: boolean
  selectedGroup?: string
  perf?: ModelPerfBadgeData
  generatedAt?: number
  onOpenPerformance?: () => void
}

export const ModelCard = memo(function ModelCard(props: ModelCardProps) {
  const { t } = useTranslation()
  const tokenUnit = props.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const priceRate = props.priceRate ?? 1
  const usdExchangeRate = props.usdExchangeRate ?? 1
  const showRechargePrice = props.showRechargePrice ?? false
  const isTokenBased = isTokenBasedModel(props.model)
  const description = getModelDescription(props.model)
  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'
  const tags = parseTags(props.model.tags)
  const endpoints = props.model.supported_endpoint_types || []
  const modelIconKey = resolveProviderIconKey(
    props.model.vendor_icon,
    props.model.icon
  )
  const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 28) : null
  const initial = props.model.model_name?.charAt(0).toUpperCase() || '?'
  const isDynamicPricing =
    props.model.billing_mode === 'tiered_expr' &&
    Boolean(props.model.billing_expr)
  const isUnconfiguredTaskUsage = isUnconfiguredTaskUsageModel(props.model)
  const hasCachedPrice = isTokenBased && props.model.cache_ratio != null
  const dynamicPriceOptions = {
    tokenUnit,
    showRechargePrice,
    priceRate,
    usdExchangeRate,
    groupRatioMultiplier: getDynamicDisplayGroupRatio(
      props.model,
      props.selectedGroup
    ),
  }
  const dynamicSummary = isDynamicPricing
    ? getDynamicPricingSummary(props.model, dynamicPriceOptions)
    : null
  const cardExamplePrice = getCardExamplePrice(props.model, dynamicPriceOptions)
  const showTaskFieldLabels =
    getTaskNumberFields(props.model.billing_usage_schema).length > 1

  const bottomTags = [...endpoints.slice(0, 2), ...tags.slice(0, 2)]
  const hiddenCount =
    Math.max(endpoints.length - 2, 0) + Math.max(tags.length - 2, 0)

  let priceSummary: ReactNode
  if (dynamicSummary) {
    if (dynamicSummary.isSpecialExpression) {
      priceSummary = (
        <span className='min-w-0'>
          <span className='text-amber-700 dark:text-amber-300'>
            {t('Special billing expression')}
          </span>
          <code className='text-muted-foreground/70 mt-0.5 line-clamp-1 block font-mono text-[11px] break-all'>
            {dynamicSummary.rawExpression}
          </code>
        </span>
      )
    } else if (dynamicSummary.primaryEntries.length > 0) {
      priceSummary = (
        <>
          {dynamicSummary.primaryEntries.map((entry) => {
            const unitLabelKey =
              getDynamicPriceUnitLabelKey(entry) ??
              (entry.unit === 'token' ? tokenUnitLabel : null)
            let fieldPrefix: ReactNode = null
            if (entry.labelKind !== 'schema') {
              fieldPrefix = <>{t(entry.shortLabel)} </>
            } else if (showTaskFieldLabels) {
              fieldPrefix = (
                <>
                  <code className='font-mono text-[11px]'>
                    {entry.shortLabel}
                  </code>{' '}
                </>
              )
            }
            return (
              <span
                key={entry.key}
                className='text-muted-foreground whitespace-nowrap'
              >
                {fieldPrefix}
                <span className='text-foreground font-mono font-semibold'>
                  {entry.formattedRange ?? entry.formatted}
                  {unitLabelKey && <> / {t(unitLabelKey)}</>}
                </span>
              </span>
            )
          })}
          {cardExamplePrice && (
            <span className='text-muted-foreground/70 max-w-full min-w-0 truncate text-xs'>
              {cardExamplePrice.label} ≈ {cardExamplePrice.formatted}
            </span>
          )}
          {dynamicSummary.isTaskUsage &&
            dynamicSummary.tier?.label &&
            !dynamicSummary.primaryEntries.some(
              (entry) => entry.formattedRange
            ) && (
              <span className='text-muted-foreground text-xs'>
                ({dynamicSummary.tier.label})
              </span>
            )}
        </>
      )
    } else {
      priceSummary = (
        <span className='text-muted-foreground text-sm'>
          {t('Dynamic Pricing')}
        </span>
      )
    }
  } else if (isUnconfiguredTaskUsage) {
    priceSummary = (
      <span className='text-muted-foreground text-sm'>
        {t('Usage-based billing · price not configured')}
      </span>
    )
  } else if (isTokenBased) {
    priceSummary = (
      <>
        <span className='text-muted-foreground whitespace-nowrap'>
          {t('Input')}{' '}
          <span className='text-foreground font-mono font-semibold'>
            {formatPrice(
              props.model,
              'input',
              tokenUnit,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              props.selectedGroup
            )}
          </span>
        </span>
        <span className='text-muted-foreground whitespace-nowrap'>
          {t('Output')}{' '}
          <span className='text-foreground font-mono font-semibold'>
            {formatPrice(
              props.model,
              'output',
              tokenUnit,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              props.selectedGroup
            )}
          </span>
        </span>
        {hasCachedPrice && (
          <span className='text-muted-foreground whitespace-nowrap'>
            {t('Cached')}{' '}
            <span className='text-foreground font-mono font-semibold'>
              {formatPrice(
                props.model,
                'cache',
                tokenUnit,
                showRechargePrice,
                priceRate,
                usdExchangeRate,
                props.selectedGroup
              )}
            </span>
          </span>
        )}
      </>
    )
  } else {
    priceSummary = (
      <span className='text-muted-foreground whitespace-nowrap'>
        <span className='text-foreground font-mono font-semibold'>
          {formatRequestPrice(
            props.model,
            showRechargePrice,
            priceRate,
            usdExchangeRate,
            props.selectedGroup
          )}
        </span>{' '}
        / {t('request')}
      </span>
    )
  }

  return (
    <div
      className={cn(
        'group relative flex flex-col rounded-xl border p-3 transition-colors sm:p-5',
        'hover:bg-muted/20'
      )}
    >
      {/* Header: icon + name + price + actions */}
      <div className='flex items-start justify-between gap-2.5 sm:gap-3'>
        <div className='flex min-w-0 items-start gap-2.5 sm:gap-3'>
          <div className='bg-muted/40 flex size-9 shrink-0 items-center justify-center rounded-lg sm:size-10 sm:rounded-xl'>
            {modelIcon || (
              <span className='text-muted-foreground text-sm font-bold'>
                {initial}
              </span>
            )}
          </div>
          <div className='min-w-0'>
            <h3
              className='text-foreground truncate font-mono text-[15px] leading-tight font-bold'
              title={props.model.model_name}
            >
              {props.model.model_name}
            </h3>
            <div className='mt-0.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm sm:mt-1 sm:gap-x-3'>
              {priceSummary}
            </div>
          </div>
        </div>

        <div className='flex shrink-0 items-center gap-1.5'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onClick}
            className='text-muted-foreground gap-1 px-2 text-xs'
          >
            {t('Details')}
            <ChevronRight className='size-3.5' />
          </Button>
          <CopyButton
            value={props.model.model_name}
            variant='outline'
            className='text-muted-foreground size-7 rounded-md'
            iconClassName='size-3.5'
            aria-label={t('Copy model name')}
          />
        </div>
      </div>

      <p
        className={cn(
          'text-muted-foreground mt-2 line-clamp-1 flex-1 text-[13px] leading-relaxed sm:line-clamp-2',
          description ? 'sm:mt-4 sm:min-h-[2.5rem]' : 'sm:mt-3'
        )}
      >
        {description}
      </p>

      <div className='mt-2 flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 sm:mt-4'>
        <div className='flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1'>
          <ModelBillingModeBadge model={props.model} />
        </div>
        <div className='flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-0.5 sm:gap-x-3 sm:gap-y-1'>
          {bottomTags.map((item) => (
            <span key={item} className='text-muted-foreground/70 text-xs'>
              {item}
            </span>
          ))}
          {isTokenBased &&
            !dynamicSummary?.isTaskUsage &&
            !isUnconfiguredTaskUsage && (
              <span className='text-muted-foreground/50 text-xs'>
                {tokenUnitLabel}
              </span>
            )}
          {hiddenCount > 0 && (
            <span className='text-muted-foreground/40 text-xs'>
              +{hiddenCount}
            </span>
          )}
        </div>
      </div>
      <ModelPerfBadge
        perf={props.perf}
        generatedAt={props.generatedAt}
        onOpenPerformance={props.onOpenPerformance}
        className='border-border/60 mt-3 border-t pt-3'
      />
    </div>
  )
})
