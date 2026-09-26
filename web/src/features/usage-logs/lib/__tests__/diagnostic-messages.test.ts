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
import fs from 'node:fs'
import path from 'node:path'

import { describe, expect, test } from 'vitest'

import type { ConversionDiagnostic } from '../../types'
import {
  conversionDiagnosticText,
  STATIC_DIAGNOSTIC_MESSAGES,
  staticDiagnosticKey,
} from '../diagnostic-messages'

const LOCALES = ['en', 'zh', 'zh-TW', 'fr', 'ru', 'ja', 'vi']

function translations(locale: string): Record<string, string> {
  return (
    JSON.parse(
      fs.readFileSync(
        path.join(process.cwd(), `src/i18n/locales/${locale}.json`),
        'utf8'
      )
    ) as { translation: Record<string, string> }
  ).translation
}

function diagnostic(
  overrides: Partial<ConversionDiagnostic> = {}
): ConversionDiagnostic {
  return {
    code: 'hosted_tool_unpaired',
    message:
      'hosted-tool output has no id for pairing the call with its result',
    severity: 'warning',
    ...overrides,
  }
}

describe('conversionDiagnosticText', () => {
  test('every listed message has a non-empty translation in every locale', () => {
    expect(STATIC_DIAGNOSTIC_MESSAGES.length).toBeGreaterThan(0)
    for (const locale of LOCALES) {
      const t = translations(locale)
      const missing = STATIC_DIAGNOSTIC_MESSAGES.filter(
        (message) => typeof t[message] !== 'string' || !t[message].trim()
      )
      expect(
        missing,
        `${locale} is missing conversion diagnostic translations`
      ).toEqual([])
    }
  })

  test('listed messages are unique, so one message maps to exactly one key', () => {
    expect(new Set(STATIC_DIAGNOSTIC_MESSAGES).size).toBe(
      STATIC_DIAGNOSTIC_MESSAGES.length
    )
  })

  test('a static message is returned as its own i18n key', () => {
    const message =
      'hosted-tool output has no id for pairing the call with its result'
    expect(staticDiagnosticKey(message)).toBe(message)
    expect(conversionDiagnosticText(diagnostic(), 8)).toBe(message)
  })

  test('an interpolated message keeps the recorded English instead of a raw key', () => {
    const message = 'Claude budget raised from 1024 to 2048'
    expect(staticDiagnosticKey(message)).toBeNull()
    expect(conversionDiagnosticText(diagnostic({ message }), 8)).toBe(message)
  })

  test('the presentation-metadata loss explains itself for display-metadata logs', () => {
    const item = diagnostic({
      code: 'omitted_presentation_metadata',
      message: 'target protocol does not carry this display metadata',
    })
    expect(conversionDiagnosticText(item, 2)).toBe(
      'The target protocol does not support this display metadata; it was omitted.'
    )
    // Other log types keep the recorded wording.
    expect(conversionDiagnosticText(item, 1)).toBe(item.message)
  })
})
