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

import type { Channel } from '../../types'
import { FetchModelsDialog } from '../dialogs/fetch-models-dialog'

const {
  fetchUpstreamModelsMock,
  providerState,
  toastSuccessMock,
  translateMock,
  updateChannelMock,
} = vi.hoisted(() => ({
  fetchUpstreamModelsMock: vi.fn(),
  providerState: { currentRow: null as Channel | null },
  toastSuccessMock: vi.fn(),
  translateMock: (key: string, values?: Record<string, string | number>) =>
    Object.entries(values || {}).reduce(
      (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
      key
    ),
  updateChannelMock: vi.fn(),
}))

vi.mock('../../api', () => ({
  fetchUpstreamModels: fetchUpstreamModelsMock,
  updateChannel: updateChannelMock,
}))

vi.mock('../channels-provider', () => ({
  useChannels: () => providerState,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translateMock }),
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: toastSuccessMock,
  },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function renderDialog(customFetcher: () => Promise<string[]>) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <FetchModelsDialog
        open
        onOpenChange={vi.fn()}
        customFetcher={customFetcher}
        existingModelsOverride={[]}
      />
    </QueryClientProvider>
  )
  return { queryClient, view }
}

describe('fetch models dialog request lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    fetchUpstreamModelsMock.mockReset()
    providerState.currentRow = null
    updateChannelMock.mockReset()
  })

  test('keeps the newest model result when an older request finishes later', async () => {
    const first = deferred<string[]>()
    const second = deferred<string[]>()
    const firstFetcher = vi.fn(() => first.promise)
    const secondFetcher = vi.fn(() => second.promise)
    const { queryClient, view } = renderDialog(firstFetcher)

    await waitFor(() => expect(firstFetcher).toHaveBeenCalledOnce())
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <FetchModelsDialog
          open
          onOpenChange={vi.fn()}
          customFetcher={secondFetcher}
          existingModelsOverride={[]}
        />
      </QueryClientProvider>
    )
    await waitFor(() => expect(secondFetcher).toHaveBeenCalledOnce())

    second.resolve(['new-channel-model'])
    expect(await screen.findByText('new-channel-model')).toBeVisible()

    await act(async () => {
      first.resolve(['stale-channel-model'])
      await first.promise
    })
    await waitFor(() => {
      expect(screen.queryByText('stale-channel-model')).toBeNull()
      expect(screen.getByText('new-channel-model')).toBeVisible()
    })
    expect(toastSuccessMock).toHaveBeenCalledTimes(1)
  })

  test('does not refetch when an equivalent models override gets a new array identity', async () => {
    const customFetcher = vi.fn().mockResolvedValue(['stable-model'])
    const { queryClient, view } = renderDialog(customFetcher)

    expect(await screen.findByText('stable-model')).toBeVisible()
    expect(customFetcher).toHaveBeenCalledOnce()

    view.rerender(
      <QueryClientProvider client={queryClient}>
        <FetchModelsDialog
          open
          onOpenChange={vi.fn()}
          customFetcher={customFetcher}
          existingModelsOverride={[]}
        />
      </QueryClientProvider>
    )

    await waitFor(() => expect(customFetcher).toHaveBeenCalledOnce())
  })

  test('uses the newest existing-model snapshot when an older fetch finishes later', async () => {
    const user = userEvent.setup()
    const first = deferred<string[]>()
    const second = deferred<string[]>()
    const customFetcher = vi
      .fn<() => Promise<string[]>>()
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)
    const onModelsSelected = vi.fn()
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={queryClient}>
        <FetchModelsDialog
          open
          onOpenChange={vi.fn()}
          onModelsSelected={onModelsSelected}
          customFetcher={customFetcher}
          existingModelsOverride={['old-existing-model']}
        />
      </QueryClientProvider>
    )
    await waitFor(() => expect(customFetcher).toHaveBeenCalledOnce())

    view.rerender(
      <QueryClientProvider client={queryClient}>
        <FetchModelsDialog
          open
          onOpenChange={vi.fn()}
          onModelsSelected={onModelsSelected}
          customFetcher={customFetcher}
          existingModelsOverride={['new-existing-model']}
        />
      </QueryClientProvider>
    )
    await waitFor(() => expect(customFetcher).toHaveBeenCalledTimes(2))

    await act(async () => {
      second.resolve(['new-existing-model'])
      await second.promise
    })
    expect(
      await screen.findByRole('button', { name: 'Save Models' })
    ).toBeEnabled()

    await act(async () => {
      first.resolve(['old-existing-model'])
      await first.promise
    })
    await user.click(screen.getByRole('button', { name: 'Save Models' }))

    expect(onModelsSelected).toHaveBeenCalledWith(['new-existing-model'])
  })

  test('does not let an obsolete standalone save close a different channel', async () => {
    const user = userEvent.setup()
    const save = deferred<{ success: boolean }>()
    fetchUpstreamModelsMock.mockResolvedValue({
      success: true,
      data: ['shared-model'],
    })
    updateChannelMock.mockImplementationOnce(() => save.promise)
    providerState.currentRow = {
      id: 1,
      name: 'first channel',
      models: 'shared-model',
    } as Channel
    const onOpenChange = vi.fn()
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const renderStandaloneDialog = () => (
      <QueryClientProvider client={queryClient}>
        <FetchModelsDialog open onOpenChange={onOpenChange} />
      </QueryClientProvider>
    )
    const view = render(renderStandaloneDialog())
    expect(
      await screen.findByRole('button', { name: 'Save Models' })
    ).toBeEnabled()

    await user.click(screen.getByRole('button', { name: 'Save Models' }))
    await waitFor(() => expect(updateChannelMock).toHaveBeenCalledOnce())

    providerState.currentRow = {
      id: 2,
      name: 'second channel',
      models: 'shared-model',
    } as Channel
    view.rerender(renderStandaloneDialog())
    await waitFor(() => {
      expect(fetchUpstreamModelsMock).toHaveBeenCalledWith(2)
    })

    await act(async () => {
      save.resolve({ success: true })
      await save.promise
    })

    expect(onOpenChange).not.toHaveBeenCalled()
    expect(
      toastSuccessMock.mock.calls.some(
        ([message]) => message === 'Models updated successfully'
      )
    ).toBe(false)
  })
})
