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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { getModelStatus } from '../api'
import { ModelStatus } from '../index'
import type { ModelStatusModel, ModelStatusResponse } from '../types'

vi.mock('../api', () => ({
  getModelStatus: vi.fn(),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, unknown>) =>
      values?.time === undefined
        ? key
        : key.replace('{{time}}', String(values.time)),
  }),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: ReactNode }) => props.children,
}))

vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: ReactNode }) => props.children,
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => <span data-testid='model-icon' />,
}))

const mockedGetModelStatus = vi.mocked(getModelStatus)
const GENERATED_AT = 1_800_000_000

function createModel(modelName: string): ModelStatusModel {
  return {
    model_name: modelName,
    vendor: 'OpenAI',
    icon: 'OpenAI.Color',
    request_count: 200,
    success_count: 199,
    success_rate: 99.5,
    avg_ttft_ms: 180,
    avg_latency_ms: 420,
    avg_tps: 31.2,
    status: 'operational',
    timeline: [],
  }
}

function createResponse(models: ModelStatusModel[]): ModelStatusResponse {
  return {
    success: true,
    data: {
      generated_at: GENERATED_AT,
      window_hours: 24,
      models,
    },
  }
}

function createDeferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })

  return { promise, reject, resolve }
}

function renderModelStatus() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        gcTime: Infinity,
        retry: false,
      },
    },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <ModelStatus />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  mockedGetModelStatus.mockReset()
})

describe('model status page request states', () => {
  test('announces initial loading and disables refresh while the request is pending', () => {
    mockedGetModelStatus.mockImplementation(() => new Promise(() => undefined))

    renderModelStatus()

    expect(screen.getByRole('status', { name: 'Loading...' })).toBeVisible()
    expect(screen.getByRole('main')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeDisabled()
  })

  test('shows the empty state after a successful response with no models', async () => {
    mockedGetModelStatus.mockResolvedValue(createResponse([]))

    renderModelStatus()

    expect(
      await screen.findByRole('status', { name: 'No models available' })
    ).toBeVisible()
    expect(screen.getByRole('main')).toHaveAttribute('aria-busy', 'false')
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
  })

  test('uses two compact status-card columns from the medium breakpoint', async () => {
    mockedGetModelStatus.mockResolvedValue(
      createResponse([createModel('compact-model')])
    )

    renderModelStatus()

    expect(
      await screen.findByRole('region', { name: 'Model Status' })
    ).toHaveClass('md:grid-cols-2')
  })

  test('disables Retry and reports progress while retrying an initial error', async () => {
    const retryRequest = createDeferred<ModelStatusResponse>()
    mockedGetModelStatus
      .mockRejectedValueOnce(new Error('offline'))
      .mockImplementationOnce(() => retryRequest.promise)
    const user = userEvent.setup()

    renderModelStatus()

    const errorState = await screen.findByRole('alert', {
      name: 'Unable to load model status',
    })
    const retryButton = within(errorState).getByRole('button', {
      name: 'Retry',
    })

    await user.click(retryButton)

    await waitFor(() => expect(mockedGetModelStatus).toHaveBeenCalledTimes(2))
    await waitFor(() => {
      const currentErrorState = screen.getByRole('alert', {
        name: 'Unable to load model status',
      })
      expect(
        within(currentErrorState).getByRole('button', {
          name: 'Refreshing...',
        })
      ).toBeDisabled()
    })
    expect(screen.getByRole('main')).toHaveAttribute('aria-busy', 'true')

    await act(async () => {
      retryRequest.resolve(createResponse([]))
      await retryRequest.promise
    })
    expect(
      await screen.findByRole('status', { name: 'No models available' })
    ).toBeVisible()
  })

  test('keeps cached cards visible and disables Refresh when a background refresh fails', async () => {
    const refreshRequest = createDeferred<ModelStatusResponse>()
    mockedGetModelStatus
      .mockResolvedValueOnce(createResponse([createModel('cached-model')]))
      .mockImplementationOnce(() => refreshRequest.promise)
    const user = userEvent.setup()

    renderModelStatus()

    expect(
      await screen.findByRole('article', { name: 'cached-model' })
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Refresh' }))

    await waitFor(() => expect(mockedGetModelStatus).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('article', { name: 'cached-model' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Refreshing...' })).toBeDisabled()
    expect(screen.getByRole('main')).toHaveAttribute('aria-busy', 'true')

    await act(async () => {
      refreshRequest.reject(new Error('refresh failed'))
      try {
        await refreshRequest.promise
      } catch {
        // React Query owns the surfaced error state.
      }
    })

    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /Refresh \([1-5]s\)/ })
      ).toBeDisabled()
    )
    expect(screen.getByRole('article', { name: 'cached-model' })).toBeVisible()
    expect(
      screen.queryByRole('alert', { name: 'Unable to load model status' })
    ).not.toBeInTheDocument()
  })

  test('shows a five-second countdown while manual refresh is cooling down', async () => {
    const refreshRequest = createDeferred<ModelStatusResponse>()
    const response = createResponse([createModel('refresh-model')])
    mockedGetModelStatus
      .mockResolvedValueOnce(response)
      .mockImplementationOnce(() => refreshRequest.promise)
    renderModelStatus()

    const refreshButton = await screen.findByRole('button', { name: 'Refresh' })
    await waitFor(() => expect(refreshButton).toBeEnabled())
    vi.useFakeTimers()
    try {
      await act(async () => {
        refreshButton.click()
        await Promise.resolve()
      })

      expect(mockedGetModelStatus).toHaveBeenCalledTimes(2)
      expect(
        screen.getByRole('button', { name: 'Refreshing...' })
      ).toBeDisabled()
      await act(async () => {
        refreshRequest.resolve(response)
        await refreshRequest.promise
        await vi.advanceTimersByTimeAsync(0)
        await Promise.resolve()
      })

      expect(
        screen.getByRole('button', { name: 'Refresh (5s)' })
      ).toBeDisabled()
      act(() => vi.advanceTimersByTime(1000))
      expect(
        screen.getByRole('button', { name: 'Refresh (4s)' })
      ).toBeDisabled()
      act(() => vi.advanceTimersByTime(3000))
      expect(
        screen.getByRole('button', { name: 'Refresh (1s)' })
      ).toBeDisabled()
      act(() => vi.advanceTimersByTime(1000))
      expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    } finally {
      vi.useRealTimers()
    }
  })
})
