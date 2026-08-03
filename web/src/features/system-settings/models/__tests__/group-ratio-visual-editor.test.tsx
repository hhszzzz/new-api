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
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { GroupRatioVisualEditor } from '../group-ratio-visual-editor'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('group pricing authorization presentation', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }))
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does not turn legacy descriptions into editable pricing groups', () => {
    render(
      <GroupRatioVisualEditor
        groupRatio='{"default":1}'
        userUsableGroups='{"default":"Default","vip":"Legacy VIP"}'
        groupGroupRatio='{}'
        autoGroups='["default","vip"]'
        maxTokenAutoGroupsField={null}
        onChange={vi.fn()}
      />
    )

    expect(screen.getByDisplayValue('default')).toBeInTheDocument()
    expect(screen.queryByDisplayValue('vip')).not.toBeInTheDocument()
    expect(screen.queryByText('User selectable')).not.toBeInTheDocument()
    expect(
      screen.getByText(
        'Edit billing ratios and descriptions for administrator-assigned groups.'
      )
    ).toBeInTheDocument()
  })
})
