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

const LOCALES = ['zh', 'zh-TW', 'fr', 'ru', 'ja', 'vi']
/**
 * Keys the billing breakdown and the peak-hours pricing editor render. They
 * are listed explicitly because these screens are where a missing translation
 * leaks the raw English key into the Chinese UI instead of failing a build.
 */
const BILLING_KEYS = [
  'Billing Mode',
  'Per-token dynamic billing',
  'Per-call',
  'Per-token',
  'Group Ratio',
  'Peak hours',
  'Idle hours',
  'Standard',
  '{{tier}} multiplier',
  'Compaction method',
  'Native compaction',
  'Model compaction',
  'Peak hours pricing',
  'Peak price',
  'Standard price',
  'Advanced absolute prices',
]

function loadTranslation(locale: string): Record<string, string> {
  const file = path.resolve(process.cwd(), `src/i18n/locales/${locale}.json`)
  return JSON.parse(fs.readFileSync(file, 'utf8')).translation
}

describe('billing breakdown translations', () => {
  test.each(LOCALES)('%s defines every billing key', (locale) => {
    const translation = loadTranslation(locale)
    const missing = BILLING_KEYS.filter((key) => !(key in translation))
    expect(missing).toEqual([])
  })

  // Only multi-word phrases are checked: a single word can be a legitimate
  // loanword in the target language (fr "Standard"), so matching English there
  // does not prove the key fell through.
  test.each(LOCALES)(
    '%s does not fall back to the English source',
    (locale) => {
      const translation = loadTranslation(locale)
      const english = loadTranslation('en')
      const untranslated = BILLING_KEYS.filter(
        (key) => key.includes(' ') && translation[key] === english[key]
      )
      expect(untranslated).toEqual([])
    }
  )

  // English is the source locale and normally repeats its key verbatim, but a
  // renamed key that keeps the old English text renders the retired wording
  // (compaction "Model summary") while every other language looks correct.
  test.each(BILLING_KEYS)(
    'renders the key itself in English for "%s"',
    (key) => {
      expect(loadTranslation('en')[key]).toBe(key)
    }
  )
})
