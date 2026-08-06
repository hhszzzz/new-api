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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import type { Channel } from '../../types'
import { OllamaModelsDialog } from '../dialogs/ollama-models-dialog'

const {
  fetchModelsMock,
  getFreshAuthHeadersMock,
  providerState,
  translateMock,
} = vi.hoisted(() => ({
  fetchModelsMock: vi.fn(),
  getFreshAuthHeadersMock: vi.fn().mockResolvedValue({}),
  providerState: { currentRow: null as Channel | null },
  translateMock: (key: string, values?: Record<string, string | number>) =>
    Object.entries(values || {}).reduce(
      (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
      key
    ),
}))

vi.mock('@/lib/api', () => ({
  getFreshAuthHeaders: getFreshAuthHeadersMock,
}))

vi.mock('../../api', () => ({
  deleteOllamaModel: vi.fn(),
  fetchModels: fetchModelsMock,
  fetchUpstreamModels: vi.fn(),
  updateChannel: vi.fn(),
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
    info: vi.fn(),
    success: vi.fn(),
  },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function ollamaChannel(id: number, name: string): Channel {
  return {
    id,
    name,
    type: 4,
    base_url: `http://ollama-${id}.test`,
    models: '',
  } as Channel
}

describe('ollama models dialog request lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    providerState.currentRow = null
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  test('ignores an older channel response after switching channels', async () => {
    const first = deferred<{ success: boolean; data: string[] }>()
    const second = deferred<{ success: boolean; data: string[] }>()
    fetchModelsMock
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)
    providerState.currentRow = ollamaChannel(1, 'first')
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={queryClient}>
        <OllamaModelsDialog open onOpenChange={vi.fn()} />
      </QueryClientProvider>
    )

    await waitFor(() => expect(fetchModelsMock).toHaveBeenCalledTimes(1))
    providerState.currentRow = ollamaChannel(2, 'second')
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <OllamaModelsDialog open onOpenChange={vi.fn()} />
      </QueryClientProvider>
    )
    await waitFor(() => expect(fetchModelsMock).toHaveBeenCalledTimes(2))

    second.resolve({ success: true, data: ['new-channel-model'] })
    expect(await screen.findByText('new-channel-model')).toBeVisible()

    await act(async () => {
      first.resolve({ success: true, data: ['stale-channel-model'] })
      await first.promise
    })
    await waitFor(() => {
      expect(screen.queryByText('stale-channel-model')).toBeNull()
      expect(screen.getByText('new-channel-model')).toBeVisible()
    })
  })

  test('clears an in-flight pull state when switching channels', async () => {
    fetchModelsMock.mockResolvedValue({ success: true, data: [] })
    const pullRequest = deferred<Response>()
    const fetchMock = vi.fn(() => pullRequest.promise)
    vi.stubGlobal('fetch', fetchMock)
    providerState.currentRow = ollamaChannel(1, 'first')
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={queryClient}>
        <OllamaModelsDialog open onOpenChange={vi.fn()} />
      </QueryClientProvider>
    )

    await waitFor(() => expect(fetchModelsMock).toHaveBeenCalledOnce())
    fireEvent.change(screen.getByLabelText('Pull model'), {
      target: { value: 'llama3.1:8b' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Pull' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
    expect(screen.getByRole('button', { name: 'Pulling...' })).toBeDisabled()

    providerState.currentRow = ollamaChannel(2, 'second')
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <OllamaModelsDialog open onOpenChange={vi.fn()} />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Pull' })).toBeEnabled()
      expect(screen.getByLabelText('Pull model')).toHaveValue('')
    })
  })
})
