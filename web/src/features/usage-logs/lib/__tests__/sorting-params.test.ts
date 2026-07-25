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

import { buildApiParams, buildBaseParams } from '../utils'

describe('usage log sorting parameters', () => {
  test('common logs send the selected server sort with pagination', () => {
    const params = buildApiParams({
      page: 3,
      pageSize: 50,
      searchParams: {},
      isAdmin: true,
      sortBy: 'quota',
      sortOrder: 'asc',
    })

    expect(params).toMatchObject({
      p: 3,
      page_size: 50,
      sort_by: 'quota',
      sort_order: 'asc',
    })
  })

  test('task and drawing logs preserve their category sort fields', () => {
    const params = buildBaseParams({
      page: 2,
      pageSize: 20,
      searchParams: {},
      sortBy: 'submit_time',
      sortOrder: 'desc',
    })

    expect(params).toMatchObject({
      p: 2,
      page_size: 20,
      sort_by: 'submit_time',
      sort_order: 'desc',
    })
  })
})
