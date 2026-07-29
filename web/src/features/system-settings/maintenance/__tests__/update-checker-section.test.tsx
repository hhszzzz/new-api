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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { UpdateCheckerSection } from '../update-checker-section'

const { getSystemUpdateInfoMock, startSystemUpdateMock } = vi.hoisted(() => ({
  getSystemUpdateInfoMock: vi.fn(),
  startSystemUpdateMock: vi.fn(),
}))

vi.mock('@/features/system-settings/api', () => ({
  getRunningSystemVersion: vi.fn(),
  getSystemUpdateInfo: getSystemUpdateInfoMock,
  getSystemUpdateTriggerState: vi.fn(),
  startSystemUpdate: startSystemUpdateMock,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) =>
      values?.version ? key.replace('{{version}}', values.version) : key,
  }),
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

function renderUpdateChecker() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <UpdateCheckerSection
        currentVersion='main-deadbeef'
        startTime={1_800_000_000}
      />
    </QueryClientProvider>
  )
}

describe('system update checker', () => {
  beforeEach(() => {
    getSystemUpdateInfoMock.mockReset()
    startSystemUpdateMock.mockReset()
  })

  test('shows the one-click action after a newer successful GHCR build is found', async () => {
    getSystemUpdateInfoMock.mockResolvedValue({
      current_version: 'main-deadbeef',
      latest_version: 'main-9de2eea0',
      latest_revision: '9de2eea0ab7d1708e3708bb8eadc89bafa2744b7',
      update_available: true,
      update_enabled: true,
      image: 'ghcr.io/hhszzzz/new-api:main',
      published_at: '2026-07-28T10:39:04Z',
      workflow_url: 'https://github.com/hhszzzz/new-api/actions/runs/123',
      trigger: { status: 'idle' },
    })
    const user = userEvent.setup()
    renderUpdateChecker()

    await user.click(screen.getByRole('button', { name: 'Check for updates' }))

    expect(await screen.findByText('main-9de2eea0')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Update now' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'View build' })).toHaveAttribute(
      'href',
      'https://github.com/hhszzzz/new-api/actions/runs/123'
    )
  })

  test('confirms and starts the application-only update', async () => {
    getSystemUpdateInfoMock.mockResolvedValue({
      current_version: 'main-deadbeef',
      latest_version: 'main-9de2eea0',
      latest_revision: '9de2eea0ab7d1708e3708bb8eadc89bafa2744b7',
      update_available: true,
      update_enabled: true,
      image: 'ghcr.io/hhszzzz/new-api:main',
      trigger: { status: 'idle' },
    })
    startSystemUpdateMock.mockResolvedValue({
      started: true,
      update: {
        current_version: 'main-deadbeef',
        latest_version: 'main-9de2eea0',
        latest_revision: '9de2eea0ab7d1708e3708bb8eadc89bafa2744b7',
        update_available: true,
        update_enabled: true,
        image: 'ghcr.io/hhszzzz/new-api:main',
        trigger: { status: 'triggering' },
      },
    })
    const user = userEvent.setup()
    renderUpdateChecker()

    await user.click(screen.getByRole('button', { name: 'Check for updates' }))
    await user.click(await screen.findByRole('button', { name: 'Update now' }))
    await user.click(screen.getByRole('button', { name: 'Confirm update' }))

    await waitFor(() => expect(startSystemUpdateMock).toHaveBeenCalledOnce())
    expect(
      screen.getByText('Waiting for the updated service to start...')
    ).toBeInTheDocument()
  })

  test('explains when the deployment has no authenticated update trigger', async () => {
    getSystemUpdateInfoMock.mockResolvedValue({
      current_version: 'main-deadbeef',
      latest_version: 'main-9de2eea0',
      latest_revision: '9de2eea0ab7d1708e3708bb8eadc89bafa2744b7',
      update_available: true,
      update_enabled: false,
      image: 'ghcr.io/hhszzzz/new-api:main',
      trigger: { status: 'idle' },
    })
    const user = userEvent.setup()
    renderUpdateChecker()

    await user.click(screen.getByRole('button', { name: 'Check for updates' }))

    expect(
      await screen.findByText(
        'One-click update is not configured on this deployment.'
      )
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Update now' })
    ).not.toBeInTheDocument()
  })
})
