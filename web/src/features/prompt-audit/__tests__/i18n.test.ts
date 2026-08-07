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

const SOURCE_FILES = [
  '../settings.tsx',
  '../records.tsx',
  '../components/prompt-audit-detail-sheet.tsx',
  '../components/prompt-audit-delete-dialog.tsx',
]
const LOCALES = ['en', 'zh', 'zh-TW', 'fr', 'ru', 'ja', 'vi']
const DYNAMIC_KEYS = [
  'violent',
  'non_violent_illegal_acts',
  'sexual_content_or_sexual_acts',
  'pii',
  'suicide_and_self_harm',
  'unethical_acts',
  'politically_sensitive_topics',
  'copyright_violation',
  'jailbreak',
  'queued',
  'processing',
  'retry',
  'done',
  'failed',
  'pass',
  'flag',
  'block',
  'unavailable',
]

describe('prompt audit translations', () => {
  test('all static UI keys exist and are non-empty in every supported locale', () => {
    const keys = new Set<string>()
    for (const sourceFile of SOURCE_FILES) {
      const source = fs.readFileSync(
        path.join(
          process.cwd(),
          'src/features/prompt-audit/__tests__',
          sourceFile
        ),
        'utf8'
      )
      for (const match of source.matchAll(/\bt\(\s*'([^']+)'/g)) {
        keys.add(match[1])
      }
    }
    for (const key of DYNAMIC_KEYS) keys.add(key)
    expect(keys.size).toBeGreaterThan(0)

    for (const locale of LOCALES) {
      const localeData = JSON.parse(
        fs.readFileSync(
          path.join(process.cwd(), `src/i18n/locales/${locale}.json`),
          'utf8'
        )
      ) as { translation: Record<string, string> }
      const translations = localeData.translation
      const missing = [...keys].filter(
        (key) =>
          typeof translations[key] !== 'string' || !translations[key].trim()
      )
      expect(missing, `${locale} is missing prompt audit translations`).toEqual(
        []
      )
    }
  })
})
