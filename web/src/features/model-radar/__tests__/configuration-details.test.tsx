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
import { render, screen, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { ConfigurationDetails } from '../components/configuration-details'
import { configurationFixture as fixture } from './fixtures'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, string | number>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? name)
      ),
  }),
}))
vi.mock('@/hooks', () => ({ useMediaQuery: () => true }))

describe('configuration details', () => {
  test('shows the station, run windows, peak pricing, and 72-hour trend alongside efficiency metrics', () => {
    render(
      <ConfigurationDetails
        open
        onOpenChange={vi.fn()}
        configuration={{
          ...fixture,
          average_price_usd_by_band: { off_peak: 0.5, peak: 1.5 },
        }}
        history={[
          { ts: 100, points: [fixture] },
          { ts: 200, points: [{ ...fixture, iq: 80 }] },
        ]}
      />
    )
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAccessibleName('gpt-radar medium OpenAI')
    expect(within(dialog).getByText('Runs 24h / 48h / total')).toBeVisible()
    expect(within(dialog).getByText('3 / 6 / 12')).toBeVisible()
    expect(
      within(dialog).getByText('Off-peak $0.50 · Peak $1.50')
    ).toBeVisible()
    expect(
      within(dialog).getByRole('img', {
        name: '72-hour IQ trend for gpt-radar medium',
      })
    ).toBeVisible()
    expect(
      within(dialog).getByRole('progressbar', { name: 'Pass rate' })
    ).toHaveAttribute('aria-valuenow', '70')
  })

  test('renders legacy missing station metrics and absent history without NaN or undefined', () => {
    // Simulate a response from a pre-upgrade server.
    const {
      harness: _harness,
      runs_24h: _daily,
      runs_48h: _twoDays,
      average_price_usd_by_band: _band,
      ...legacy
    } = fixture
    render(
      <ConfigurationDetails
        open
        onOpenChange={vi.fn()}
        configuration={legacy as typeof fixture}
        history={[]}
      />
    )
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAccessibleName('gpt-radar medium OpenAI')
    expect(within(dialog).getByText('— / — / 12')).toBeVisible()
    expect(within(dialog).getByText('No history data available')).toBeVisible()
    expect(dialog).not.toHaveTextContent(/NaN|undefined|Off-peak|Peak/)
  })
})
