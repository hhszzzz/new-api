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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { Redemption } from '../../types'
import { RedemptionsMutateDrawer } from '../redemptions-mutate-drawer'

const {
  createRedemptionMock,
  getRedemptionMock,
  toastErrorMock,
  triggerRefreshMock,
  translateMock,
  updateRedemptionMock,
} = vi.hoisted(() => ({
  createRedemptionMock: vi.fn(),
  getRedemptionMock: vi.fn(),
  toastErrorMock: vi.fn(),
  triggerRefreshMock: vi.fn(),
  translateMock: (key: string) => key,
  updateRedemptionMock: vi.fn(),
}))

vi.mock('../../api', () => ({
  createRedemption: createRedemptionMock,
  getRedemption: getRedemptionMock,
  updateRedemption: updateRedemptionMock,
}))

vi.mock('../redemptions-provider', () => ({
  useRedemptions: () => ({ triggerRefresh: triggerRefreshMock }),
}))

vi.mock('@/components/datetime-picker', () => ({
  DateTimePicker: () => null,
}))

vi.mock('@/components/ui/sheet', () => ({
  Sheet: (props: { children: ReactNode; open: boolean }) =>
    props.open ? <div>{props.children}</div> : null,
  SheetClose: (props: { children: ReactNode }) => props.children,
  SheetContent: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SheetDescription: (props: { children: ReactNode }) => (
    <div>{props.children}</div>
  ),
  SheetFooter: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SheetHeader: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SheetTitle: (props: { children: ReactNode }) => <h1>{props.children}</h1>,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translateMock }),
}))

vi.mock('sonner', () => ({
  toast: { error: toastErrorMock, success: vi.fn() },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function redemption(id: number, name: string): Redemption {
  return {
    id,
    user_id: 1,
    name,
    key: `key-${id}`,
    status: 1,
    quota: 500000,
    created_time: 1,
    redeemed_time: 0,
    expired_time: 0,
    used_user_id: 0,
  }
}

function submitRedemptionForm() {
  const form = screen.getByLabelText('Name').closest('form')
  if (!form) throw new Error('redemption form not found')
  fireEvent.submit(form)
}

describe('redemption drawer load lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createRedemptionMock.mockReset()
    getRedemptionMock.mockReset()
    updateRedemptionMock.mockReset()
  })

  test('keeps the new record when the previous request resolves later', async () => {
    const first = deferred<{ success: boolean; data: Redemption }>()
    const second = deferred<{ success: boolean; data: Redemption }>()
    getRedemptionMock.mockImplementation((id: number) =>
      id === 1 ? first.promise : second.promise
    )
    const view = render(
      <RedemptionsMutateDrawer
        open
        onOpenChange={vi.fn()}
        currentRow={redemption(1, 'first')}
      />
    )
    expect(screen.getByLabelText('Name').closest('form')).toHaveAttribute(
      'inert'
    )

    view.rerender(
      <RedemptionsMutateDrawer
        open
        onOpenChange={vi.fn()}
        currentRow={redemption(2, 'second')}
      />
    )
    await act(async () => {
      second.resolve({ success: true, data: redemption(2, 'second loaded') })
      await second.promise
    })
    expect(screen.getByLabelText('Name')).toHaveValue('second loaded')
    expect(screen.getByLabelText('Name').closest('form')).not.toHaveAttribute(
      'inert'
    )

    await act(async () => {
      first.resolve({ success: true, data: redemption(1, 'stale first') })
      await first.promise
    })
    expect(screen.getByLabelText('Name')).toHaveValue('second loaded')
  })

  test('clears the previous record while a different record is loading', async () => {
    getRedemptionMock
      .mockResolvedValueOnce({
        success: true,
        data: redemption(1, 'first loaded'),
      })
      .mockImplementationOnce(() => new Promise(() => {}))
    const view = render(
      <RedemptionsMutateDrawer
        open
        onOpenChange={vi.fn()}
        currentRow={redemption(1, 'first')}
      />
    )
    expect(await screen.findByDisplayValue('first loaded')).toBeEnabled()

    view.rerender(
      <RedemptionsMutateDrawer
        open
        onOpenChange={vi.fn()}
        currentRow={redemption(2, 'second')}
      />
    )

    expect(screen.getByLabelText('Name').closest('form')).toHaveAttribute(
      'inert'
    )
    expect(screen.getByLabelText('Name')).toHaveValue('')
  })

  test('does not let an obsolete save close a reopened drawer', async () => {
    const save = deferred<{ success: boolean }>()
    getRedemptionMock.mockResolvedValue({
      success: true,
      data: redemption(1, 'loaded redemption'),
    })
    updateRedemptionMock.mockImplementationOnce(() => save.promise)
    const onOpenChange = vi.fn()
    const renderDrawer = (open: boolean) => (
      <RedemptionsMutateDrawer
        open={open}
        onOpenChange={onOpenChange}
        currentRow={redemption(1, 'row redemption')}
      />
    )
    const view = render(renderDrawer(true))
    expect(await screen.findByDisplayValue('loaded redemption')).toBeEnabled()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()
    })

    submitRedemptionForm()
    await waitFor(() => expect(updateRedemptionMock).toHaveBeenCalledOnce())

    view.rerender(renderDrawer(false))
    view.rerender(renderDrawer(true))
    expect(await screen.findByDisplayValue('loaded redemption')).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()

    await act(async () => {
      save.resolve({ success: true })
      await save.promise
    })

    expect(onOpenChange).not.toHaveBeenCalled()
    expect(triggerRefreshMock).not.toHaveBeenCalled()
  })

  test('shows the server error when an update is rejected', async () => {
    getRedemptionMock.mockResolvedValue({
      success: true,
      data: redemption(1, 'loaded redemption'),
    })
    updateRedemptionMock.mockResolvedValue({
      success: false,
      message: 'update rejected',
    })
    render(
      <RedemptionsMutateDrawer
        open
        onOpenChange={vi.fn()}
        currentRow={redemption(1, 'row redemption')}
      />
    )
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()
    })

    submitRedemptionForm()

    await waitFor(() => {
      expect(toastErrorMock).toHaveBeenCalledWith('update rejected')
    })
  })
})
