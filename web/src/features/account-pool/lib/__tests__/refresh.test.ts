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

import type { AccountPoolApiResponse, AccountPoolSnapshot } from '../../types'
import {
  getAccountPoolRefetchInterval,
  getAccountPoolServerNow,
  shouldRefreshAccountPoolOnVisibility,
} from '../refresh'

function response(nextRefreshAt: string, serverTime = '2026-08-29T12:00:00Z') {
  return {
    success: true,
    message: '',
    data: {
      next_refresh_at: nextRefreshAt,
      server_time: serverTime,
    } as AccountPoolSnapshot,
  } satisfies AccountPoolApiResponse<AccountPoolSnapshot>
}

describe('account pool refresh scheduling', () => {
  test('uses the server next-refresh time and keeps a one-second floor', () => {
    const clientReceivedAt = Date.parse('2026-08-29T20:00:00Z')
    expect(
      getAccountPoolRefetchInterval(
        response('2026-08-29T12:05:00Z'),
        clientReceivedAt,
        clientReceivedAt
      )
    ).toBe(300_000)
    expect(
      getAccountPoolRefetchInterval(
        response('2026-08-29T11:59:59Z'),
        clientReceivedAt,
        clientReceivedAt
      )
    ).toBe(1000)
  })

  test('keeps countdowns aligned when client clocks are different', () => {
    const fastClientReceivedAt = Date.parse('2026-08-29T20:00:00Z')
    const slowClientReceivedAt = Date.parse('2026-08-29T04:00:00Z')
    const data = response('2026-08-29T12:05:00Z')

    expect(
      getAccountPoolServerNow(
        data.data.server_time,
        fastClientReceivedAt,
        fastClientReceivedAt + 60_000
      )
    ).toBe(Date.parse('2026-08-29T12:01:00Z'))
    expect(
      getAccountPoolRefetchInterval(
        data,
        fastClientReceivedAt,
        fastClientReceivedAt + 60_000
      )
    ).toBe(240_000)
    expect(
      getAccountPoolRefetchInterval(
        data,
        slowClientReceivedAt,
        slowClientReceivedAt + 60_000
      )
    ).toBe(240_000)
  })

  test('refreshes on visibility only after the server deadline', () => {
    const now = Date.parse('2026-08-29T12:00:00Z')
    expect(
      shouldRefreshAccountPoolOnVisibility('2026-08-29T11:59:59Z', now)
    ).toBe(true)
    expect(
      shouldRefreshAccountPoolOnVisibility('2026-08-29T12:00:01Z', now)
    ).toBe(false)
  })
})
