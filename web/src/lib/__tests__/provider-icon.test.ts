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
import { describe, expect, test } from 'vitest'

import {
  resolveDefaultProviderIconKey,
  resolveProviderIconKey,
} from '../provider-icon'

describe('resolveProviderIconKey', () => {
  test('uses the configured vendor icon before a model-specific icon', () => {
    expect(resolveProviderIconKey('OpenAI', 'GPT.Color')).toBe('OpenAI')
  })

  test('falls back to the model icon when the vendor has no icon', () => {
    expect(resolveProviderIconKey(' ', 'Claude')).toBe('Claude')
  })

  test('preserves an explicitly configured vendor variant', () => {
    expect(resolveProviderIconKey('Anthropic.Color', 'Claude')).toBe(
      'Anthropic.Color'
    )
  })

  test('uses the color variant for an inferred plain provider icon', () => {
    expect(resolveDefaultProviderIconKey('Gemini')).toBe('Gemini.Color')
  })

  test.each(['OpenAI.Mono', 'Claude.Color', "XAI.Avatar.type={'platform'}"])(
    'preserves the explicit provider icon variant %s',
    (iconKey) => {
      expect(resolveDefaultProviderIconKey(iconKey)).toBe(iconKey)
    }
  )
})
