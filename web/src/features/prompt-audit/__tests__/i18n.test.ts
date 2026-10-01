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

import { getPromptAuditProtocolName, PROMPT_AUDIT_PROTOCOLS } from '../lib'

const SOURCE_FILES = [
  '../settings.tsx',
  '../records.tsx',
  '../wordlists.tsx',
  '../scopes.ts',
  '../lib.ts',
  '../components/prompt-audit-group-detail-sheet.tsx',
  '../components/prompt-audit-content-version.tsx',
  '../components/prompt-audit-session-questions.tsx',
  '../components/prompt-audit-outcome-counts.tsx',
  '../components/prompt-audit-payload-view.tsx',
  '../test-failure.ts',
  '../components/scope-policies-section.tsx',
  '../components/advanced-limits-section.tsx',
  '../components/audit-models-section.tsx',
  '../components/audit-model-card.tsx',
  '../components/enforcement-section.tsx',
  '../components/number-field.tsx',
  '../components/wordlist-import-dialog.tsx',
  '../components/wordlist-test-card.tsx',
  '../components/manual-wordlist-dialog.tsx',
  '../components/prompt-audit-navigation.tsx',
  '../components/prompt-audit-detail-sheet.tsx',
  '../components/prompt-audit-filter-bar.tsx',
  '../components/prompt-audit-columns.tsx',
  '../components/prompt-audit-delete-dialog.tsx',
  '../components/stored-prompt-limit-field.tsx',
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
  'Summary continuation',
  'Continuation linkage unresolved',
  'Read prompt audits',
  'View prompt audit lists, statistics, decisions, and redacted previews.',
  'View full audited prompts',
  'View retained cleartext prompts in prompt audit details.',
  'Manage prompt audit',
  'Change prompt audit configuration, test nodes, and retry failed jobs.',
  'Delete prompt audits',
  'Preview and delete terminal prompt audit records.',
  // Protocol labels are looked up from the stored protocol value.
  ...PROMPT_AUDIT_PROTOCOLS.map((protocol) =>
    getPromptAuditProtocolName(protocol)
  ),
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

  // One verdict must read one word wherever it appears. The decision keys carry
  // the verdict on a record, a filter, and a detail sheet, while the count keys
  // carry the same verdict inside a merged row. They were authored separately,
  // so the same "pass" could read 放行 on one screen and 通过 in a group count.
  test.each([
    ['pass', 'Passed'],
    ['block', 'Blocked'],
    ['flag', 'Flagged'],
  ])(
    '%s and %s name the verdict identically in every locale',
    (decision, count) => {
      const mismatches: string[] = []
      for (const locale of LOCALES) {
        const { translation } = JSON.parse(
          fs.readFileSync(
            path.join(process.cwd(), `src/i18n/locales/${locale}.json`),
            'utf8'
          )
        ) as { translation: Record<string, string> }
        if (
          typeof translation[decision] !== 'string' ||
          typeof translation[count] !== 'string' ||
          translation[decision] !== translation[count]
        ) {
          mismatches.push(
            `${locale}: ${decision}=${translation[decision]} vs ${count}=${translation[count]}`
          )
        }
      }
      expect(mismatches).toEqual([])
    }
  )
})
