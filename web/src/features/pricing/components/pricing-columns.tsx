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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { BadgeCell, DataTableColumnHeader } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { getLobeIcon } from '@/lib/lobe-icon'
import { resolveProviderIconKey } from '@/lib/provider-icon'

import { DEFAULT_TOKEN_UNIT } from '../constants'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
  isUnconfiguredTaskUsageModel,
} from '../lib/dynamic-price'
import { isTokenBasedModel } from '../lib/model-helpers'
import { formatPrice, stripTrailingZeros } from '../lib/price'
import type { PricingModel } from '../types'
import { ModelBillingModeBadge } from './model-billing-mode-badge'
import {
  ModelPerfBadge,
  type ModelPerfBadgeData,
} from './model-perf-badge'
import { ModelPriceCell, type ModelPriceCellOptions } from './model-price-cell' 

// ----------------------------------------------------------------------------
// Pricing Table Columns
// ----------------------------------------------------------------------------

export type PricingColumnsOptions = ModelPriceCellOptions & {
  onModelClick?: (modelName: string) => void
  perfByModel?: ReadonlyMap<string, ModelPerfBadgeData>
  onOpenPerformance?: (modelName: string) => void
}

export function usePricingColumns(
  options: PricingColumnsOptions = {}
): ColumnDef<PricingModel>[] {
  const { t } = useTranslation()
  const {
    tokenUnit = DEFAULT_TOKEN_UNIT,
    priceRate = 1,
    usdExchangeRate = 1,
    showRechargePrice = false,
    selectedGroup,
  } = options

  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'

  const columns: ColumnDef<PricingModel>[] = [
    // Model column
    {
      accessorKey: 'model_name',
      meta: { label: t('Model') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Model')} />
      ),
      cell: ({ row }) => {
        const model = row.original
        const modelIconKey = resolveProviderIconKey(
          model.vendor_icon,
          model.icon
        )
        const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 14) : null

        return (
          <div className='flex max-w-[240px] min-w-0 items-center gap-2'>
            {modelIcon}
            <span
              title={model.model_name}
              className='truncate font-mono text-sm font-medium'
            >
              {model.model_name}
            </span>
          </div>
        )
      },
      minSize: 200,
    },

    // Type column
    {
      id: 'health',
      meta: { label: t('Status') },
      header: t('Status'),
      minSize: 310,
      size: 340,
      enableSorting: false,
      cell: ({ row }) => (
        <ModelPerfBadge
          className='min-w-[295px]'
          perf={options.perfByModel?.get(row.original.model_name)}
          onOpenPerformance={
            options.onOpenPerformance
              ? () => options.onOpenPerformance?.(row.original.model_name)
              : undefined
          }
        />
      ),
    },
    {
      accessorKey: 'quota_type',
      header: t('Type'),
      cell: ({ row }) => (
        <ModelBillingModeBadge model={row.original} className='-ml-1.5' />
      ),
      size: 110,
      enableSorting: false,
    },

    // Price column
    {
      accessorKey: 'price',
      meta: { label: t('Price') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Price')} />
      ),
      cell: ({ row }) => (
        <ModelPriceCell model={row.original} options={options} />
      ),
      size: 180,
      enableSorting: false,
    },

    // Cached price column (Vercel AI Gateway style)
    {
      id: 'cached_price',
      header: t('Cached'),
      cell: ({ row }) => {
        const model = row.original
        const dynamicSummary = getDynamicPricingSummary(model, {
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          groupRatioMultiplier: getDynamicDisplayGroupRatio(
            model,
            selectedGroup
          ),
        })

        if (dynamicSummary) {
          if (dynamicSummary.isSpecialExpression) {
            return (
              <span className='text-muted-foreground/50 text-xs'>
                {t('Special billing expression')}
              </span>
            )
          }

          const cacheEntry = dynamicSummary.entries.find(
            (entry) => entry.field === 'cacheReadPrice'
          )
          if (!cacheEntry) {
            return <span className='text-muted-foreground/30 text-xs'>—</span>
          }

          return (
            <div className='max-w-full min-w-0'>
              <span className='font-mono text-sm tabular-nums'>
                {stripTrailingZeros(cacheEntry.formatted)}
              </span>
              <div className='text-muted-foreground/50 text-[10px]'>
                / {tokenUnitLabel}
              </div>
            </div>
          )
        }

        if (isUnconfiguredTaskUsageModel(model)) {
          return <span className='text-muted-foreground/30 text-xs'>—</span>
        }

        const isTokenBased = isTokenBasedModel(model)

        if (!isTokenBased || model.cache_ratio == null) {
          return <span className='text-muted-foreground/30 text-xs'>—</span>
        }

        const cachedPrice = stripTrailingZeros(
          formatPrice(
            model,
            'cache',
            tokenUnit,
            showRechargePrice,
            priceRate,
            usdExchangeRate,
            selectedGroup
          )
        )

        return (
          <div className='max-w-full min-w-0'>
            <span className='font-mono text-sm tabular-nums'>
              {cachedPrice}
            </span>
            <div className='text-muted-foreground/50 text-[10px]'>
              / {tokenUnitLabel}
            </div>
          </div>
        )
      },
      size: 110,
      enableSorting: false,
    },

    // Vendor column
    {
      accessorKey: 'vendor_name',
      header: t('Vendor'),
      cell: ({ row }) => {
        const model = row.original
        if (!model.vendor_name) {
          return <span className='text-muted-foreground/50 text-xs'>—</span>
        }
        const vendorIcon = model.vendor_icon
          ? getLobeIcon(model.vendor_icon, 12)
          : null
        return (
          <BadgeCell className='gap-1.5'>
            {vendorIcon}
            <StatusBadge
              label={model.vendor_name}
              autoColor={model.vendor_name}
              size='sm'
              copyable={false}
            />
          </BadgeCell>
        )
      },
      size: 130,
    },
  ]

  if (options.onModelClick) {
    columns.push({
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) => (
        <Button
          type='button'
          variant='ghost'
          size='sm'
          aria-label={`${t('View details')}: ${row.original.model_name}`}
          onClick={(event) => {
            event.stopPropagation()
            options.onModelClick?.(row.original.model_name)
          }}
        >
          {t('Details')}
        </Button>
      ),
      size: 90,
      enableSorting: false,
    })
  }

  return columns
}
