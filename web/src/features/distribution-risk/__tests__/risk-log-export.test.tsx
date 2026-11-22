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
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { listDistributionRiskEvents } from '../api'
import { DistributionRiskExportDialog } from '../components/distribution-risk-export-dialog'
import {
  buildDistributionRiskCsv,
  fetchAllDistributionRiskEvents,
} from '../lib/export'
import type { DistributionRiskEvent } from '../types'

vi.mock('../api', () => ({
  listDistributionRiskEvents: vi.fn(),
}))

const createObjectURL = vi.fn((_blob: Blob) => 'blob:risk-logs')

beforeEach(() => {
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    value: createObjectURL,
  })
  Object.defineProperty(URL, 'revokeObjectURL', {
    configurable: true,
    value: vi.fn(),
  })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
})

afterEach(() => {
  cleanup()
  Reflect.deleteProperty(URL, 'createObjectURL')
  Reflect.deleteProperty(URL, 'revokeObjectURL')
  vi.restoreAllMocks()
})

const event = (id: number): DistributionRiskEvent => ({
  id,
  user_id: 17,
  username: 'alice',
  tokens: [{ id: 24, name: 'production' }],
  score: 88,
  level: 'high',
  category_scores: { network: 41, session: 47 },
  status: 'confirmed',
  created_at: 1_760_000_000,
  reviewed_at: 1_760_000_100,
  reviewer_id: 3,
  review_reason: '=HYPERLINK("https://example.invalid")',
  evidence: {
    ips: ['198.51.100.25'],
    user_agents: ['synthetic-browser'],
    session_hashes: ['session-hash-value'],
  },
})

describe('risk log export data', () => {
  it('stops when an export is cancelled while a page is loading', async () => {
    const controller = new AbortController()
    const fetchPage = vi.fn(async () => {
      controller.abort(new Error('cancelled by operator'))
      return {
        items: [event(1)],
        total: 1,
        pending_count: 0,
        page: 1,
        page_size: 100,
      }
    })

    await expect(
      fetchAllDistributionRiskEvents(fetchPage, controller.signal, vi.fn())
    ).rejects.toThrow('cancelled by operator')
  })

  it('pages through all results and ignores duplicate events', async () => {
    const first = event(1)
    const second = event(2)
    const fetchPage = vi.fn(
      async (page: number, pageSize: number, _signal: AbortSignal) => ({
        items: page === 1 ? [first, second] : [second, event(3)],
        total: 101,
        pending_count: 0,
        page,
        page_size: pageSize,
      })
    )
    const onProgress = vi.fn()

    const result = await fetchAllDistributionRiskEvents(
      fetchPage,
      new AbortController().signal,
      onProgress
    )

    expect(result.map((item) => item.id)).toEqual([1, 2, 3])
    expect(
      fetchPage.mock.calls.map(([page, pageSize]) => [page, pageSize])
    ).toEqual([
      [1, 100],
      [2, 100],
    ])
    expect(onProgress.mock.calls.map(([count]) => count)).toEqual([2, 3])
  })

  it('exports concise log fields while excluding event IDs and client fingerprints', () => {
    const lines = buildDistributionRiskCsv([event(42)], (key) => key)
    const csv = lines.join('')

    expect(lines[0]).not.toContain('"Risk event ID"')
    expect(lines[0]).not.toContain('"Scope"')
    expect(lines[0]).toContain('"User"')
    expect(lines[0]).toContain('"Token"')
    expect(lines[0]).toContain('"Reviewed at"')
    expect(lines[0]).toContain('"Reviewer ID"')
    expect(csv).toContain('"alice"')
    expect(csv).toContain('"production (#24)"')
    expect(csv).not.toContain('"42"')
    expect(csv).not.toContain('"17"')
    expect(csv).toContain("'=HYPERLINK")
    expect(csv).not.toContain('198.51.100.25')
    expect(csv).not.toContain('synthetic-browser')
    expect(csv).not.toContain('session-hash-value')
  })
})

describe('risk log export dialog', () => {
  it('exports the applied filters and downloads the returned records', async () => {
    const startTimestamp = Math.floor(Date.now() / 1000) - 3600
    const endTimestamp = Math.floor(Date.now() / 1000) + 3600
    vi.mocked(listDistributionRiskEvents).mockResolvedValue({
      items: [event(42)],
      total: 1,
      pending_count: 0,
      page: 1,
      page_size: 100,
    })
    const user = userEvent.setup()

    render(
      <DistributionRiskExportDialog
        filters={{
          status: 'pending',
          level: 'high',
          username: 'alice',
          start_timestamp: startTimestamp,
          end_timestamp: endTimestamp,
        }}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Export' }))
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Export' }))

    await waitFor(() => expect(createObjectURL).toHaveBeenCalledTimes(1))
    expect(listDistributionRiskEvents).toHaveBeenCalledWith(
      expect.objectContaining({
        status: 'pending',
        level: 'high',
        username: 'alice',
        start_timestamp: startTimestamp,
        end_timestamp: endTimestamp,
      }),
      1,
      100,
      expect.any(AbortSignal)
    )
    const blob = createObjectURL.mock.calls[0][0]
    const csv = await blob.text()
    expect(csv).toContain('"alice"')
    expect(csv).toContain('"\'=HYPERLINK')
    expect(csv).not.toContain('198.51.100.25')
  })

  it('blocks an export whose selected date range exceeds 365 days', async () => {
    const endTimestamp = Math.floor(Date.now() / 1000)
    const user = userEvent.setup()

    render(
      <DistributionRiskExportDialog
        filters={{
          status: 'all',
          level: 'all',
          username: '',
          start_timestamp: endTimestamp - 366 * 24 * 60 * 60,
          end_timestamp: endTimestamp,
        }}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Export' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The time range cannot exceed 365 days'
    )
    expect(
      within(screen.getByRole('dialog')).getByRole('button', {
        name: 'Export',
      })
    ).toBeDisabled()
    expect(listDistributionRiskEvents).not.toHaveBeenCalled()
  })
})
