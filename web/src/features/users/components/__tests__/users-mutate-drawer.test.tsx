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

import type { User, UserPolicy } from '../../types'
import { UsersMutateDrawer } from '../users-mutate-drawer'

const {
  createUserMock,
  getUserMock,
  getUserPolicyMock,
  triggerRefreshMock,
  translateMock,
  updateUserMock,
} = vi.hoisted(() => ({
  createUserMock: vi.fn(),
  getUserMock: vi.fn(),
  getUserPolicyMock: vi.fn(),
  triggerRefreshMock: vi.fn(),
  translateMock: (key: string) => key,
  updateUserMock: vi.fn(),
}))

vi.mock('../../api', () => ({
  createUser: createUserMock,
  getGroups: vi.fn().mockResolvedValue({ success: true, data: ['default'] }),
  getPermissionCatalog: vi.fn().mockResolvedValue({ success: true, data: {} }),
  getUser: getUserMock,
  getUserPolicy: getUserPolicyMock,
  updateUser: updateUserMock,
}))

vi.mock('@/features/pricing/api', () => ({
  getPricing: vi.fn().mockResolvedValue({ success: true, data: [] }),
}))

vi.mock('../users-provider', () => ({
  useUsers: () => ({ triggerRefresh: triggerRefreshMock }),
}))

vi.mock('../user-quota-dialog', () => ({
  UserQuotaDialog: () => null,
}))

vi.mock('@/components/multi-select', () => ({
  MultiSelect: () => <div />,
}))

vi.mock('@/components/ui/select', () => ({
  Select: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SelectContent: (props: { children: ReactNode }) => (
    <div>{props.children}</div>
  ),
  SelectGroup: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SelectItem: (props: { children: ReactNode }) => <div>{props.children}</div>,
  SelectTrigger: (props: { children: ReactNode }) => (
    <button type='button'>{props.children}</button>
  ),
  SelectValue: () => null,
}))

vi.mock('@/components/ui/switch', () => ({
  Switch: () => <input type='checkbox' />,
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

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (
    selector: (state: { auth: { user: { role: number } } }) => unknown
  ) => selector({ auth: { user: { role: 100 } } }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translateMock }),
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function user(id: number, displayName: string): User {
  return {
    id,
    username: `user-${id}`,
    display_name: displayName,
    quota: 500000,
    used_quota: 0,
    request_count: 0,
    group: 'default',
    groups: ['default'],
    status: 1,
    role: 1,
  }
}

function policy(): UserPolicy {
  return {
    groups: ['default'],
    primary_group: 'default',
    topup_group: 'default',
    model_limits_enabled: false,
    model_limits: [],
    model_blocklist_enabled: false,
    model_blocklist: [],
  }
}

describe('user drawer load lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createUserMock.mockReset()
    getUserMock.mockReset()
    getUserPolicyMock.mockReset()
    updateUserMock.mockReset()
  })

  test('keeps the new user when the previous request resolves later', async () => {
    const firstUser = deferred<{ success: boolean; data: User }>()
    const firstPolicy = deferred<{ success: boolean; data: UserPolicy }>()
    const secondUser = deferred<{ success: boolean; data: User }>()
    const secondPolicy = deferred<{ success: boolean; data: UserPolicy }>()
    getUserMock.mockImplementation((id: number) =>
      id === 1 ? firstUser.promise : secondUser.promise
    )
    getUserPolicyMock.mockImplementation((id: number) =>
      id === 1 ? firstPolicy.promise : secondPolicy.promise
    )
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={queryClient}>
        <UsersMutateDrawer
          open
          onOpenChange={vi.fn()}
          currentRow={user(1, 'first')}
        />
      </QueryClientProvider>
    )
    expect(
      screen.getByLabelText('Display Name').closest('form')
    ).toHaveAttribute('inert')

    view.rerender(
      <QueryClientProvider client={queryClient}>
        <UsersMutateDrawer
          open
          onOpenChange={vi.fn()}
          currentRow={user(2, 'second')}
        />
      </QueryClientProvider>
    )
    await act(async () => {
      secondUser.resolve({ success: true, data: user(2, 'second loaded') })
      secondPolicy.resolve({ success: true, data: policy() })
      await Promise.all([secondUser.promise, secondPolicy.promise])
    })
    expect(screen.getByLabelText('Display Name')).toHaveValue('second loaded')
    expect(
      screen.getByLabelText('Display Name').closest('form')
    ).not.toHaveAttribute('inert')

    await act(async () => {
      firstUser.resolve({ success: true, data: user(1, 'stale first') })
      firstPolicy.resolve({ success: true, data: policy() })
      await Promise.all([firstUser.promise, firstPolicy.promise])
    })
    expect(screen.getByLabelText('Display Name')).toHaveValue('second loaded')
  })

  test('clears the previous user values while a different user is loading', async () => {
    getUserMock
      .mockResolvedValueOnce({ success: true, data: user(1, 'first loaded') })
      .mockImplementationOnce(() => new Promise(() => {}))
    getUserPolicyMock
      .mockResolvedValueOnce({ success: true, data: policy() })
      .mockImplementationOnce(() => new Promise(() => {}))
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={queryClient}>
        <UsersMutateDrawer
          open
          onOpenChange={vi.fn()}
          currentRow={user(1, 'first')}
        />
      </QueryClientProvider>
    )
    expect(await screen.findByDisplayValue('first loaded')).toBeEnabled()

    view.rerender(
      <QueryClientProvider client={queryClient}>
        <UsersMutateDrawer
          open
          onOpenChange={vi.fn()}
          currentRow={user(2, 'second')}
        />
      </QueryClientProvider>
    )

    expect(
      screen.getByLabelText('Display Name').closest('form')
    ).toHaveAttribute('inert')
    expect(screen.getByLabelText('Display Name')).toHaveValue('')
  })

  test('does not let an obsolete save close a reopened drawer', async () => {
    const userEventSession = userEvent.setup()
    const save = deferred<{ success: boolean }>()
    getUserMock.mockResolvedValue({
      success: true,
      data: user(1, 'loaded user'),
    })
    getUserPolicyMock.mockResolvedValue({ success: true, data: policy() })
    updateUserMock.mockImplementationOnce(() => save.promise)
    const onOpenChange = vi.fn()
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const renderDrawer = (open: boolean) => (
      <QueryClientProvider client={queryClient}>
        <UsersMutateDrawer
          open={open}
          onOpenChange={onOpenChange}
          currentRow={user(1, 'row user')}
        />
      </QueryClientProvider>
    )
    const view = render(renderDrawer(true))
    expect(await screen.findByDisplayValue('loaded user')).toBeEnabled()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()
    })

    await userEventSession.click(
      screen.getByRole('button', { name: 'Save changes' })
    )
    await waitFor(() => expect(updateUserMock).toHaveBeenCalledOnce())

    view.rerender(renderDrawer(false))
    view.rerender(renderDrawer(true))
    expect(await screen.findByDisplayValue('loaded user')).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()

    await act(async () => {
      save.resolve({ success: true })
      await save.promise
    })

    expect(onOpenChange).not.toHaveBeenCalled()
    expect(triggerRefreshMock).not.toHaveBeenCalled()
  })
})
