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
import {
  QueryClient,
  QueryClientProvider,
  focusManager,
} from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { PropsWithChildren } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useModelStatus } from '@/features/performance-metrics/hooks/use-model-status'
import type {
  ModelHealthStatus,
  ModelStatusModel,
  ModelStatusResponse,
} from '@/features/performance-metrics/status-types'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { filterModelsByStatus } from '../lib/filters'
import type { PricingModel } from '../types'

function statusModel(
  name: string,
  status: ModelHealthStatus
): ModelStatusModel {
  return {
    model_name: name,
    vendor: '',
    icon: '',
    status,
    request_count: null,
    success_count: null,
    success_rate: status === 'failed' ? 0 : null,
    avg_latency_ms: null,
    avg_ttft_ms: null,
    avg_tps: null,
    timeline: [],
  }
}
function response(model: ModelStatusModel): ModelStatusResponse {
  return {
    success: true,
    data: { generated_at: 1800000000, window_hours: 24, models: [model] },
  }
}
let queryClient: QueryClient
function Wrapper(props: PropsWithChildren) {
  return (
    <QueryClientProvider client={queryClient}>
      {props.children}
    </QueryClientProvider>
  )
}

beforeEach(() => {
  useAuthStore.getState().auth.reset()
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
})
afterEach(() => {
  queryClient.clear()
  focusManager.setFocused(undefined)
  useAuthStore.getState().auth.reset()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('catalog status data', () => {
  it('shares the snapshot between consumers without periodic polling and refreshes on focus after expiry', async () => {
    const request = vi.spyOn(api, 'get').mockResolvedValue({
      data: response(statusModel('example', 'operational')),
    })
    renderHook(
      () => {
        const first = useModelStatus()
        const second = useModelStatus()
        return { first, second }
      },
      { wrapper: Wrapper }
    )
    await waitFor(() =>
      expect(
        queryClient.getQueryState(['model-status', null, null, null, null])
          ?.status
      ).toBe('success')
    )
    expect(request).toHaveBeenCalledTimes(1)
    vi.useFakeTimers()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(61000)
    })
    expect(request).toHaveBeenCalledTimes(1)
    await act(async () => {
      focusManager.setFocused(false)
      focusManager.setFocused(true)
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(request).toHaveBeenCalledTimes(2)
  })

  it('does not show another group or visitor identity snapshot while a new scope loads', async () => {
    const request = vi.spyOn(api, 'get').mockResolvedValue({
      data: response(statusModel('default-model', 'operational')),
    })
    const { result, rerender } = renderHook(
      (props: { group?: string }) => useModelStatus({ group: props.group }),
      { initialProps: {}, wrapper: Wrapper }
    )
    await waitFor(() =>
      expect(result.current.data?.data.models[0].model_name).toBe(
        'default-model'
      )
    )
    let resolveGroup!: (value: { data: ModelStatusResponse }) => void
    request.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveGroup = resolve
        })
    )
    rerender({ group: 'vip' })
    expect(result.current.data).toBeUndefined()
    await waitFor(() =>
      expect(request).toHaveBeenLastCalledWith('/api/perf-metrics/status', {
        params: { group: 'vip' },
      })
    )
    await act(async () => {
      resolveGroup({ data: response(statusModel('vip-model', 'failed')) })
    })
    await waitFor(() =>
      expect(result.current.data?.data.models[0].model_name).toBe('vip-model')
    )
    request.mockImplementationOnce(() => new Promise(() => {}))
    act(() =>
      useAuthStore.getState().auth.setUser({
        id: 42,
        username: 'another-user',
        role: 1,
        groups: ['vip'],
      })
    )
    expect(result.current.data).toBeUndefined()
  })

  it('retains the last snapshot on refresh failure and exposes the error', async () => {
    const request = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ data: response(statusModel('example', 'failed')) })
    const { result } = renderHook(() => useModelStatus(), { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    request.mockRejectedValueOnce(new Error('unavailable'))
    await act(async () => {
      await result.current.refetch()
    })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.data?.data.models[0].status).toBe('failed')
  })

  it('filters actual health states and sorts failures before healthy and missing samples', () => {
    const models = [
      'healthy',
      'no-traffic',
      'failed',
      'degraded',
      'not-loaded',
    ].map(
      (model_name, index) =>
        ({
          id: index + 1,
          model_name,
          quota_type: 0,
          model_ratio: 1,
          completion_ratio: 1,
          enable_groups: ['default'],
        }) satisfies PricingModel
    )
    const statuses = new Map([
      ['healthy', statusModel('healthy', 'operational')],
      ['failed', statusModel('failed', 'failed')],
      ['degraded', statusModel('degraded', 'degraded')],
      ['no-traffic', statusModel('no-traffic', 'no_data')],
    ])
    expect(
      filterModelsByStatus(models, statuses, 'all', 'status').map(
        (model) => model.model_name
      )
    ).toEqual(['failed', 'degraded', 'healthy', 'no-traffic', 'not-loaded'])
    expect(
      filterModelsByStatus(models, statuses, 'no_data', 'name').map(
        (model) => model.model_name
      )
    ).toEqual(['no-traffic'])
    expect(
      filterModelsByStatus(models, statuses, 'failed', 'name').map(
        (model) => model.model_name
      )
    ).toEqual(['failed'])
    expect(filterModelsByStatus(models, undefined, 'failed', 'status')).toEqual(
      models
    )
  })
})
