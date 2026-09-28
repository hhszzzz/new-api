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
import { useTranslation } from 'react-i18next'

import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { cn } from '@/lib/utils'

import {
  getBillingModeLabelKey,
  type BillingModeLabelKey,
} from '../lib/billing-mode'
import type { PricingModel } from '../types'

interface ModelBillingModeBadgeProps {
  model: PricingModel
  appearance?: 'default' | 'caption'
  className?: string
}

/**
 * The color belongs to the label, never to the storage mode: two models that
 * show the same badge have to look the same. Coloring by whether the price is
 * stored as an expression would paint every expression model amber, including
 * the ones the label reports as token or per-request pricing.
 */
const variantByLabel: Record<BillingModeLabelKey, StatusVariant> = {
  'Token-based': 'info',
  'Per Request': 'purple',
  'Dynamic Pricing': 'warning',
  'Task billing': 'purple',
}

export function ModelBillingModeBadge(props: ModelBillingModeBadgeProps) {
  const { t } = useTranslation()
  const labelKey = getBillingModeLabelKey(props.model)
  const label = t(labelKey)
  const isCaption = props.appearance === 'caption'
  const variant = variantByLabel[labelKey]

  return (
    <StatusBadge
      label={label}
      variant={variant}
      type={isCaption ? 'text' : undefined}
      copyable={false}
      size='sm'
      className={cn(isCaption && 'text-xs font-normal', props.className)}
    />
  )
}
