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

import type { AccountPoolAccount } from '../../types'
import {
  formatAccountPoolCountdown,
  getAccountPoolSecondaryWindowLabel,
  getAccountPoolWindowGroupLabelKey,
  getAccountPoolWindowGroups,
} from '../quota'

describe('account pool quota formatting', () => {
  test('formats a reset countdown without issuing any refresh action', () => {
    const now = Date.parse('2026-08-29T12:00:00Z')
    expect(formatAccountPoolCountdown('2026-08-29T13:02:03Z', now)).toBe(
      '1h 2m'
    )
    expect(formatAccountPoolCountdown('2026-08-29T12:04:05Z', now)).toBe(
      '4m 5s'
    )
    expect(formatAccountPoolCountdown('2026-08-29T11:59:59Z', now)).toBe('0s')
  })

  test('labels 28-to-31-day secondary windows as monthly', () => {
    expect(
      getAccountPoolSecondaryWindowLabel({
        used_percent: 10,
        remaining_percent: 90,
        reset_at: null,
        limit_window_seconds: 28 * 86400,
      })
    ).toBe('Monthly quota')
    expect(
      getAccountPoolSecondaryWindowLabel({
        used_percent: 10,
        remaining_percent: 90,
        reset_at: null,
        limit_window_seconds: 7 * 86400,
      })
    ).toBe('Weekly quota')
  })

  test('maps known upstream group labels to i18n keys and keeps unknown labels raw', () => {
    expect(getAccountPoolWindowGroupLabelKey('Gemini Models')).toBe(
      'Gemini models'
    )
    expect(getAccountPoolWindowGroupLabelKey('claude and gpt models')).toBe(
      'Claude and GPT models'
    )
    expect(getAccountPoolWindowGroupLabelKey('Future Models')).toBe(
      'Future Models'
    )
  })

  test('falls back to the flat windows as one unlabeled group when window_groups is empty', () => {
    const account = {
      primary_window: {
        used_percent: 10,
        remaining_percent: 90,
        reset_at: null,
        limit_window_seconds: 18000,
      },
      secondary_window: null,
      window_groups: [],
    } as Pick<
      AccountPoolAccount,
      'primary_window' | 'secondary_window' | 'window_groups'
    >

    expect(getAccountPoolWindowGroups(account)).toEqual([
      {
        label: null,
        primary_window: account.primary_window,
        secondary_window: null,
      },
    ])
  })

  test('keeps the server-provided window groups untouched', () => {
    const groups = [
      {
        label: 'Gemini Models',
        primary_window: null,
        secondary_window: null,
      },
    ]
    const account = {
      primary_window: null,
      secondary_window: null,
      window_groups: groups,
    } as Pick<
      AccountPoolAccount,
      'primary_window' | 'secondary_window' | 'window_groups'
    >

    expect(getAccountPoolWindowGroups(account)).toBe(groups)
  })
})
