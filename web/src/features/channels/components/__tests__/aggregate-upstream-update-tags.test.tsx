/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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

import { AggregateUpstreamUpdateTags } from '../aggregate-upstream-update-tags'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: { count?: number }) =>
      key.replace('{{count}}', String(options?.count ?? '')),
  }),
}))

describe('aggregate upstream update tags', () => {
  test('shows combined model changes and affected child counts', () => {
    render(
      <AggregateUpstreamUpdateTags
        channels={[
          {
            settings: JSON.stringify({
              upstream_model_update_check_enabled: true,
              upstream_model_update_last_detected_models: [
                'model-a',
                'model-a',
                'model-b',
              ],
              upstream_model_update_last_removed_models: ['legacy-a'],
            }),
          },
          {
            settings: JSON.stringify({
              upstream_model_update_check_enabled: true,
              upstream_model_update_last_detected_models: ['model-c'],
              upstream_model_update_last_removed_models: [
                'legacy-b',
                'legacy-c',
              ],
            }),
          },
          {
            settings: JSON.stringify({
              upstream_model_update_check_enabled: false,
              upstream_model_update_last_detected_models: ['hidden-model'],
              upstream_model_update_last_removed_models: ['hidden-legacy'],
            }),
          },
        ]}
      />
    )

    expect(
      screen.getByLabelText('Upstream Updates: +3 (2 child channels)')
    ).toHaveTextContent('+3')
    expect(
      screen.getByLabelText('Upstream Updates: -3 (2 child channels)')
    ).toHaveTextContent('-3')
  })

  test('renders no badges when child channels have no staged changes', () => {
    const { container } = render(
      <AggregateUpstreamUpdateTags
        channels={[
          {
            settings: JSON.stringify({
              upstream_model_update_check_enabled: true,
              upstream_model_update_last_detected_models: [],
              upstream_model_update_last_removed_models: [],
            }),
          },
        ]}
      />
    )

    expect(container).toBeEmptyDOMElement()
  })
})
