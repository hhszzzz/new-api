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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { useCommonLogsColumns } from '../components/columns/common-logs-columns'
import { UsageLogsProvider } from '../components/usage-logs-provider'
import type { UsageLog } from '../data/schema'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

const routedChannelLog: UsageLog = {
  id: 1,
  user_id: 1,
  created_at: 1_800_000_000,
  type: 2,
  content: '',
  username: 'tester',
  token_name: 'test-token',
  model_name: 'demo-gpt-5.4',
  quota: 0,
  prompt_tokens: 0,
  completion_tokens: 0,
  use_time: 0,
  is_stream: false,
  channel: 4,
  channel_name: 'Demo route pool',
  token_id: 1,
  group: 'default',
  ip: '',
  request_id: '',
  upstream_request_id: '',
  other: JSON.stringify({
    admin_info: {
      surface_channel_name: 'Demo route pool',
      actual_channel_id: 4,
      actual_channel_name: 'OpenAI child #4',
      retry_chain: ['2', '3', '4'],
    },
  }),
}

function ChannelCellHarness() {
  const columns = useCommonLogsColumns(true, true)
  const table = useReactTable({
    data: [routedChannelLog],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  const channelCell = table
    .getRowModel()
    .rows[0]?.getVisibleCells()
    .find((cell) => cell.column.id === 'channel')

  if (!channelCell) throw new Error('Expected channel column')
  return flexRender(channelCell.column.columnDef.cell, channelCell.getContext())
}

describe('usage-log channel route presentation', () => {
  test('opens surface and actual channel details from the channel badge', async () => {
    const user = userEvent.setup()
    render(
      <UsageLogsProvider>
        <ChannelCellHarness />
      </UsageLogsProvider>
    )

    expect(screen.getByText('Demo route pool')).toBeVisible()
    expect(screen.getByText('#4')).toBeVisible()
    expect(screen.queryByText('OpenAI child #4')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Demo route pool' }))

    expect(await screen.findByText('OpenAI child #4')).toBeVisible()
    expect(screen.getByText('2 → 3 → 4')).toBeVisible()
    expect(screen.getByTestId('actual-channel-row')).toHaveClass('items-center')
    expect(screen.getByTestId('actual-channel-id')).toHaveClass(
      'h-4',
      'px-1',
      '!text-[11px]'
    )
  })
})
