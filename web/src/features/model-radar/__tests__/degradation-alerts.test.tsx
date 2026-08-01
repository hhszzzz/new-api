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
})
