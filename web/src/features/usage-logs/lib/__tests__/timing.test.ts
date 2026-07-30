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
import { describe, expect, test } from 'vitest'

import { resolveLogTimingMetrics } from '../format'

describe('usage-log timing metrics', () => {
  test('uses millisecond duration and post-first-response throughput for WebSocket logs', () => {
    const metrics = resolveLogTimingMetrics({
      other: {
        transport: 'websocket',
        duration_ms: 900,
        frt: 250,
      },
      useTimeSeconds: 1,
      completionTokens: 13,
      isStream: false,
    })

    expect(metrics.transport).toBe('websocket')
    expect(metrics.isStreaming).toBe(true)
    expect(metrics.durationSeconds).toBe(0.9)
    expect(metrics.tokensPerSecond).toBe(20)
  })

  test('falls back to legacy second precision when duration_ms is absent', () => {
    const metrics = resolveLogTimingMetrics({
      other: { transport: 'sse', frt: 1000 },
      useTimeSeconds: 3,
      completionTokens: 10,
      isStream: true,
    })

    expect(metrics.durationSeconds).toBe(3)
    expect(metrics.tokensPerSecond).toBe(5)
  })

  test('uses diagnostic millisecond duration for existing administrator logs', () => {
    const metrics = resolveLogTimingMetrics({
      other: { diagnostics: { duration_ms: 2750 } },
      useTimeSeconds: 3,
      completionTokens: 11,
      isStream: false,
    })

    expect(metrics.durationSeconds).toBe(2.75)
    expect(metrics.tokensPerSecond).toBe(4)
  })
})
