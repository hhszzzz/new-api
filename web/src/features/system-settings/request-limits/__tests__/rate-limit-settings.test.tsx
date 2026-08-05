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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { RateLimitSection } from '../rate-limit-section'
import {
  isValidGroupPoliciesJSON,
  isValidRequestCountJSON,
} from '../rate-limit-validation'
import { RateLimitVisualEditor } from '../rate-limit-visual-editor'

const apiMocks = vi.hoisted(() => ({
  updateGroupRateLimitOptions: vi.fn(),
  updateSystemOption: vi.fn(),
}))

vi.mock('../../api', () => apiMocks)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('group rate-limit settings', () => {
  beforeEach(() => {
    apiMocks.updateGroupRateLimitOptions.mockReset()
    apiMocks.updateSystemOption.mockReset()
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }))
    )
  })

  test('validates the legacy request-count format without changing it', () => {
    expect(isValidRequestCountJSON('{"default":[200,100]}')).toBe(true)
    expect(isValidRequestCountJSON('{"default":[0,1]}')).toBe(true)
    expect(isValidRequestCountJSON('{"default":[1,0]}')).toBe(false)
    expect(isValidRequestCountJSON('{"default":[1.5,2]}')).toBe(false)
  })

  test('accepts nullable limits and rejects invalid or ambiguous policies', () => {
    expect(
      isValidGroupPoliciesJSON(
        '{"default":{"member_limits":{"rpm_limit":60,"concurrency_limit":null},"shared_pool":{"stream_tps_limit":1000}}}'
      )
    ).toBe(true)
    expect(
      isValidGroupPoliciesJSON(
        '{"default":{"member_limits":{"rpm_limit":0},"shared_pool":{}}}'
      )
    ).toBe(false)
    expect(
      isValidGroupPoliciesJSON(
        '{"vip":{"member_limits":{}}," vip ":{"shared_pool":{}}}'
      )
    ).toBe(false)
  })

  test('unions legacy-only and advanced-only groups in the visual table', () => {
    render(
      <RateLimitVisualEditor
        requestCounts='{"legacy":[200,100]}'
        policies='{"advanced":{"member_limits":{"rpm_limit":60},"shared_pool":{"concurrency_limit":10}}}'
        onRequestCountsChange={vi.fn()}
        onPoliciesChange={vi.fn()}
      />
    )

    expect(screen.getByText('legacy')).toBeInTheDocument()
    expect(screen.getByText('advanced')).toBeInTheDocument()
    expect(screen.getByText('RPM 60')).toBeInTheDocument()
    expect(screen.getByText('Concurrency 10')).toBeInTheDocument()
    expect(screen.getByText('Use global values')).toBeInTheDocument()
  })

  test('opens one editor with all three policy sections', () => {
    render(
      <RateLimitVisualEditor
        requestCounts='{}'
        policies='{}'
        onRequestCountsChange={vi.fn()}
        onPoliciesChange={vi.fn()}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Add group' }))
    const dialog = screen.getByRole('dialog')
    expect(
      within(dialog).getByText('Request-count override')
    ).toBeInTheDocument()
    expect(within(dialog).getByText('Group member limits')).toBeInTheDocument()
    expect(within(dialog).getByText('Group shared pool')).toBeInTheDocument()
  })

  test('submits the nested form fields through the flat atomic API contract', async () => {
    apiMocks.updateGroupRateLimitOptions.mockResolvedValue({
      success: true,
      message: '',
    })
    const actionsContainer = document.createElement('div')
    document.body.append(actionsContainer)
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    const view = render(
      <QueryClientProvider client={queryClient}>
        <SettingsPageProvider actionsContainer={actionsContainer}>
          <RateLimitSection
            defaultValues={{
              ModelRequestRateLimitEnabled: false,
              ModelRequestRateLimitDurationMinutes: 1,
              ModelRequestRateLimitCount: 0,
              ModelRequestRateLimitSuccessCount: 1000,
              ModelRequestRateLimitGroup: '{"default":[200,100]}',
              'group_rate_limit_setting.member_enabled': false,
              'group_rate_limit_setting.shared_pool_enabled': false,
              'group_rate_limit_setting.policies':
                '{"default":{"member_limits":{"rpm_limit":60}}}',
            }}
          />
        </SettingsPageProvider>
      </QueryClientProvider>
    )

    const switches = screen.getAllByRole('switch')
    fireEvent.click(switches[1])
    fireEvent.click(screen.getByRole('button', { name: 'Save rate limits' }))

    await waitFor(() =>
      expect(apiMocks.updateGroupRateLimitOptions).toHaveBeenCalledWith({
        member_enabled: true,
        shared_pool_enabled: false,
        model_request_rate_limit_group: { default: [200, 100] },
        policies: {
          default: { member_limits: { rpm_limit: 60 } },
        },
      })
    )
    expect(apiMocks.updateSystemOption).not.toHaveBeenCalled()

    view.unmount()
    actionsContainer.remove()
  })
})
