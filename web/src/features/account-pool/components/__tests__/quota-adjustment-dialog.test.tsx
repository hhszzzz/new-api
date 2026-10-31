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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { AccountPoolAccount } from '../../types'
import { QuotaAdjustmentDialog } from '../quota-adjustment-dialog'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { resolvedLanguage: 'en', language: 'en' },
  }),
}))

function account(
  override?: AccountPoolAccount['quota_override']
): AccountPoolAccount {
  return {
    public_id: 'aaaaaaaaaaaaaaaaaaaaaaaa',
    display_name: 'Codex #1',
    provider: 'codex',
    status: 'available',
    plan: 'plus',
    subscription_active_until: null,
    primary_window: null,
    secondary_window: null,
    window_groups: [],
    updated_at: '2026-10-31T12:00:00Z',
    stale: false,
    quota_override: override,
  }
}

function renderDialog(
  props: Partial<{
    item: AccountPoolAccount
    onSave: (input: unknown) => Promise<boolean>
    onClear: () => Promise<boolean>
  }> = {}
) {
  const onSave = props.onSave ?? vi.fn(async () => true)
  const onClear = props.onClear ?? vi.fn(async () => true)
  const onClose = vi.fn()
  render(
    <QuotaAdjustmentDialog
      account={props.item ?? account()}
      pending={false}
      onClose={onClose}
      onSave={onSave}
      onClear={onClear}
    />
  )
  return { onSave, onClear, onClose }
}

afterEach(cleanup)

describe('account pool quota adjustment dialog', () => {
  test('submits an offset and a manual-refresh pin', async () => {
    const user = userEvent.setup()
    const { onSave } = renderDialog()

    const offset = screen.getByLabelText('Remaining percentage offset')
    await user.clear(offset)
    await user.type(offset, '25')
    await user.click(
      screen.getByRole('switch', { name: 'Pin remaining at 100%' })
    )
    await user.click(
      screen.getByRole('button', { name: 'Next manual refresh' })
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        offset_percent: 25,
        pinned: true,
        pin_release_mode: 'manual_refresh',
        pin_release_at: null,
      })
    )
  })

  test('requires a future release time for a time-based pin', async () => {
    const user = userEvent.setup()
    const { onSave } = renderDialog()

    await user.click(
      screen.getByRole('switch', { name: 'Pin remaining at 100%' })
    )
    await user.click(
      screen.getByRole('button', { name: 'At a specified time' })
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByText('Select a release time.')).toBeVisible()
    expect(onSave).not.toHaveBeenCalled()
  })

  test('clears an existing adjustment', async () => {
    const user = userEvent.setup()
    const { onClear, onClose } = renderDialog({
      item: account({
        offset_percent: 10,
        pinned: false,
        pin_release_mode: null,
        pin_release_at: null,
      }),
    })

    await user.click(screen.getByRole('button', { name: 'Clear adjustment' }))

    await waitFor(() => expect(onClear).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })
})
