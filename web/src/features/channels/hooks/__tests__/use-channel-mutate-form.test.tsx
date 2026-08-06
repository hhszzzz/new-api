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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { ChannelFormValues } from '../../lib'
import type { Channel } from '../../types'
import { useChannelMutateForm } from '../use-channel-mutate-form'

const { toastErrorMock, toastSuccessMock, updateChannelMock } = vi.hoisted(
  () => ({
    toastErrorMock: vi.fn(),
    toastSuccessMock: vi.fn(),
    updateChannelMock: vi.fn(),
  })
)

vi.mock('../../api', () => ({
  createChannel: vi.fn(),
  updateChannel: updateChannelMock,
}))

vi.mock('../../lib', () => ({
  transformFormDataToCreatePayload: vi.fn((data) => data),
  transformFormDataToUpdatePayload: vi.fn((data) => data),
}))

vi.mock('@/lib/admin-permissions', () => ({
  ADMIN_PERMISSION_ACTIONS: { SENSITIVE_WRITE: 'sensitive_write' },
  ADMIN_PERMISSION_RESOURCES: { CHANNEL: 'channel' },
  hasPermission: () => true,
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (
    selector: (state: { auth: { user: { role: number } } }) => unknown
  ) => selector({ auth: { user: { role: 100 } } }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('sonner', () => ({
  toast: { error: toastErrorMock, success: toastSuccessMock },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function MutationHarness(props: {
  isCurrent: () => boolean
  onSuccess: () => void
}) {
  const mutation = useChannelMutateForm({
    currentRow: { id: 7 } as Channel,
    isEditing: true,
    isMultiKeyChannel: false,
    onSuccess: props.onSuccess,
  })

  return (
    <button
      type='button'
      onClick={() =>
        void mutation
          .mutateAsync({
            data: { name: 'updated channel' } as ChannelFormValues,
            isCurrent: props.isCurrent,
          })
          .catch(() => undefined)
      }
    >
      Save
    </button>
  )
}

function renderHarness(props: {
  isCurrent: () => boolean
  onSuccess: () => void
}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MutationHarness {...props} />
    </QueryClientProvider>
  )
}

describe('channel mutation session lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    updateChannelMock.mockReset()
  })

  test('does not complete the active drawer after an obsolete save succeeds', async () => {
    const user = userEvent.setup()
    const save = deferred<{ success: boolean }>()
    updateChannelMock.mockImplementationOnce(() => save.promise)
    const onSuccess = vi.fn()
    let current = true
    renderHarness({ isCurrent: () => current, onSuccess })

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(updateChannelMock).toHaveBeenCalledOnce())
    current = false
    await act(async () => {
      save.resolve({ success: true })
      await save.promise
    })

    expect(onSuccess).not.toHaveBeenCalled()
    expect(toastSuccessMock).not.toHaveBeenCalled()
    expect(toastErrorMock).not.toHaveBeenCalled()
  })

  test('completes the drawer when the save still belongs to the current session', async () => {
    const user = userEvent.setup()
    updateChannelMock.mockResolvedValue({ success: true })
    const onSuccess = vi.fn()
    renderHarness({ isCurrent: () => true, onSuccess })

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSuccess).toHaveBeenCalledOnce())

    expect(toastSuccessMock).toHaveBeenCalledWith(
      'Channel updated successfully'
    )
  })
})
