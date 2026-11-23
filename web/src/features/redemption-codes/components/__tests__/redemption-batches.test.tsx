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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import type { RedemptionBatch, RedemptionBatchDetail } from '../../types'
import { RedemptionBatches } from '../redemption-batches'

const apiMocks = vi.hoisted(() => ({
  disableUnusedRedemptionBatch: vi.fn(),
  exportRedemptionBatch: vi.fn(),
  getRedemptionBatch: vi.fn(),
  getRedemptionBatches: vi.fn(),
}))

vi.mock('../../api', () => apiMocks)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string | number>) =>
      Object.entries(params ?? {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        key
      ),
  }),
}))

const batch: RedemptionBatch = {
  id: 4,
  name: 'launch',
  type: 'balance',
  created_by: 1,
  created_at: 1_700_000_000,
  expired_time: 0,
  count: 2,
  quota: 500,
  rpm_delta: 0,
  concurrency_delta: 0,
  validity_seconds: 0,
  available_count: 2,
  used_count: 0,
  disabled_count: 0,
  expired_count: 0,
}

const batchDetail: RedemptionBatchDetail = {
  ...batch,
  redemptions: [
    {
      id: 9,
      user_id: 1,
      name: 'launch',
      key: 'launch-code-9',
      status: 1,
      quota: 500,
      type: 'balance',
      batch_id: 4,
      created_time: 1_700_000_000,
      redeemed_time: 0,
      expired_time: 0,
      used_user_id: 0,
    },
  ],
}

function renderBatches() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <RedemptionBatches />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.clearAllMocks()
})

test('shows batch details, disables unused codes and paginates batches', async () => {
  apiMocks.getRedemptionBatches.mockImplementation(
    async ({ p }: { p: number }) => ({
      success: true,
      data: { items: [{ ...batch, name: `launch-${p}` }], total: 51 },
    })
  )
  apiMocks.getRedemptionBatch.mockResolvedValue({
    success: true,
    data: batchDetail,
  })
  apiMocks.disableUnusedRedemptionBatch.mockResolvedValue({
    success: true,
    data: 2,
  })
  apiMocks.exportRedemptionBatch.mockResolvedValue(undefined)

  const user = userEvent.setup()
  renderBatches()

  const viewButton = await screen.findByRole('button', {
    name: 'View batch details',
  })
  await user.click(viewButton)
  expect(await screen.findByText('launch-code-9')).toBeInTheDocument()
  expect(screen.getByText('Count: 2')).toBeInTheDocument()
  expect(screen.getByText('Expires: Never')).toBeInTheDocument()
  expect(apiMocks.getRedemptionBatch).toHaveBeenCalledWith(4)
  await user.click(screen.getByRole('button', { name: 'Close' }))

  await user.click(screen.getByRole('button', { name: 'Disable unused codes' }))
  const confirmation = await screen.findByRole('alertdialog')
  expect(confirmation).toHaveTextContent(
    'This disables all unused codes in batch launch-1.'
  )
  await user.click(
    within(confirmation).getByRole('button', { name: 'Disable unused codes' })
  )
  await waitFor(() =>
    expect(apiMocks.disableUnusedRedemptionBatch.mock.calls[0]?.[0]).toBe(4)
  )
  await waitFor(() =>
    expect(apiMocks.getRedemptionBatches.mock.calls.length).toBeGreaterThan(1)
  )
  fireEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  expect(await screen.findByText('launch-2')).toBeInTheDocument()
  expect(apiMocks.getRedemptionBatches).toHaveBeenLastCalledWith({
    p: 2,
    page_size: 50,
  })
})

test('shows a retry action when batch loading fails', async () => {
  apiMocks.getRedemptionBatches.mockRejectedValueOnce(new Error('offline'))

  renderBatches()

  const retryButton = await screen.findByRole('button', { name: 'Retry' })
  fireEvent.click(retryButton)
  await waitFor(() => expect(apiMocks.getRedemptionBatches).toHaveBeenCalled())
})

test('allows disabling a batch whose unused codes have expired', async () => {
  apiMocks.getRedemptionBatches.mockResolvedValue({
    success: true,
    data: {
      items: [{ ...batch, available_count: 0, expired_count: 2 }],
      total: 1,
    },
  })

  renderBatches()

  expect(
    await screen.findByRole('button', { name: 'Disable unused codes' })
  ).toBeInTheDocument()
})
