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

import { getSuccessRateDotClass } from '@/features/performance-metrics/lib/format'

import type { ModelHealthStatus } from '../status-types'

// Compact card strips render no-data bars without a ring so the thin 3px bars
// read as one flat neutral band, matching the original catalog cards.
const NO_DATA_BAR_CLASS = 'bg-muted-foreground/15'

/**
 * Bar color for one hour of the compact status strip on model cards.
 *
 * Uses the model catalog ("模型广场") success-rate palette, including the
 * catalog's two shades of green (full green at 100%, lighter green above 90%).
 * The theme-level `bg-success`/`bg-warning`/`bg-destructive` tokens are
 * deliberately not used here: theme presets redefine them, which would make
 * the strip drift from the catalog.
 */
export function getModelStatusBarClass(
  status: ModelHealthStatus,
  successRate: number | null
): string {
  if (status === 'no_data' || successRate === null) return NO_DATA_BAR_CLASS
  return getSuccessRateDotClass(successRate)
}

export function getModelStatusLabel(
  t: TFunction,
  status: ModelHealthStatus
): string {
  switch (status) {
    case 'operational':
      return t('Operational')
    case 'degraded':
      return t('Degraded')
    case 'failed':
      return t('Unavailable')
    case 'no_data':
      return t('No data')
  }
}
