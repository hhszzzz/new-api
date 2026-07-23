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
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { useRankings } from '../hooks/use-rankings'
import { Rankings } from '../index'

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
  useSearch: () => ({ period: 'week' }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: ReactNode }) => props.children,
}))

vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: ReactNode }) => props.children,
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: <T,>(selector: (state: { auth: { user: null } }) => T) =>
    selector({ auth: { user: null } }),
}))

vi.mock('../components', () => ({
  MarketShareSection: () => null,
  ModelsSection: () => null,
  PulseSection: () => null,
  RankingsHero: () => null,
  UserUsageSection: () => null,
}))

vi.mock('../hooks/use-rankings', () => ({
  useRankings: vi.fn(),
}))

const mockedUseRankings = vi.mocked(useRankings)

beforeEach(() => {
  mockedUseRankings.mockReset()
})

describe('rankings request states', () => {
  test('announces loading while the rankings request is pending', () => {
    mockedUseRankings.mockReturnValue({
      data: undefined,
      error: null,
      isLoading: true,
    } as ReturnType<typeof useRankings>)

    render(<Rankings />)

    expect(screen.getByRole('status', { name: 'Loading...' })).toBeVisible()
  })

  test('surfaces the request error in an alert', () => {
    mockedUseRankings.mockReturnValue({
      data: undefined,
      error: new Error('rankings unavailable'),
      isLoading: false,
    } as ReturnType<typeof useRankings>)

    render(<Rankings />)

    expect(
      screen.getByRole('alert', { name: 'Unable to load rankings' })
    ).toBeVisible()
    expect(screen.getByText('rankings unavailable')).toBeVisible()
  })
})
