/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

import type { Channel, ChannelAggregate } from '../../types'
import {
  aggregateChannelsByAggregate,
  aggregateChannelsByTag,
  getChannelAggregateById,
  isChannelAggregateRow,
  isTagAggregateRow,
} from '../channel-utils'

function channel(id: number, overrides: Partial<Channel> = {}): Channel {
  const baseChannel: Channel = {
    id,
    type: 1,
    key: '',
    status: 1,
    name: `channel-${id}`,
    weight: 10,
    created_time: 0,
    test_time: 0,
    response_time: 100,
    base_url: '',
    other: '',
    balance: 2,
    balance_updated_time: 0,
    models: 'demo-model',
    group: 'default',
    used_quota: 100,
    priority: 10,
    other_info: '',
    remark: '',
    inherit_aggregate_base_url: false,
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    schedule: {
      timezone: 'Asia/Shanghai',
      weekly_enabled: false,
      weekly_windows: {},
    },
    settings: '{}',
  }
  return {
    ...baseChannel,
    ...overrides,
    schedule: overrides.schedule ?? baseChannel.schedule,
  }
}

describe('channel aggregate presentation tree', () => {
  test('resolves the selected parent by id so the editor can show its name', () => {
    const aggregates: ChannelAggregate[] = [
      {
        id: 8,
        name: 'Shared upstream',
        base_url: 'https://shared.example/v1',
        remark: '',
        created_at: 0,
        updated_at: 0,
        child_count: 2,
      },
    ]

    expect(getChannelAggregateById(aggregates, 8)?.name).toBe('Shared upstream')
    expect(getChannelAggregateById(aggregates, null)).toBeUndefined()
    expect(getChannelAggregateById(aggregates, 99)).toBeUndefined()
  })

  test('folds concrete children under one parent while leaving ungrouped channels flat', () => {
    const rows = aggregateChannelsByAggregate([
      channel(1, {
        aggregate_id: 8,
        aggregate_name: 'Shared upstream',
        aggregate_base_url: 'https://shared.example/v1',
        group: 'default',
        models: 'demo-a',
        used_quota: 100,
      }),
      channel(2, {
        aggregate_id: 8,
        aggregate_name: 'Shared upstream',
        group: 'vip',
        models: 'demo-b',
        used_quota: 250,
      }),
      channel(3),
    ])

    expect(rows).toHaveLength(2)
    expect(isChannelAggregateRow(rows[0])).toBe(true)
    if (!isChannelAggregateRow(rows[0])) throw new Error('missing parent row')

    expect(rows[0].aggregate_id).toBe(8)
    expect(rows[0].aggregate_name).toBe('Shared upstream')
    expect(rows[0].children.map((child) => child.id)).toEqual([1, 2])
    expect(rows[0].used_quota).toBe(350)
    expect(rows[0].group.split(',').sort()).toEqual(['default', 'vip'])
    expect(rows[0].models.split(',').sort()).toEqual(['demo-a', 'demo-b'])
    expect(rows[1].id).toBe(3)
  })

  test('keeps tag rows and channel aggregate rows as distinct row kinds', () => {
    const aggregateRows = aggregateChannelsByAggregate([
      channel(1, { aggregate_id: 4, aggregate_name: 'Parent' }),
    ])
    const tagRows = aggregateChannelsByTag([channel(2, { tag: 'batch-a' })])

    expect(isChannelAggregateRow(aggregateRows[0])).toBe(true)
    expect(isTagAggregateRow(aggregateRows[0])).toBe(false)
    expect(isTagAggregateRow(tagRows[0])).toBe(true)
    expect(isChannelAggregateRow(tagRows[0])).toBe(false)
  })

  test('counts only effectively available children as active', () => {
    const rows = aggregateChannelsByAggregate([
      channel(1, {
        aggregate_id: 8,
        aggregate_name: 'Shared upstream',
        effective_status: 'enabled',
      }),
      channel(2, {
        aggregate_id: 8,
        aggregate_name: 'Shared upstream',
        status: 1,
        effective_status: 'scheduled_disabled',
      }),
    ])

    expect(isChannelAggregateRow(rows[0])).toBe(true)
    if (!isChannelAggregateRow(rows[0])) throw new Error('missing parent row')

    expect(rows[0].children).toHaveLength(2)
    expect(rows[0].active_count).toBe(1)
    expect(rows[0].status).toBe(1)
  })
})
