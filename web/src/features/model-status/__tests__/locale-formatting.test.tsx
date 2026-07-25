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
import { describe, expect, test, vi } from 'vitest'

import { ModelStatus } from '../index'

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({
    data: {
      data: {
        generated_at: 1_800_000_000,
        window_hours: 24,
        models: [],
      },
    },
    isError: false,
    isFetching: false,
    isLoading: false,
    refetch: vi.fn(),
  }),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'zhCN', resolvedLanguage: 'zhCN' },
    t: (key: string) => key,
  }),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => <span data-testid='model-icon' />,
}))

describe('model status locale formatting', () => {
  test('renders with the project zhCN language code without an Intl error', () => {
    render(<ModelStatus />)

    expect(
      screen.getByRole('heading', { name: 'Model Status' })
    ).toBeInTheDocument()
  })
})
