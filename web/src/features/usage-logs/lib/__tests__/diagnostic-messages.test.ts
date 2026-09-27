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

// The Go trees that build conversion diagnostics. The scan below reads them so
// the check is anchored to what the backend can actually record, rather than to
// the list above, which would otherwise only ever confirm itself.
const GO_SOURCE_ROOTS = ['relaykit', 'relay']

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

// Test fixtures are not diagnostics the server can emit.
const isGoSource = (name: string) =>
  name.endsWith('.go') && !name.endsWith('_test.go')

function walkGoFiles(root: string): string[] {
  const found: string[] = []
  const visit = (dir: string) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name)
      if (entry.isDirectory()) visit(full)
      else if (isGoSource(entry.name)) found.push(full)
    }
  }
  visit(root)
  return found
}

/**
 * Every Go string literal that carries a `{{name}}` slot and some prose around
 * it. A literal made of slots alone is a placeholder constant (for example the
 * notification `{{value}}`), not a diagnostic sentence.
 */
function slottedGoLiterals(): string[] {
  const literals = new Set<string>()
  for (const file of walkGoFiles(path.join(process.cwd(), '..'))) {
    const inScope = GO_SOURCE_ROOTS.some((root) =>
      file.includes(path.sep + root + path.sep)
    )
    if (!inScope) {
      continue
    }
    const source = fs.readFileSync(file, 'utf8')
    for (const match of source.matchAll(/`[^`]*`|"[^"\n]*"/g)) {
      const literal = match[0].slice(1, -1)
      if (!/\{\{[A-Za-z0-9_]+\}\}/.test(literal)) {
        continue
      }
      const prose = literal.replaceAll(/\{\{[A-Za-z0-9_]+\}\}/g, '')
      if (!/[A-Za-z]/.test(prose)) {
        continue
      }
      literals.add(literal)
    }
  }
  return [...literals].sort()
}

const goLiterals = slottedGoLiterals()
const hasGoSources = goLiterals.length > 0

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
    expect(conversionDiagnosticText(diagnostic(), 8)).toEqual({ key: message })
  })

  test('a slotted message is returned as its template plus the recorded values', () => {
    const item = diagnostic({
      code: 'claude_budget_adjusted',
      message:
        'model "claude-x" requires 1024 <= budget_tokens < max_tokens; adjusted 512 to 1024',
      message_key:
        'model "{{model}}" requires 1024 <= budget_tokens < max_tokens; adjusted {{requested}} to {{budget}}',
      params: { model: 'claude-x', requested: '512', budget: '1024' },
    })
    expect(conversionDiagnosticText(item, 8)).toEqual({
      key: item.message_key,
      params: item.params,
    })
  })

  test('an interpolated message with no recorded template keeps its English', () => {
    const message = 'Claude budget raised from 1024 to 2048'
    expect(staticDiagnosticKey(message)).toBeNull()
    expect(conversionDiagnosticText(diagnostic({ message }), 8)).toEqual({
      literal: message,
    })
  })

  test('an unrecognised message is not handed to i18next, so request text cannot nest translations', () => {
    const message = 'Responses include "$t(Login)" is not supported'
    expect(conversionDiagnosticText(diagnostic({ message }), 8)).toEqual({
      literal: message,
    })
  })

  test('the presentation-metadata loss explains itself for display-metadata logs', () => {
    const item = diagnostic({
      code: 'omitted_presentation_metadata',
      message: 'target protocol does not carry this display metadata',
    })
    expect(conversionDiagnosticText(item, 2)).toEqual({
      key: 'The target protocol does not support this display metadata; it was omitted.',
    })
    // Other log types keep the recorded wording.
    expect(conversionDiagnosticText(item, 1)).toEqual({ key: item.message })
  })
})

// The dialog translates a slotted sentence by using the template as the i18n
// key, so a template the backend can write must exist as a key everywhere. This
// reads the templates out of the Go sources on purpose: a hand-kept list would
// be checked against itself and could not catch a sentence added upstream.
describe.skipIf(!hasGoSources)('backend diagnostic templates', () => {
  test('the Go sources are reachable, so the scan is not vacuous', () => {
    expect(goLiterals.length).toBeGreaterThan(50)
  })

  test('every slotted template has a translation in every locale', () => {
    for (const locale of LOCALES) {
      const t = translations(locale)
      const missing = goLiterals.filter(
        (template) => typeof t[template] !== 'string' || !t[template].trim()
      )
      expect(
        missing,
        `${locale} is missing backend diagnostic template translations`
      ).toEqual([])
    }
  })

  test('every translation keeps exactly the slots its template declares', () => {
    const slotPattern = /\{\{[A-Za-z0-9_]+\}\}/g
    for (const locale of LOCALES) {
      const t = translations(locale)
      for (const template of goLiterals) {
        const expected = new Set(template.match(slotPattern) ?? [])
        const actual = new Set(t[template]?.match(slotPattern) ?? [])
        expect(
          [...actual].sort(),
          `${locale} changed the slots of ${JSON.stringify(template)}`
        ).toEqual([...expected].sort())
      }
    }
  })
})
