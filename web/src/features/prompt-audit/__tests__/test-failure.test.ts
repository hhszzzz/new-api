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
import i18next from 'i18next'
import { describe, expect, test } from 'vitest'

import {
  auditDirectionLabel,
  promptAuditTestFailureMessage,
  type PromptAuditTestFailure,
} from '../test-failure'

// The test setup initialises i18next with empty English resources, so `t`
// returns the English source string with its placeholders interpolated: every
// expectation below is the sentence an operator actually reads.
const message = (failure: PromptAuditTestFailure) =>
  promptAuditTestFailureMessage(i18next.getFixedT(null, null), failure)

describe('promptAuditTestFailureMessage', () => {
  test.each<[string, PromptAuditTestFailure, string]>([
    [
      'an HTTP failure names the status and the direction being checked',
      { error_code: 'endpoint_http_401', direction: 'output' },
      'The audit model returned HTTP 401 during the Generated output check.',
    ],
    [
      'a review node HTTP failure keeps the status without inventing a direction',
      { error_code: 'review_endpoint_http_429' },
      'The audit model returned HTTP 429.',
    ],
    [
      'an unreadable verdict points at the missing Safety and Categories lines',
      { error_code: 'invalid_response' },
      'The audit model replied, but its answer had no readable Safety and Categories verdict.',
    ],
    [
      'an attempt timeout reports that the model never answered',
      { error_code: 'endpoint_timeout' },
      'The audit model did not answer in time.',
    ],
    [
      'a total timeout reports that the model never answered',
      { error_code: 'total_timeout' },
      'The audit model did not answer in time.',
    ],
    [
      'a connection failure reports that the model was unreachable',
      { error_code: 'network_error' },
      'The audit model could not be reached: the connection failed.',
    ],
    [
      'a redirecting base URL reports that redirects are not followed',
      { error_code: 'redirect_not_allowed' },
      'The audit model base URL redirects, and redirects are not followed.',
    ],
    [
      'a configuration failure points at the URL and the enabled directions',
      { error_code: 'configuration_invalid' },
      'The audit model configuration is invalid: check the base URL and that at least one audit direction is enabled.',
    ],
    [
      'a failure kind this build does not know still prints its raw code',
      { error_code: 'request_create_failed' },
      'Audit model test failed (request_create_failed).',
    ],
    [
      'a failure carrying no code falls back to the generic message',
      {},
      'Audit model test failed',
    ],
  ])('%s', (_name, failure, expected) => {
    expect(message(failure)).toBe(expected)
  })

  test('an output capability failure quotes the verdict the model returned', () => {
    expect(
      message({
        error_code: 'output_capability_unverified',
        direction: 'output',
        safety: 'Unsafe',
      })
    ).toBe(
      'Output audit test failed: the model judged the assistant message as Unsafe. It may be inspecting the request input instead of the assistant reply.'
    )
  })

  test('an input capability failure quotes the verdict the model returned', () => {
    expect(
      message({ error_code: 'input_capability_unverified', safety: 'Unsafe' })
    ).toBe(
      'Input audit test failed: the model judged a harmless sample as Unsafe.'
    )
  })
})

describe('auditDirectionLabel', () => {
  test.each([
    ['input', 'Request input'],
    ['output', 'Generated output'],
    ['review', 'Gray-area review'],
    [undefined, 'Request input'],
  ])('labels %s as the direction the toast prints', (direction, expected) => {
    expect(auditDirectionLabel(i18next.getFixedT(null, null), direction)).toBe(
      expected
    )
  })
})
