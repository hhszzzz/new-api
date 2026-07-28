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
import { describe, expect, test } from 'vitest'

import {
  buildChannelAggregateMergeParams,
  channelBatchMergeFormSchema,
} from '../channel-batch-merge'

describe('channel batch merge request', () => {
  test('targets an existing aggregate and normalizes selected channel IDs', () => {
    const values = channelBatchMergeFormSchema.parse({
      target_mode: 'existing',
      aggregate_id: 8,
      name: '',
      base_url: '',
      remark: '',
      inherit_aggregate_base_url: true,
    })

    expect(buildChannelAggregateMergeParams(values, [4, -1, 2, 4])).toEqual({
      ids: [2, 4],
      aggregate_id: 8,
      inherit_aggregate_base_url: true,
    })
  })

  test('creates a normalized aggregate in the same merge request', () => {
    const values = channelBatchMergeFormSchema.parse({
      target_mode: 'new',
      aggregate_id: null,
      name: '  Shared endpoint  ',
      base_url: ' https://shared.example.com/v1/ ',
      remark: '  selected providers  ',
      inherit_aggregate_base_url: false,
    })

    expect(buildChannelAggregateMergeParams(values, [7, 6])).toEqual({
      ids: [6, 7],
      new_aggregate: {
        name: 'Shared endpoint',
        base_url: 'https://shared.example.com/v1/',
        remark: 'selected providers',
      },
      inherit_aggregate_base_url: false,
    })
  })
})
