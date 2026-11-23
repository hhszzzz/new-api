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
import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { useRedemption } from './use-redemption'

const mocks = vi.hoisted(() => ({
  getSelf: vi.fn(),
  redeemTopupCode: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
}))

vi.mock('../api', () => ({ redeemTopupCode: mocks.redeemTopupCode }))
vi.mock('@/lib/api', () => ({ getSelf: mocks.getSelf }))
vi.mock('sonner', () => ({
  toast: { error: mocks.toastError, success: mocks.toastSuccess },
}))
vi.mock('@/lib/format', () => ({
  formatQuota: (quota: number) => `quota:${quota}`,
  formatTimestampToDate: (timestamp: number) => `date:${timestamp}`,
}))
vi.mock('i18next', () => ({
  default: {
    t: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values ?? {}).reduce(
        (message, [name, value]) =>
          message.replace(`{{${name}}}`, String(value)),
        key
      ),
  },
}))

afterEach(() => vi.clearAllMocks())

test('keeps the legacy balance response and shows full entitlement results', async () => {
  mocks.getSelf.mockResolvedValue({ success: true })
  mocks.redeemTopupCode
    .mockResolvedValueOnce({ success: true, data: 250 })
    .mockResolvedValueOnce({
      success: true,
      data: {
        type: 'subscription',
        plan_title: 'Starter',
        subscription_end_at: 1_800_000_000,
      },
    })
    .mockResolvedValueOnce({
      success: true,
      data: {
        type: 'rate_limit',
        rpm_delta: 20,
        concurrency_delta: 2,
        expires_at: 1_700_000_100,
      },
    })

  const { result } = renderHook(() => useRedemption())
  await act(async () => {
    await result.current.redeemCode('balance-code')
    await result.current.redeemCode('subscription-code')
    await result.current.redeemCode('rate-code')
  })

  expect(mocks.toastSuccess).toHaveBeenNthCalledWith(
    1,
    'Redemption successful! Added: quota:250'
  )
  expect(mocks.toastSuccess).toHaveBeenNthCalledWith(
    2,
    'Subscription code applied: Starter until date:1800000000'
  )
  expect(mocks.toastSuccess).toHaveBeenNthCalledWith(
    3,
    'Rate-limit bonus: RPM +20, concurrency +2 until date:1700000100'
  )
  expect(mocks.getSelf).toHaveBeenCalledTimes(3)
})
