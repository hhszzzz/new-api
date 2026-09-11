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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { Channel } from '../../types'
import { ChannelDisabledModelsDialog } from '../dialogs/channel-disabled-models-dialog'

const { toastErrorMock, toastSuccessMock, translateMock, updateStatusMock } =
  vi.hoisted(() => ({
    toastErrorMock: vi.fn(),
    toastSuccessMock: vi.fn(),
    translateMock: (key: string, values?: Record<string, string | number>) =>
      Object.entries(values || {}).reduce(
        (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
        key
      ),
    updateStatusMock: vi.fn(),
  }))

vi.mock('../../api', () => ({
  updateChannelModelStatus: updateStatusMock,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translateMock }),
}))

vi.mock('sonner', () => ({
  toast: {
    error: toastErrorMock,
    success: toastSuccessMock,
  },
}))

function channelWith(disabledModels: unknown[], groups = 'default'): Channel {
  return {
    id: 7,
    name: 'test-channel',
    models: 'gpt-4o,claude-3',
    group: groups,
    other_info: JSON.stringify({ disabled_models: disabledModels }),
  } as Channel
}

function renderDialog(channel: Channel) {
  const onChanged = vi.fn()
  render(
    <ChannelDisabledModelsDialog
      open
      onOpenChange={vi.fn()}
      channel={channel}
      onChanged={onChanged}
    />
  )
  return { onChanged }
}

function modelRow(model: string): HTMLElement {
  return screen.getByRole('group', { name: model })
}

describe('channel disabled models dialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    updateStatusMock.mockResolvedValue({ success: true })
  })

  test('strikes through a disabled model and marks its source', () => {
    renderDialog(
      channelWith([
        { group: 'default', model: 'gpt-4o', source: 'manual', reason: 'op 1' },
      ])
    )

    expect(within(modelRow('gpt-4o')).getByText('gpt-4o')).toHaveClass(
      'line-through'
    )
    expect(within(modelRow('claude-3')).getByText('claude-3')).not.toHaveClass(
      'line-through'
    )
    expect(screen.getByText('Manually disabled')).toBeVisible()
    expect(screen.getByText('op 1')).toBeVisible()
    expect(
      within(modelRow('gpt-4o')).getByRole('button', { name: 'Enable' })
    ).toBeVisible()
    expect(
      within(modelRow('claude-3')).getByRole('button', { name: 'Disable' })
    ).toBeVisible()
  })

  test('shows the auto source for an automatically disabled model', () => {
    renderDialog(
      channelWith([{ group: 'default', model: 'gpt-4o', source: 'auto' }])
    )

    expect(screen.getByText('Auto disabled')).toBeVisible()
  })

  test('re-enables a disabled model for the active group', async () => {
    const user = userEvent.setup()
    const { onChanged } = renderDialog(
      channelWith([{ group: 'default', model: 'gpt-4o', source: 'manual' }])
    )

    await user.click(
      within(modelRow('gpt-4o')).getByRole('button', { name: 'Enable' })
    )

    await waitFor(() =>
      expect(updateStatusMock).toHaveBeenCalledWith(7, {
        group: 'default',
        model: 'gpt-4o',
        disabled: false,
      })
    )
    expect(toastSuccessMock).toHaveBeenCalledWith('Model enabled: gpt-4o')
    expect(onChanged).toHaveBeenCalledOnce()
  })

  test('disables a healthy model for the active group', async () => {
    const user = userEvent.setup()
    const { onChanged } = renderDialog(channelWith([]))

    await user.click(
      within(modelRow('gpt-4o')).getByRole('button', { name: 'Disable' })
    )

    await waitFor(() =>
      expect(updateStatusMock).toHaveBeenCalledWith(7, {
        group: 'default',
        model: 'gpt-4o',
        disabled: true,
      })
    )
    expect(toastSuccessMock).toHaveBeenCalledWith('Model disabled: gpt-4o')
    expect(onChanged).toHaveBeenCalledOnce()
  })

  test('reports a failed toggle without refreshing the channel', async () => {
    updateStatusMock.mockResolvedValue({ success: false, message: 'denied' })
    const user = userEvent.setup()
    const { onChanged } = renderDialog(channelWith([]))

    await user.click(
      within(modelRow('gpt-4o')).getByRole('button', { name: 'Disable' })
    )

    await waitFor(() => expect(toastErrorMock).toHaveBeenCalledWith('denied'))
    expect(toastSuccessMock).not.toHaveBeenCalled()
    expect(onChanged).not.toHaveBeenCalled()
  })

  test('scopes the disabled state to the selected group', async () => {
    const user = userEvent.setup()
    renderDialog(
      channelWith(
        [{ group: 'vip', model: 'gpt-4o', source: 'manual' }],
        'default,vip'
      )
    )

    // default is the active group, so the model is not struck through there.
    expect(within(modelRow('gpt-4o')).getByText('gpt-4o')).not.toHaveClass(
      'line-through'
    )

    await user.click(screen.getByRole('combobox', { name: 'Group' }))
    await user.click(await screen.findByRole('option', { name: 'vip' }))

    expect(within(modelRow('gpt-4o')).getByText('gpt-4o')).toHaveClass(
      'line-through'
    )
    await user.click(
      within(modelRow('gpt-4o')).getByRole('button', { name: 'Enable' })
    )
    await waitFor(() =>
      expect(updateStatusMock).toHaveBeenCalledWith(7, {
        group: 'vip',
        model: 'gpt-4o',
        disabled: false,
      })
    )
  })

  test('hides the group selector for a single-group channel', () => {
    renderDialog(channelWith([]))

    expect(screen.queryByRole('combobox', { name: 'Group' })).toBeNull()
  })
})
