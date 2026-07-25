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
  createUserSubscription,
  deleteUserSubscription,
  getAdminPlans,
  getUserSubscriptions,
  invalidateUserSubscription,
  resetUserSubscriptionsByPlan,
} from '../../../api'
import { UserSubscriptionsDialog } from '../user-subscriptions-dialog'

const toastError = vi.fn()
const toastSuccess = vi.fn()

vi.mock('../../../api', () => ({
  createUserSubscription: vi.fn(),
  deleteUserSubscription: vi.fn(),
  getAdminPlans: vi.fn(),
  getUserSubscriptions: vi.fn(),
  invalidateUserSubscription: vi.fn(),
  resetUserSubscriptionsByPlan: vi.fn(),
}))

vi.mock('react-i18next', () => {
  const translate = (key: string) => key
  return {
    useTranslation: () => ({ t: translate }),
  }
})

vi.mock('sonner', () => ({
  toast: {
    error: (...args: unknown[]) => toastError(...args),
    success: (...args: unknown[]) => toastSuccess(...args),
  },
}))

vi.mock('@/components/confirm-dialog', () => ({
  ConfirmDialog: (props: {
    open: boolean
    title: string
    desc?: ReactNode
    confirmText?: ReactNode
    handleConfirm: () => void | Promise<void>
    children?: ReactNode
  }) =>
    props.open ? (
      <div role='dialog' aria-label={props.title}>
        {props.desc}
        {props.children}
        <button type='button' onClick={() => void props.handleConfirm()}>
          {props.confirmText || 'Confirm'}
        </button>
      </div>
    ) : null,
}))

vi.mock('@/components/data-table', () => ({
  DataTableRowActionMenu: (props: { children: ReactNode }) => props.children,
  StaticDataTable: (props: {
    data?: Array<{ subscription: { id: number } }>
    columns?: Array<{
      id: string
      cell: (record: { subscription: { id: number } }) => ReactNode
    }>
    emptyContent?: ReactNode
  }) => {
    const data = props.data ?? []
    if (data.length === 0) return <div>{props.emptyContent}</div>

    return (
      <div>
        {data.map((record) => (
          <div key={record.subscription.id}>
            {(props.columns ?? []).map((column) => (
              <div key={column.id}>{column.cell(record)}</div>
            ))}
          </div>
        ))}
      </div>
    )
  },
}))

vi.mock('@/components/drawer-layout', () => ({
  sideDrawerContentClassName: () => '',
  sideDrawerFormClassName: () => '',
  sideDrawerHeaderClassName: () => '',
}))

vi.mock('@/components/status-badge', () => ({
  StatusBadge: (props: { label: string }) => <span>{props.label}</span>,
}))

vi.mock('@/components/table-id', () => ({
  TableId: (props: { value: number }) => <span>{props.value}</span>,
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

vi.mock('@/components/ui/dropdown-menu', () => ({
  DropdownMenuItem: (props: {
    children: ReactNode
    disabled?: boolean
    onClick?: () => void
  }) => (
    <button type='button' disabled={props.disabled} onClick={props.onClick}>
      {props.children}
    </button>
  ),
  DropdownMenuSeparator: () => null,
  DropdownMenuShortcut: (props: { children: ReactNode }) => props.children,
}))

vi.mock('@/components/ui/input', () => ({
  Input: (props: {
    value: string
    onChange: (event: { target: { value: string } }) => void
    'aria-label'?: string
    disabled?: boolean
  }) => (
    <input
      value={props.value}
      aria-label={props['aria-label']}
      disabled={props.disabled}
      onChange={(event) => props.onChange(event)}
    />
  ),
}))

vi.mock('@/components/ui/select', () => ({
  Select: (props: {
    items?: Array<{ value: string; label: ReactNode }>
    value: string
    onValueChange: (value: string | null) => void
  }) => (
    <select
      aria-label='Subscription plan'
      value={props.value}
      onChange={(event) => props.onValueChange(event.currentTarget.value)}
    >
      <option value=''>Select subscription plan</option>
      {(props.items ?? []).map((item) => (
        <option key={item.value} value={item.value}>
          {item.label}
        </option>
      ))}
    </select>
  ),
  SelectContent: () => null,
  SelectGroup: () => null,
  SelectItem: () => null,
  SelectTrigger: () => null,
  SelectValue: () => null,
}))

vi.mock('@/components/ui/sheet', () => ({
  Sheet: (props: { open: boolean; children: ReactNode }) =>
    props.open ? <div>{props.children}</div> : null,
  SheetContent: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SheetHeader: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SheetTitle: (props: { children: ReactNode }) => <h2>{props.children}</h2>,
  SheetDescription: (props: { children: ReactNode }) => <p>{props.children}</p>,
}))

vi.mock('@/components/ui/switch', () => ({
  Switch: (props: {
    checked?: boolean
    onCheckedChange?: (checked: boolean) => void
    'aria-label'?: string
  }) => (
    <button
      type='button'
      role='switch'
      aria-checked={!!props.checked}
      aria-label={props['aria-label']}
      onClick={() => props.onCheckedChange?.(!props.checked)}
    />
  ),
}))

const mockedCreateSubscription = vi.mocked(createUserSubscription)
const mockedDeleteSubscription = vi.mocked(deleteUserSubscription)
const mockedGetAdminPlans = vi.mocked(getAdminPlans)
const mockedGetUserSubscriptions = vi.mocked(getUserSubscriptions)
const mockedInvalidateSubscription = vi.mocked(invalidateUserSubscription)
const mockedResetSubscriptions = vi.mocked(resetUserSubscriptionsByPlan)

const activeSubscription = {
  subscription: {
    id: 11,
    user_id: 7,
    plan_id: 1,
    status: 'active',
    source: 'admin',
    source_note: 'manual grant',
    start_time: 1,
    end_time: 4_102_444_800,
    amount_total: 0,
    amount_used: 0,
  },
}

beforeEach(() => {
  toastError.mockReset()
  toastSuccess.mockReset()
  mockedCreateSubscription.mockReset()
  mockedDeleteSubscription.mockReset()
  mockedGetAdminPlans.mockReset()
  mockedGetUserSubscriptions.mockReset()
  mockedInvalidateSubscription.mockReset()
  mockedResetSubscriptions.mockReset()

  mockedGetAdminPlans.mockResolvedValue({
    success: true,
    data: [
      {
        plan: {
          id: 1,
          title: 'Internal plan',
          price_amount: 0,
          currency: 'USD',
          duration_unit: 'month',
          duration_value: 1,
          quota_reset_period: 'never',
          enabled: true,
          purchasable: false,
          sort_order: 0,
          allow_balance_pay: true,
          allow_wallet_overflow: true,
          max_purchase_per_user: 0,
          total_amount: 0,
        },
      },
    ],
  })
  mockedGetUserSubscriptions.mockResolvedValue({ success: true, data: [] })
})

describe('user subscriptions dialog', () => {
  test('keeps enabled internal plans assignable and requires an administrator note', async () => {
    const user = userEvent.setup()

    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 7, username: 'subscription-user' }}
      />
    )

    const planSelect = await screen.findByLabelText('Subscription plan')
    expect(
      screen.getByRole('option', { name: 'Internal plan($0.00)' })
    ).toBeInTheDocument()

    await user.selectOptions(planSelect, '1')
    expect(
      screen.getByRole('button', { name: 'Add subscription' })
    ).toBeDisabled()

    await user.type(
      screen.getByLabelText('Administrator assignment note'),
      'manual grant'
    )
    expect(
      screen.getByRole('button', { name: 'Add subscription' })
    ).toBeEnabled()
  })

  test('requires confirmation before adding a duplicate active subscription', async () => {
    mockedGetUserSubscriptions.mockResolvedValue({
      success: true,
      data: [activeSubscription],
    })
    mockedCreateSubscription.mockResolvedValue({
      success: true,
      data: { message: 'Assigned' },
    })
    const user = userEvent.setup()

    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 7, username: 'subscription-user' }}
      />
    )

    await user.selectOptions(
      await screen.findByLabelText('Subscription plan'),
      '1'
    )
    await user.type(
      screen.getByLabelText('Administrator assignment note'),
      'second grant'
    )
    await user.click(screen.getByRole('button', { name: 'Add subscription' }))

    const confirmDialog = await screen.findByRole('dialog', {
      name: 'Add another subscription',
    })
    expect(mockedCreateSubscription).not.toHaveBeenCalled()

    await user.click(
      within(confirmDialog).getByRole('button', { name: 'Add subscription' })
    )
    await waitFor(() => {
      expect(mockedCreateSubscription).toHaveBeenCalledWith(7, {
        plan_id: 1,
        source_note: 'second grant',
      })
    })
  })

  test('shows the server message when administrator assignment is rejected', async () => {
    mockedCreateSubscription.mockResolvedValue({
      success: false,
      message: 'Assignment rejected by policy',
    })
    const user = userEvent.setup()

    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 7, username: 'subscription-user' }}
      />
    )

    await user.selectOptions(
      await screen.findByLabelText('Subscription plan'),
      '1'
    )
    await user.type(
      screen.getByLabelText('Administrator assignment note'),
      'manual grant'
    )
    await user.click(screen.getByRole('button', { name: 'Add subscription' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('Assignment rejected by policy')
    })
    expect(toastSuccess).not.toHaveBeenCalled()
    expect(mockedCreateSubscription).toHaveBeenCalledWith(7, {
      plan_id: 1,
      source_note: 'manual grant',
    })
    expect(mockedGetUserSubscriptions).toHaveBeenCalledTimes(1)
  })

  test('shows the server message when invalidation is rejected', async () => {
    mockedGetUserSubscriptions.mockResolvedValue({
      success: true,
      data: [activeSubscription],
    })
    mockedInvalidateSubscription.mockResolvedValue({
      success: false,
      message: 'Invalidation rejected by policy',
    })
    const user = userEvent.setup()

    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 7, username: 'subscription-user' }}
      />
    )

    await user.click(await screen.findByRole('button', { name: 'Invalidate' }))
    const confirmDialog = await screen.findByRole('dialog', {
      name: 'Confirm invalidate',
    })
    await user.click(
      within(confirmDialog).getByRole('button', { name: 'Confirm' })
    )

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('Invalidation rejected by policy')
    })
    expect(toastSuccess).not.toHaveBeenCalled()
  })

  test('shows the server message when deletion is rejected', async () => {
    mockedGetUserSubscriptions.mockResolvedValue({
      success: true,
      data: [activeSubscription],
    })
    mockedDeleteSubscription.mockResolvedValue({
      success: false,
      message: 'Deletion rejected by policy',
    })
    const user = userEvent.setup()

    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 7, username: 'subscription-user' }}
      />
    )

    await user.click(await screen.findByRole('button', { name: 'Delete' }))
    const confirmDialog = await screen.findByRole('dialog', {
      name: 'Confirm delete',
    })
    await user.click(
      within(confirmDialog).getByRole('button', { name: 'Confirm' })
    )

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('Deletion rejected by policy')
    })
    expect(toastSuccess).not.toHaveBeenCalled()
  })

  test('shows the server message when quota reset is rejected', async () => {
    mockedGetUserSubscriptions.mockResolvedValue({
      success: true,
      data: [activeSubscription],
    })
    mockedResetSubscriptions.mockResolvedValue({
      success: false,
      message: 'Reset rejected by policy',
    })
    const user = userEvent.setup()

    render(
      <UserSubscriptionsDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 7, username: 'subscription-user' }}
      />
    )

    await user.click(await screen.findByRole('button', { name: 'Reset quota' }))
    const confirmDialog = await screen.findByRole('dialog', {
      name: 'Reset subscription quota',
    })
    await user.click(
      within(confirmDialog).getByRole('button', { name: 'Reset quota' })
    )

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('Reset rejected by policy')
    })
    expect(toastSuccess).not.toHaveBeenCalled()
  })
})
