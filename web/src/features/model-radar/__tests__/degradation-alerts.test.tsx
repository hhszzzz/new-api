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

import { DegradationAlerts } from '../components/degradation-alerts'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    i18n: { language: 'en', resolvedLanguage: 'en' },
    t: (key: string, values?: Record<string, string | number>) =>
      key.replaceAll(/\{\{(\w+)\}\}/g, (_, name: string) =>
        String(values?.[name] ?? `{{${name}}}`)
      ),
  }),
}))
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: (iconName: string) => <svg data-icon-key={iconName} />,
}))

describe('model radar degradation alerts', () => {
  test('renders nothing when there are no alerts', () => {
    const view = render(
      <DegradationAlerts alerts={[]} history={[]} configurations={[]} />
    )
    expect(view.container).toBeEmptyDOMElement()
  })
  test('renders a negative degradation as an improvement with the correct prior IQ', () => {
    render(
      <DegradationAlerts
        alerts={[
          {
            model: 'radar-model',
            effort: 'low',
            iq: 39,
            degradation_12h_iq: 6.5,
            degradation_24h_iq: 6.5,
            degradation_48h_iq: -0.2,
          },
        ]}
        configurations={[]}
        history={[]}
      />
    )

    const alert = screen.getByRole('article', { name: 'radar-model low' })
    expect(within(alert).getByText('48 hours ago 38.8')).toBeVisible()
    expect(within(alert).getByText('+0.2')).toBeVisible()
    expect(within(alert).getAllByText('-6.5')).toHaveLength(2)
  })

  test('renders the vendor configured icon variant in degradation alerts', () => {
    const view = render(
      <DegradationAlerts
        alerts={[
          {
            model: 'deepseek-v3.2',
            effort: 'low',
            iq: 75,
            degradation_12h_iq: 1,
            degradation_24h_iq: 2,
            degradation_48h_iq: 3,
          },
        ]}
        configurations={[]}
        history={[]}
        iconRegistry={{
          modelIcons: new Map<string, string>(),
          providerIcons: new Map([['deepseek', 'DeepSeek.Color']]),
        }}
      />
    )

    expect(
      view.container.querySelector('[data-icon-key="DeepSeek.Color"]')
    ).not.toBeNull()
  })

  test('shows insufficient data when upstream has no 48-hour window for a new tier', () => {
    render(
      <DegradationAlerts
        alerts={[
          {
            model: 'grok-4.7',
            effort: 'medium',
            iq: 105,
            degradation_12h_iq: 45,
            degradation_24h_iq: 45,
            degradation_48h_iq: null,
            average_iq_24h: 142.81,
            average_iq_48h: null,
          },
        ]}
        configurations={[]}
        history={[]}
      />
    )

    const alert = screen.getByRole('article', { name: 'grok-4.7 medium' })
    expect(
      within(alert).getByText('48 hours ago Insufficient data')
    ).toBeVisible()
    const fortyEightHours = within(alert).getByText('48 hours').closest('div')
    expect(fortyEightHours).not.toBeNull()
    expect(
      within(fortyEightHours as HTMLElement).getByText('Insufficient data')
    ).toBeVisible()
    expect(within(alert).getAllByText('-45.0')).toHaveLength(2)
    expect(within(alert).getByText('Average 142.81')).toBeVisible()
  })

  test('charts the upstream hourly trend and starts the 48-hour baseline from it', () => {
    render(
      <DegradationAlerts
        alerts={[
          {
            model: 'grok-4.7',
            effort: 'xhigh',
            iq: 117,
            degradation_12h_iq: 19,
            degradation_24h_iq: 19,
            degradation_48h_iq: 19,
            trend_48h: [
              { ts: 1_790_000_000, iq: 150, samples: 2 },
              { ts: 1_790_003_600, iq: 128.6, samples: 7 },
              { ts: 1_790_007_200, iq: 117, samples: 9 },
            ],
          },
        ]}
        configurations={[]}
        history={[]}
      />
    )

    const alert = screen.getByRole('article', { name: 'grok-4.7 xhigh' })
    expect(
      within(alert).getByRole('img', {
        name: '48-hour IQ trend for grok-4.7 xhigh',
      })
    ).toBeVisible()
    expect(within(alert).getByText('48 hours ago 150')).toBeVisible()
  })

  test('reports insufficient data when the trend has a single reading', () => {
    render(
      <DegradationAlerts
        alerts={[
          {
            model: 'grok-4.7',
            effort: 'low',
            iq: 96,
            degradation_12h_iq: 5.1,
            degradation_24h_iq: 5.1,
            degradation_48h_iq: null,
            trend_48h: [{ ts: 1_790_000_000, iq: 96, samples: 53 }],
          },
        ]}
        configurations={[]}
        history={[]}
      />
    )

    const alert = screen.getByRole('article', { name: 'grok-4.7 low' })
    expect(within(alert).queryByRole('img')).toBeNull()
    expect(within(alert).getAllByText('Insufficient data').length).toBe(2)
  })
})
