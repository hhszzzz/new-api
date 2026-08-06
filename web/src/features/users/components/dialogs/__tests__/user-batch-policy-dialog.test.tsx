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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { UserBatchPolicyDialog } from '../user-batch-policy-dialog'

const { batchUpdateUserPolicyMock, getPricingMock, translateMock } = vi.hoisted(
  () => ({
    batchUpdateUserPolicyMock: vi.fn(),
    getPricingMock: vi.fn(),
    translateMock: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        key
      ),
  })
)

vi.mock('../../../api', () => ({
  batchUpdateUserPolicy: batchUpdateUserPolicyMock,
}))

vi.mock('@/features/pricing/api', () => ({
  getPricing: getPricingMock,
}))

vi.mock('@/components/dialog', () => ({
  Dialog: (props: {
    children: ReactNode
    footer: ReactNode
    open: boolean
    title: ReactNode
  }) =>
    props.open ? (
      <div role='dialog'>
        <h1>{props.title}</h1>
        {props.children}
        {props.footer}
      </div>
    ) : null,
}))

vi.mock('@/components/ui/select', () => ({
  Select: (props: {
    children: ReactNode
    onValueChange: (value: string) => void
    value: string
  }) => (
    <select
      value={props.value}
      onChange={(event) => props.onValueChange(event.target.value)}
    >
      {props.children}
    </select>
  ),
  SelectContent: (props: { children: ReactNode }) => props.children,
  SelectItem: (props: { children: ReactNode; value: string }) => (
    <option value={props.value}>{props.children}</option>
  ),
  SelectTrigger: () => null,
  SelectValue: () => null,
}))

vi.mock('@/components/multi-select', () => ({
  MultiSelect: (props: { onChange: (models: string[]) => void }) => (
    <button type='button' onClick={() => props.onChange(['gpt-4o'])}>
      Select one model
    </button>
  ),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translateMock }),
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

describe('user batch policy dialog target lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getPricingMock.mockResolvedValue({ success: true, data: [] })
  })

  test('resets state and ignores an obsolete submit after the target changes', async () => {
    const user = userEvent.setup()
    const pending = deferred<{
      success: boolean
      data: { updated: number; skipped: [] }
    }>()
    batchUpdateUserPolicyMock.mockReturnValue(pending.promise)
    const onOpenChange = vi.fn()
    const onSuccess = vi.fn()
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={queryClient}>
        <UserBatchPolicyDialog
          open
          onOpenChange={onOpenChange}
          userIds={[1]}
          onSuccess={onSuccess}
        />
      </QueryClientProvider>
    )

    const listMode = screen.getAllByRole('combobox')[0]
    await user.selectOptions(listMode, 'replace')
    await user.click(screen.getByRole('button', { name: 'Select one model' }))
    await user.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() =>
      expect(batchUpdateUserPolicyMock).toHaveBeenCalledOnce()
    )

    view.rerender(
      <QueryClientProvider client={queryClient}>
        <UserBatchPolicyDialog
          open
          onOpenChange={onOpenChange}
          userIds={[2]}
          onSuccess={onSuccess}
        />
      </QueryClientProvider>
    )
    await waitFor(() =>
      expect(screen.getAllByRole('combobox')[0]).toHaveValue('keep')
    )

    await act(async () => {
      pending.resolve({ success: true, data: { updated: 1, skipped: [] } })
      await pending.promise
    })

    expect(onOpenChange).not.toHaveBeenCalled()
    expect(onSuccess).not.toHaveBeenCalled()
  })
})
