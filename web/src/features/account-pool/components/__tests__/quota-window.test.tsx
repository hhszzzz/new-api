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
import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { AccountPoolWindow } from '../../types'
import { QuotaWindow } from '../quota-window'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

function quota(remainingPercent: number | null): AccountPoolWindow {
  return {
    used_percent:
      remainingPercent === null ? null : Math.max(0, 100 - remainingPercent),
    remaining_percent: remainingPercent,
    reset_at: null,
    limit_window_seconds: 18_000,
  }
}

function renderQuota(remainingPercent: number | null) {
  const view = render(
    <QuotaWindow
      window={quota(remainingPercent)}
      label='5-hour quota'
      now={Date.parse('2026-08-29T12:00:00Z')}
    />
  )
  return {
    indicator: view.container.querySelector('[data-slot="progress-indicator"]'),
    track: view.container.querySelector('[data-slot="progress-track"]'),
  }
}

afterEach(cleanup)

describe('account pool quota colors', () => {
  test.each([
    [75, 'bg-success', 'bg-success/15'],
    [50, 'bg-success', 'bg-success/15'],
    [40, 'bg-warning', 'bg-warning/15'],
    [20, 'bg-warning', 'bg-warning/15'],
    [10, 'bg-destructive', 'bg-destructive/15'],
    [null, 'bg-muted-foreground/50', 'bg-muted'],
  ])(
    'uses the semantic quota tone for %s percent remaining',
    (remainingPercent, indicatorClass, trackClass) => {
      const { indicator, track } = renderQuota(remainingPercent)

      expect(indicator).toHaveClass(indicatorClass)
      expect(track).toHaveClass(trackClass)
    }
  )
})
