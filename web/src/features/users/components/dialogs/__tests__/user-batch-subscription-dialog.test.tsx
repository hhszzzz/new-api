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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import {
  batchAssignUserSubscriptions,
  batchResetUserSubscriptions,
  batchRevokeUserSubscriptions,
} from '@/features/subscriptions/api'

import { UserBatchSubscriptionDialog } from '../user-batch-subscription-dialog'

const { plansData } = vi.hoisted(() => ({
  plansData: {
    success: true,
    data: [
      {
        plan: { id: 1, title: 'Pro', price_amount: 9, enabled: true },
      },
    ],
  },
}))

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({ data: plansData }),
}))

vi.mock('@/features/subscriptions/api', () => ({
  getAdminPlans: vi.fn(),
  batchAssignUserSubscriptions: vi.fn(),
  batchRevokeUserSubscriptions: vi.fn(),
  batchResetUserSubscriptions: vi.fn(),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

const toastSuccess = vi.fn()
const toastWarning = vi.fn()
const toastError = vi.fn()
vi.mock('sonner', () => ({
  toast: {
    success: (...args: unknown[]) => toastSuccess(...args),
    warning: (...args: unknown[]) => toastWarning(...args),
    error: (...args: unknown[]) => toastError(...args),
  },
}))

vi.mock('@/components/dialog', () => ({
  Dialog: (props: {
    open: boolean
    title: string
    description?: ReactNode
    children?: ReactNode
    footer?: ReactNode
  }) =>
    props.open ? (
      <div role='dialog' aria-label={props.title}>
        <div>{props.description}</div>
        {props.children}
        {props.footer}
      </div>
    ) : null,
}))

vi.mock('@/components/confirm-dialog', () => ({
  ConfirmDialog: (props: {
    open: boolean
    title: string
    desc?: ReactNode
    handleConfirm: () => void | Promise<void>
  }) =>
    props.open ? (
      <div role='dialog' aria-label={props.title}>
        {props.desc}
        <button type='button' onClick={() => void props.handleConfirm()}>
          Confirm
        </button>
      </div>
    ) : null,
}))

vi.mock('@/components/ui/button', () => ({
  Button: (props: {
    children: ReactNode
    disabled?: boolean
    onClick?: () => void
  }) => (
    <button type='button' disabled={props.disabled} onClick={props.onClick}>
      {props.children}
    </button>
  ),
}))

vi.mock('@/components/ui/combobox', () => ({
  Combobox: (props: {
    options: Array<{ value: string; label: string }>
    value: string
    onValueChange: (value: string | null) => void
    placeholder?: string
  }) => (
    <select
      aria-label={props.placeholder}
      value={props.value}
      onChange={(event) => props.onValueChange(event.target.value)}
    >
      <option value='' />
      {props.options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  ),
}))

vi.mock('@/components/ui/input', () => ({
  Input: (props: {
    id?: string
    value: string
    onChange: (event: { target: { value: string } }) => void
  }) => <input id={props.id} value={props.value} onChange={props.onChange} />,
}))

vi.mock('@/components/ui/label', () => ({
  Label: (props: { children: ReactNode }) => <label>{props.children}</label>,
}))

vi.mock('@/components/ui/select', () => ({
  Select: (props: {
    value: string
    onValueChange: (value: string) => void
    children?: ReactNode
  }) => (
    <select
      aria-label='Operation'
      value={props.value}
      onChange={(event) => props.onValueChange(event.target.value)}
    >
      {props.children}
    </select>
  ),
  SelectTrigger: (props: { children?: ReactNode }) => props.children,
  SelectValue: () => null,
  SelectContent: (props: { children?: ReactNode }) => props.children,
  SelectItem: (props: { value: string; children?: ReactNode }) => (
    <option value={props.value}>{props.children}</option>
  ),
}))

vi.mock('@/components/ui/switch', () => ({
  Switch: (props: { id?: string; checked: boolean }) => (
    <input id={props.id} type='checkbox' checked={props.checked} readOnly />
  ),
}))

const mockedAssign = vi.mocked(batchAssignUserSubscriptions)
const mockedRevoke = vi.mocked(batchRevokeUserSubscriptions)
const mockedReset = vi.mocked(batchResetUserSubscriptions)

const renderDialog = () =>
  render(
    <UserBatchSubscriptionDialog
      open
      onOpenChange={() => undefined}
      userIds={[1, 2]}
    />
  )

const selectPlan = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.selectOptions(
    screen.getByLabelText('Select subscription plan'),
    '1'
  )
}

beforeEach(() => {
  mockedAssign.mockReset()
  mockedRevoke.mockReset()
  mockedReset.mockReset()
  toastSuccess.mockReset()
  toastWarning.mockReset()
  toastError.mockReset()
  mockedAssign.mockResolvedValue({ success: true, data: { updated: 2, skipped: [] } })
  mockedRevoke.mockResolvedValue({ success: true, data: { updated: 2, revoked: 2, skipped: [] } })
  mockedReset.mockResolvedValue({ success: true, data: { updated: 2, reset_count: 2, skipped: [] } })
})

describe('user batch subscription dialog', () => {
  test('assigns the selected plan to every user by default', async () => {
    const user = userEvent.setup()
    renderDialog()

    await selectPlan(user)
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(mockedAssign).toHaveBeenCalledWith({
        user_ids: [1, 2],
        plan_id: 1,
        source_note: '',
      })
    })
    expect(mockedRevoke).not.toHaveBeenCalled()
  })

  test('invalidates matching subscriptions without confirmation', async () => {
    const user = userEvent.setup()
    renderDialog()

    await user.selectOptions(screen.getByLabelText('Operation'), 'invalidate')
    await selectPlan(user)
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(mockedRevoke).toHaveBeenCalledWith({
        user_ids: [1, 2],
        plan_id: 1,
        action: 'invalidate',
      })
    })
  })

  test('requires confirmation before deleting matching subscriptions', async () => {
    const user = userEvent.setup()
    renderDialog()

    await user.selectOptions(screen.getByLabelText('Operation'), 'delete')
    await selectPlan(user)
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(mockedRevoke).not.toHaveBeenCalled()
    const confirmDialog = await screen.findByRole('dialog', {
      name: 'Confirm batch subscription deletion',
    })
    await user.click(within(confirmDialog).getByRole('button', { name: 'Confirm' }))

    await waitFor(() => {
      expect(mockedRevoke).toHaveBeenCalledWith({
        user_ids: [1, 2],
        plan_id: 1,
        action: 'delete',
      })
    })
  })

  test('resets usage for the selected plan', async () => {
    const user = userEvent.setup()
    renderDialog()

    await user.selectOptions(screen.getByLabelText('Operation'), 'reset')
    await selectPlan(user)
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(mockedReset).toHaveBeenCalledWith({
        user_ids: [1, 2],
        plan_id: 1,
        advance_reset_time: true,
      })
    })
  })
})
