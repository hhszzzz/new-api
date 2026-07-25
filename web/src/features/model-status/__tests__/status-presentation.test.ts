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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import { getSuccessRateDotClass } from '@/features/performance-metrics/lib/format'

import { getModelStatusBarClass } from '../components/status-presentation.ts'

// The status page and the model catalog ("模型广场") must never show a different
// color for the same model, so the timeline bar is graded by success rate with
// the catalog's own palette rather than a parallel copy of it.
describe('model status bar color', () => {
  test('matches the model catalog palette at every success rate', () => {
    for (const rate of [100, 99.5, 90, 89.9, 70, 69.9, 1, 0]) {
      assert.equal(
        getModelStatusBarClass('operational', rate),
        getSuccessRateDotClass(rate)
      )
    }
  })

  test('keeps the catalog two shades of green apart', () => {
    const fullGreen = getModelStatusBarClass('operational', 100)
    const lighterGreen = getModelStatusBarClass('operational', 95)

    assert.equal(fullGreen, 'bg-emerald-500')
    assert.equal(lighterGreen, 'bg-emerald-400')
    assert.notEqual(fullGreen, lighterGreen)
  })

  test('uses the neutral bar when an hour has no requests', () => {
    const noData = getModelStatusBarClass('no_data', null)

    assert.equal(noData, 'bg-muted ring-1 ring-inset ring-border/70')
    assert.equal(getModelStatusBarClass('operational', null), noData)
  })

  test('grades degraded and failed hours as amber and red', () => {
    assert.equal(getModelStatusBarClass('degraded', 75), 'bg-amber-500')
    assert.equal(getModelStatusBarClass('failed', 20), 'bg-red-500')
  })
})
