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
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import type { Channel } from '../../types'
import { useChannelsColumns } from '../channels-columns'

const { translateMock } = vi.hoisted(() => ({
  translateMock: (key: string, values?: Record<string, string | number>) =>
    Object.entries(values || {}).reduce(
      (result, [name, value]) => result.replace(`{{${name}}}`, String(value)),
      key
    ),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: translateMock,
    i18n: { language: 'en', resolvedLanguage: 'en' },
  }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

vi.mock('../channels-provider', () => ({
  useChannels: () => ({ sensitiveVisible: false }),
}))

function modelChannel(group: string, disabledModels: unknown[] = []): Channel {
  return {
    id: 1,
    name: 'channel',
    type: 1,
    status: 1,
    models: 'gpt-4o',
    group,
    other_info: JSON.stringify({ disabled_models: disabledModels }),
  } as Channel
}

function ModelsCell({ channel }: { channel: Channel }) {
  const columns = useChannelsColumns({ enableSelection: false })
  // eslint-disable-next-line react/incompatible-library -- the test renders a single static table row and never memoizes its options.
  const table = useReactTable({
    data: [channel],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getVisibleCells()
    .find((item) => item.column.id === 'models')
  if (!cell?.column.columnDef.cell) return null
  return <>{flexRender(cell.column.columnDef.cell, cell.getContext())}</>
}

function renderModelCell(channel: Channel) {
  render(<ModelsCell channel={channel} />)
  // Base UI merges its trigger props onto StatusBadge, so a tooltip-wrapped
  // badge carries the tooltip-trigger slot instead of the status-badge slot.
  const badge = screen
    .getByText('gpt-4o')
    .closest('[data-slot="status-badge"], [data-slot="tooltip-trigger"]')
  expect(badge).not.toBeNull()
  return badge as HTMLElement
}

describe('channels table models column', () => {
  test('strikes through a model disabled for every group', () => {
    const model = renderModelCell(
      modelChannel('default', [
        { group: 'default', model: 'gpt-4o', source: 'manual' },
      ])
    )

    expect(model).toHaveClass('line-through')
  })

  test('strikes through a model disabled for the only group', () => {
    const model = renderModelCell(modelChannel('default'))

    expect(model).not.toHaveClass('line-through')
  })

  test('keeps a partially disabled model unstruck', () => {
    const model = renderModelCell(
      modelChannel('default,vip', [
        { group: 'vip', model: 'gpt-4o', source: 'manual' },
      ])
    )

    expect(model).not.toHaveClass('line-through')
  })

  test('explains a fully disabled model on hover', async () => {
    const user = userEvent.setup()
    renderModelCell(
      modelChannel('default', [
        { group: 'default', model: 'gpt-4o', source: 'auto' },
      ])
    )

    await user.hover(screen.getByText('gpt-4o'))

    expect(await screen.findByText('Disabled on this channel')).toBeVisible()
  })

  test('names the affected groups for a partial disable on hover', async () => {
    const user = userEvent.setup()
    renderModelCell(
      modelChannel('default,vip', [
        { group: 'vip', model: 'gpt-4o', source: 'manual' },
      ])
    )

    await user.hover(screen.getByText('gpt-4o'))

    expect(await screen.findByText('Disabled in groups: vip')).toBeVisible()
  })

  test('leaves a healthy model without a disabled tooltip', async () => {
    const user = userEvent.setup()
    renderModelCell(modelChannel('default'))

    await user.hover(screen.getByText('gpt-4o'))

    expect(screen.queryByRole('tooltip')).toBeNull()
    expect(
      within(document.body).queryByText('Disabled on this channel')
    ).toBeNull()
  })
})
