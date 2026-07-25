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

import {
  getModelStatusContentKind,
  normalizeStatusTimeline,
  sortModelStatuses,
} from '../lib/model-status.ts'
import type {
  ModelHealthStatus,
  ModelStatusModel,
  ModelStatusSnapshot,
} from '../types.ts'

function createModel(
  modelName: string,
  status: ModelHealthStatus
): ModelStatusModel {
  return {
    model_name: modelName,
    vendor: 'test',
    icon: '',
    request_count: 0,
    success_count: 0,
    success_rate: null,
    avg_ttft_ms: null,
    avg_latency_ms: null,
    avg_tps: null,
    status,
    timeline: [],
  }
}

describe('model status presentation data', () => {
  test('sorts by severity and model name without mutating the source array', () => {
    const models = [
      createModel('no-data', 'no_data'),
      createModel('healthy-b', 'operational'),
      createModel('degraded', 'degraded'),
      createModel('failed', 'failed'),
      createModel('healthy-a', 'operational'),
    ]

    const sorted = sortModelStatuses(models)

    assert.deepEqual(
      sorted.map((model) => model.model_name),
      ['failed', 'degraded', 'healthy-a', 'healthy-b', 'no-data']
    )
    assert.equal(models[0]?.model_name, 'no-data')
  })

  test('fills a 24-hour timeline with no-data slots while preserving real data', () => {
    const currentHour = 1_800_000_000
    const operationalHour = currentHour - 2 * 60 * 60

    const timeline = normalizeStatusTimeline(
      [
        {
          ts: operationalHour + 120,
          status: 'operational',
          request_count: 4,
          success_count: 4,
          success_rate: 100,
          avg_ttft_ms: 120,
          avg_latency_ms: 240,
          avg_tps: 30,
        },
      ],
      currentHour + 300
    )

    assert.equal(timeline.length, 24)
    assert.equal(timeline[0]?.ts, currentHour - 23 * 60 * 60)
    assert.equal(timeline.at(-1)?.ts, currentHour)
    assert.deepEqual(timeline.at(-3), {
      ts: operationalHour,
      status: 'operational',
      request_count: 4,
      success_count: 4,
      success_rate: 100,
      avg_ttft_ms: 120,
      avg_latency_ms: 240,
      avg_tps: 30,
    })
    assert.equal(
      timeline.filter((point) => point.status === 'no_data').length,
      23
    )
    assert.deepEqual(timeline.at(-1), {
      ts: currentHour,
      status: 'no_data',
      request_count: 0,
      success_count: 0,
      success_rate: null,
      avg_ttft_ms: null,
      avg_latency_ms: null,
      avg_tps: null,
    })
  })

  test('keeps a cached snapshot visible when a background refresh fails', () => {
    const snapshot: ModelStatusSnapshot = {
      generated_at: 1_800_000_000,
      window_hours: 24,
      models: [createModel('cached-model', 'operational')],
    }

    assert.equal(getModelStatusContentKind(snapshot, false, true), 'ready')
  })

  test('shows an error when the initial request fails without cached data', () => {
    assert.equal(getModelStatusContentKind(undefined, false, true), 'error')
  })

  test('distinguishes initial loading from an empty successful snapshot', () => {
    const emptySnapshot: ModelStatusSnapshot = {
      generated_at: 1_800_000_000,
      window_hours: 24,
      models: [],
    }

    assert.equal(getModelStatusContentKind(undefined, true, false), 'loading')
    assert.equal(
      getModelStatusContentKind(emptySnapshot, false, false),
      'empty'
    )
  })
})
