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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { resolveRadarSettings } from '@/features/model-radar/lib/model-radar'

import {
  getModelRadarManagement,
  getSystemTask,
  triggerModelRadarSync,
  updateSystemOption,
} from '../../api'
import { SettingsPageProvider } from '../../components/settings-page-context'
import type { ModelRadarManagement, SystemTask } from '../../types'
import { ModelRadarSection } from '../model-radar-section'

const toasts = vi.hoisted(() => ({
  success: vi.fn(),
  info: vi.fn(),
  error: vi.fn(),
}))
vi.mock('sonner', () => ({ toast: toasts }))
vi.mock('../../api', () => ({
  getModelRadarManagement: vi.fn(),
  getSystemTask: vi.fn(),
  triggerModelRadarSync: vi.fn(),
  updateSystemOption: vi.fn(),
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, unknown>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      ),
  }),
}))

const task: SystemTask = {
  id: 1,
  task_id: 'radar-sync-1',
  type: 'model_radar_sync',
  status: 'pending',
  created_at: 100,
  updated_at: 100,
}
let management: ModelRadarManagement
const clients: QueryClient[] = []
const actions: HTMLDivElement[] = []

function renderSection(raw = '') {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  clients.push(client)
  const container = document.createElement('div')
  document.body.appendChild(container)
  actions.push(container)
  const view = render(
    <QueryClientProvider client={client}>
      <SettingsPageProvider
        actionsContainer={container}
        suppressSectionHeader={false}
      >
        <ModelRadarSection initialSerialized={raw} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  return { ...view, client }
}

beforeEach(() => {
  vi.clearAllMocks()
  management = {
    settings: resolveRadarSettings({}),
    sync: { enabled: false, interval_minutes: 10, stale_after_minutes: 30 },
    snapshot: {
      fetched_at: 100,
      source_updated_at: 90,
      alerts_updated_at: 90,
      stale: false,
      model_count: 2,
      configuration_count: 3,
      alert_count: 0,
      models: [
        { model: 'k3', configuration_count: 2, efforts: ['high', 'low'] },
        { model: 'gpt-a', configuration_count: 1, efforts: ['low'] },
      ],
    },
    current_task: null,
  }
  vi.mocked(getModelRadarManagement).mockImplementation(async () =>
    structuredClone(management)
  )
  vi.mocked(updateSystemOption).mockResolvedValue({
    success: true,
    message: '',
  })
  vi.mocked(triggerModelRadarSync).mockResolvedValue({ task, created: true })
  vi.mocked(getSystemTask).mockResolvedValue({
    success: true,
    message: '',
    data: { ...task, status: 'succeeded' },
  })
})
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  for (const action of actions.splice(0)) action.remove()
})

describe('model radar management', () => {
  test('renders alias placeholders and saves hidden overrides without persisting untouched rows', async () => {
    const user = userEvent.setup()
    renderSection()
    const name = await screen.findByRole('textbox', {
      name: 'Display name: k3',
    })
    expect(name).toHaveAttribute('placeholder', 'kimi-k3')
    expect(screen.getByText('2 tiers')).toBeVisible()
    expect(
      screen.getByText(
        'Scheduled sync is disabled by MODEL_RADAR_SYNC_ENABLED.'
      )
    ).toBeVisible()
    await user.click(screen.getByRole('switch', { name: 'Hidden: k3' }))
    await user.type(
      screen.getByRole('textbox', { name: 'Aliases: k3' }),
      'kimi-k3-latest'
    )
    await user.click(
      screen.getByRole('button', { name: 'Save radar settings' })
    )
    await waitFor(() =>
      expect(updateSystemOption).toHaveBeenCalledWith({
        key: 'ModelRadarSettings',
        value:
          '{"auto_effort_enabled":true,"default_vendor":"openai","show_degradation_alerts":true,"models":{"k3":{"hidden":true,"aliases":["kimi-k3-latest"]}}}',
      })
    )
  })

  test('lists canonical vendors and All in the default selector and skips unchanged saves', async () => {
    const user = userEvent.setup()
    renderSection()
    await user.click(
      await screen.findByRole('combobox', { name: 'Default vendor' })
    )
    expect(screen.getByRole('option', { name: 'All' })).toBeVisible()
    expect(screen.getByRole('option', { name: 'Anthropic' })).toBeVisible()
    expect(screen.getByRole('option', { name: 'Tencent' })).toBeVisible()
    await user.keyboard('{Escape}')
    await user.click(
      screen.getByRole('button', { name: 'Save radar settings' })
    )
    expect(updateSystemOption).not.toHaveBeenCalled()
  })

  test('reset restores a snapshot row and removes an override for a missing model', async () => {
    const user = userEvent.setup()
    renderSection(
      '{"models":{"k3":{"hidden":true,"auto_effort":true},"retired":{"vendor":"moonshot"}}}'
    )
    await user.click(
      await screen.findByRole('button', { name: 'Reset row: retired' })
    )
    expect(
      screen.queryByRole('textbox', { name: 'Display name: retired' })
    ).toBeNull()
    await user.click(screen.getByRole('button', { name: 'Reset row: k3' }))
    expect(screen.getByRole('switch', { name: 'Hidden: k3' })).not.toBeChecked()
    expect(
      screen.getByRole('switch', { name: 'Allow tier adjustment: k3' })
    ).not.toBeChecked()
    await user.click(
      screen.getByRole('button', { name: 'Save radar settings' })
    )
    await waitFor(() =>
      expect(updateSystemOption).toHaveBeenCalledWith({
        key: 'ModelRadarSettings',
        value:
          '{"auto_effort_enabled":true,"default_vendor":"openai","show_degradation_alerts":true,"models":{}}',
      })
    )
  })

  test('carries the automatic reasoning tier switch into the saved settings', async () => {
    const user = userEvent.setup()
    renderSection()
    const toggle = await screen.findByRole('switch', {
      name: 'Allow automatic reasoning tiers',
    })
    expect(toggle).toBeChecked()
    await user.click(toggle)
    await user.click(
      screen.getByRole('button', { name: 'Save radar settings' })
    )
    await waitFor(() =>
      expect(updateSystemOption).toHaveBeenCalledWith({
        key: 'ModelRadarSettings',
        value:
          '{"auto_effort_enabled":false,"default_vendor":"openai","show_degradation_alerts":true,"models":{}}',
      })
    )
  })

  test('lets the administrator allow tier adjustment per model', async () => {
    const user = userEvent.setup()
    renderSection()
    const toggle = await screen.findByRole('switch', {
      name: 'Allow tier adjustment: k3',
    })
    expect(toggle).not.toBeChecked()
    await user.click(toggle)
    await user.click(
      screen.getByRole('button', { name: 'Save radar settings' })
    )
    await waitFor(() =>
      expect(updateSystemOption).toHaveBeenCalledWith({
        key: 'ModelRadarSettings',
        value:
          '{"auto_effort_enabled":true,"default_vendor":"openai","show_degradation_alerts":true,"models":{"k3":{"auto_effort":true}}}',
      })
    )
  })

  test.each([true, false])(
    'starts or follows a sync with created=%s and reports its completion',
    async (created) => {
      const user = userEvent.setup()
      vi.mocked(triggerModelRadarSync).mockResolvedValue({ task, created })
      renderSection()
      await user.click(await screen.findByRole('button', { name: 'Sync now' }))
      await waitFor(() => expect(triggerModelRadarSync).toHaveBeenCalledOnce())
      await waitFor(() =>
        expect(getSystemTask).toHaveBeenCalledWith('radar-sync-1')
      )
      expect(created ? toasts.success : toasts.info).toHaveBeenCalledWith(
        created ? 'Sync started' : 'A sync is already running.'
      )
      await waitFor(() =>
        expect(toasts.success).toHaveBeenCalledWith('Sync finished')
      )
    }
  )

  test('follows an existing task, keeps sync disabled while pending and surfaces failure', async () => {
    const user = userEvent.setup()
    management.current_task = task
    vi.mocked(getSystemTask).mockResolvedValue({
      success: true,
      message: '',
      data: task,
    })
    const view = renderSection()
    await waitFor(() =>
      expect(getSystemTask).toHaveBeenCalledWith(task.task_id)
    )
    expect(screen.getByRole('button', { name: 'Syncing...' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Syncing...' }))
    expect(triggerModelRadarSync).not.toHaveBeenCalled()
    vi.mocked(getSystemTask).mockResolvedValue({
      success: true,
      message: '',
      data: { ...task, status: 'failed', error: 'upstream unavailable' },
    })
    await view.client.invalidateQueries({ queryKey: ['model-radar-sync-task'] })
    await waitFor(() =>
      expect(toasts.error).toHaveBeenCalledWith(
        'Sync failed: upstream unavailable'
      )
    )
  })

  test('renders a usable management form before the first snapshot', async () => {
    management.snapshot = null
    renderSection()
    await screen.findByRole('button', { name: 'Sync now' })
    expect(screen.getAllByText('No snapshot yet')).toHaveLength(2)
    expect(
      screen.getByRole('combobox', { name: 'Default vendor' })
    ).toBeEnabled()
  })

  test('a management refresh appends new models without discarding unsaved edits', async () => {
    const user = userEvent.setup()
    const view = renderSection()
    await user.type(
      await screen.findByRole('textbox', { name: 'Display name: k3' }),
      'My Kimi'
    )
    management.snapshot?.models.push({
      model: 'hy4-preview',
      configuration_count: 1,
      efforts: ['low'],
    })
    await view.client.invalidateQueries({
      queryKey: ['model-radar-management'],
    })
    await screen.findByRole('textbox', { name: 'Display name: hy4-preview' })
    expect(
      screen.getByRole('textbox', { name: 'Display name: k3' })
    ).toHaveValue('My Kimi')
    expect(
      within(screen.getByRole('table', { name: 'Model mapping' })).getAllByRole(
        'row'
      )
    ).toHaveLength(4)
  })
})
