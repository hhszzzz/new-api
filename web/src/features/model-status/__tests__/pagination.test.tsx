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
import { act, fireEvent, render, screen, within } from '@testing-library/react'
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
      Object.entries(values ?? {}).reduce(
        (copy, [name, value]) => copy.replace(`{{${name}}}`, String(value)),
        key
      ),
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
const scrollIntoView = vi.fn()

function createModels(count: number): ModelStatusModel[] {
  return Array.from({ length: count }, (_, index) => ({
    model_name: `model-${String(index + 1).padStart(2, '0')}`,
    vendor: 'Test Vendor',
    icon: 'OpenAI.Color',
    request_count: 10,
    success_count: 10,
    success_rate: 100,
    avg_ttft_ms: 100,
    avg_latency_ms: 200,
    avg_tps: 20,
    status: 'operational',
    timeline: [],
  }))
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
  scrollIntoView.mockReset()
  Object.defineProperty(Element.prototype, 'scrollIntoView', {
    configurable: true,
    value: scrollIntoView,
  })
})

describe('model status pagination', () => {
  test('renders 20 sorted models per page and exposes accessible page controls', async () => {
    const models = createModels(21)
    models[20] = {
      ...models[20],
      success_rate: 0,
      status: 'failed',
    }
    mockedGetModelStatus.mockResolvedValue(createResponse(models))
    const user = userEvent.setup()

    renderModelStatus()

    const modelRegion = await screen.findByRole('region', {
      name: 'Model Status',
    })
    expect(within(modelRegion).getAllByRole('article')).toHaveLength(20)
    expect(within(modelRegion).getAllByRole('listitem')).toHaveLength(20 * 24)
    expect(
      within(modelRegion).getByRole('article', { name: 'model-21' })
    ).toBeVisible()
    expect(screen.getByText('Page 1 of 2')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Previous page' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'Next page' }))

    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'start' })
    expect(within(modelRegion).getAllByRole('article')).toHaveLength(1)
    expect(
      within(modelRegion).getByRole('article', { name: 'model-20' })
    ).toBeVisible()
    expect(within(modelRegion).getAllByRole('listitem')).toHaveLength(24)
    expect(screen.getByText('Page 2 of 2')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Previous page' })).toBeEnabled()
  })

  test('clamps the stored page after refresh returns fewer models', async () => {
    mockedGetModelStatus
      .mockResolvedValueOnce(createResponse(createModels(21)))
      .mockResolvedValueOnce(createResponse(createModels(5)))
      .mockResolvedValueOnce(createResponse(createModels(21)))
    const user = userEvent.setup()

    renderModelStatus()

    const modelRegion = await screen.findByRole('region', {
      name: 'Model Status',
    })
    await user.click(screen.getByRole('button', { name: 'Next page' }))
    expect(
      within(modelRegion).getByRole('article', { name: 'model-21' })
    ).toBeVisible()

    vi.useFakeTimers()
    try {
      fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
        await Promise.resolve()
      })

      expect(within(modelRegion).getAllByRole('article')).toHaveLength(5)
      expect(screen.queryByText('Page 2 of 2')).not.toBeInTheDocument()
      expect(
        within(modelRegion).getByRole('article', { name: 'model-01' })
      ).toBeVisible()

      act(() => vi.advanceTimersByTime(5000))
      fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
        await Promise.resolve()
      })

      expect(screen.getByText('Page 1 of 2')).toBeVisible()
      expect(
        within(modelRegion).getByRole('article', { name: 'model-01' })
      ).toBeVisible()
      expect(
        within(modelRegion).queryByRole('article', { name: 'model-21' })
      ).not.toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })
})
