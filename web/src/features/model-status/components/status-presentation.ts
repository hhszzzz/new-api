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

import type { ModelHealthStatus } from '../types'

type StatusPresentation = {
  barClassName: string
}

export const STATUS_PRESENTATION: Record<
  ModelHealthStatus,
  StatusPresentation
> = {
  operational: {
    barClassName: 'bg-success',
  },
  degraded: {
    barClassName: 'bg-warning',
  },
  failed: {
    barClassName: 'bg-destructive',
  },
  no_data: {
    barClassName: 'bg-muted ring-1 ring-inset ring-border/70',
  },
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
