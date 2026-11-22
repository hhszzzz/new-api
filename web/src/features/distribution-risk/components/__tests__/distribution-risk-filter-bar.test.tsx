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
import type { Table } from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { DistributionRiskEvent } from '../../types'
import {
  DistributionRiskFilterBar,
  type DistributionRiskEventDraftFilters,
} from '../distribution-risk-filter-bar'

const DEFAULT_FILTERS: DistributionRiskEventDraftFilters = {
  status: 'all',
  level: 'all',
  scope_kind: 'all',
  user_id: '',
}

function FilterHarness(props: { onSearch: () => void; onReset: () => void }) {
  const [filters, setFilters] = useState(DEFAULT_FILTERS)
  const table = {
    getAllColumns: () => [],
  } as unknown as Table<DistributionRiskEvent>

  return (
    <DistributionRiskFilterBar
      table={table}
      filters={filters}
      onChange={(patch) => setFilters((current) => ({ ...current, ...patch }))}
      onSearch={props.onSearch}
      onReset={() => {
        setFilters(DEFAULT_FILTERS)
        props.onReset()
      }}
      isFetching={false}
    />
  )
}

describe('distribution risk filters', () => {
  it('applies an advanced user ID filter and resets it through the shared log toolbar', async () => {
    const user = userEvent.setup()
    const onSearch = vi.fn()
    const onReset = vi.fn()
    render(<FilterHarness onSearch={onSearch} onReset={onReset} />)

    await user.click(screen.getByRole('button', { name: 'Expand' }))
    const userID = await screen.findByRole('spinbutton', {
      name: 'Search by user ID',
    })
    await user.type(userID, '42')
    expect(userID).toHaveValue(42)

    await user.click(screen.getByRole('button', { name: 'Search' }))
    expect(onSearch).toHaveBeenCalledOnce()

    await user.click(screen.getByRole('button', { name: 'Reset' }))
    expect(userID).toHaveValue(null)
    expect(onReset).toHaveBeenCalledOnce()
  })
})
