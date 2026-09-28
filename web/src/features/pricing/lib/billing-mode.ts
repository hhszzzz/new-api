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
import type { PricingModel } from '../types'
import {
  getBillingContentKind,
  hasTaskUsageSchema,
  hasVaryingBilling,
  isDynamicPricingModel,
} from './dynamic-price'
import { isTokenBasedModel } from './model-helpers'

export type BillingModeLabelKey =
  | 'Per Request'
  | 'Dynamic Pricing'
  | 'Token-based'
  | 'Task billing'

/**
 * Billing badge for a model. Every new price is an expression now, so the
 * badge reports what the expression actually charges rather than the storage
 * mode: a flat token expression is token pricing, a flat `fixed()` expression
 * is per-request pricing, and only several prices, tiers, a clock window, or a
 * request-conditional multiplier read as dynamic.
 */
export function getBillingModeLabelKey(
  model: PricingModel
): BillingModeLabelKey {
  // Task-usage models badge as one business category; the metering unit
  // ($/1M token, $/credit, $/second) is already carried by the price line.
  if (hasTaskUsageSchema(model)) return 'Task billing'
  if (isDynamicPricingModel(model)) {
    if (hasVaryingBilling(model)) return 'Dynamic Pricing'
    const kind = getBillingContentKind(model)
    if (kind === 'token') return 'Token-based'
    if (kind === 'request') return 'Per Request'
    return 'Dynamic Pricing'
  }
  if (isTokenBasedModel(model)) return 'Token-based'
  return 'Per Request'
}
