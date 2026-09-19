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
import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { AccountPoolSettingsSection } from '../account-pool-settings-section'

const apiMocks = vi.hoisted(() => ({
  getAccountPoolSettings: vi.fn(),
  updateAccountPoolSettings: vi.fn(),
  getGroups: vi.fn(),
}))

vi.mock('@/features/account-pool/api', () => ({
  getAccountPoolSettings: apiMocks.getAccountPoolSettings,
  updateAccountPoolSettings: apiMocks.updateAccountPoolSettings,
}))

vi.mock('@/features/users/api', () => ({
  getGroups: apiMocks.getGroups,
}))

vi.mock('@/components/multi-select', () => ({
  MultiSelect: (props: { selected: string[]; 'aria-label'?: string }) => (
    <output data-testid={props['aria-label'] ?? 'allowed-groups'}>
      {props.selected.join(',')}
    </output>
  ),
}))

vi.mock('../components/settings-page-context', () => ({
  SettingsPageFormActions: () => null,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('account pool settings section', () => {
  test('treats a legacy null provider_groups response as an empty selection', async () => {
    apiMocks.getAccountPoolSettings.mockResolvedValue({
      success: true,
      message: '',
      data: {
        enabled: true,
        provider_groups: null as unknown as Record<string, string[]>,
        regular_refresh_seconds: 300,
        near_reset_threshold_seconds: 600,
        near_reset_refresh_seconds: 60,
        post_reset_delay_seconds: 10,
        manual_refresh_cooldown_seconds: 60,
        management_key_configured: true,
        management_ready: true,
        last_sync_at: null,
        last_sync_status: 'never',
      },
    })
    apiMocks.getGroups.mockResolvedValue({ success: true, data: [] })
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <AccountPoolSettingsSection />
      </QueryClientProvider>
    )

    expect(await screen.findByTestId('Codex')).toHaveTextContent('')
    expect(screen.getByTestId('Claude')).toHaveTextContent('')
    expect(screen.getByTestId('Antigravity')).toHaveTextContent('')
    expect(
      screen.getByRole('switch', {
        name: 'Hide account emails from regular users',
      })
    ).toBeChecked()
  })

  test('renders per-provider group selections', async () => {
    apiMocks.getAccountPoolSettings.mockResolvedValue({
      success: true,
      message: '',
      data: {
        enabled: true,
        provider_groups: { claude: ['team'] },
        regular_refresh_seconds: 300,
        near_reset_threshold_seconds: 600,
        near_reset_refresh_seconds: 60,
        post_reset_delay_seconds: 10,
        manual_refresh_cooldown_seconds: 60,
        management_key_configured: true,
        management_ready: true,
        last_sync_at: null,
        last_sync_status: 'never',
      },
    })
    apiMocks.getGroups.mockResolvedValue({
      success: true,
      data: ['vip', 'team'],
    })
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <AccountPoolSettingsSection />
      </QueryClientProvider>
    )

    expect(await screen.findByTestId('Codex')).toHaveTextContent('')
    expect(screen.getByTestId('Claude')).toHaveTextContent('team')
    expect(screen.getByTestId('Antigravity')).toHaveTextContent('')
  })
})
