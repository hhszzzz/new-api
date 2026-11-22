import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { channelSchema, type Channel } from '../../types'
import { useChannelsColumns } from '../channels-columns'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', resolvedLanguage: 'en' },
  }),
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

vi.mock('../channels-provider', () => ({
  useChannels: () => ({ sensitiveVisible: false }),
}))

function channelWithClientPolicy(policy?: {
  mode: string
  clients: string[]
}): Channel {
  return channelSchema.parse({
    id: 1,
    type: 1,
    key: 'test-key',
    name: 'Test channel',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    settings: policy ? JSON.stringify({ client_policy: policy }) : '{}',
  })
}

function ChannelNameCell(props: { channel: Channel }) {
  // eslint-disable-next-line react/incompatible-library -- This test exercises one static TanStack Table row without compiler memoization.
  const table = useReactTable({
    data: [props.channel],
    columns: useChannelsColumns({ enableSelection: false }),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getVisibleCells()
    .find((item) => item.column.id === 'name')

  return cell?.column.columnDef.cell ? (
    <>{flexRender(cell.column.columnDef.cell, cell.getContext())}</>
  ) : null
}

describe('channel client policy marker', () => {
  test('marks concrete channel rows with their configured policy', () => {
    render(
      <ChannelNameCell
        channel={channelWithClientPolicy({
          mode: 'deny',
          clients: ['claude_code'],
        })}
      />
    )

    expect(screen.getByTestId('client-policy-indicator')).toHaveAttribute(
      'data-policy-mode',
      'deny'
    )
  })

  test('leaves unrestricted channels without a marker', () => {
    render(<ChannelNameCell channel={channelWithClientPolicy()} />)

    expect(screen.queryByTestId('client-policy-indicator')).toBeNull()
  })

  test('does not mark collapsed channel aggregates', () => {
    const channel = channelWithClientPolicy({
      mode: 'allow',
      clients: ['codex'],
    })
    const aggregate = {
      ...channel,
      id: -2,
      name: 'Combined channels',
      key: 'aggregate:2',
      children: [channel],
      row_kind: 'channel_aggregate',
    } as unknown as Channel

    render(<ChannelNameCell channel={aggregate} />)

    expect(screen.queryByTestId('client-policy-indicator')).toBeNull()
  })
})
