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
      // A base URL carrying a segment the API does not serve reads like a wrong
      // host until the resolved address is shown.
      'an HTTP failure names the address the audit model was asked at',
      {
        error_code: 'endpoint_http_404',
        direction: 'input',
        request_url: 'https://api.typesafe.ai/typesafe/v1/systemone',
      },
      'The audit model returned HTTP 404 during the Request input check. The audit model was called at https://api.typesafe.ai/typesafe/v1/systemone.',
    ],
    [
      // "HTTP 403" on its own sends the operator hunting; TypeSafe's own body
      // names the missing key outright. The reply is quoted verbatim and stands
      // ahead of the address, because the cause outranks the location.
      'an HTTP failure quotes what the node said when it refused',
      {
        error_code: 'endpoint_http_403',
        direction: 'input',
        request_url: 'https://api.typesafe.ai/v1/systemone',
        failure_detail:
          '{"detail":{"error_type":"authentication_error", "message":"Must supply an API key! Check your request and try again."}}',
      },
      'The audit model returned HTTP 403 during the Request input check. The audit model replied: {"detail":{"error_type":"authentication_error", "message":"Must supply an API key! Check your request and try again."}} The audit model was called at https://api.typesafe.ai/v1/systemone.',
    ],
    [
      'an unreachable node names the address it tried',
      {
        error_code: 'network_error',
        request_url: 'https://guard.example.com/v1/chat/completions',
      },
      'The audit model could not be reached: the connection failed. The audit model was called at https://guard.example.com/v1/chat/completions.',
    ],
    [
      'a failure with no resolved address stays as it was',
      { error_code: 'network_error' },
      'The audit model could not be reached: the connection failed.',
    ],
    [
      'an unreadable verdict says so without inventing a missing field',
      { error_code: 'invalid_response' },
      'The audit model answered, but nothing in its answer could be read as a verdict.',
    ],
    [
      // The answer is quoted because the three ways to be unreadable — a
      // probability under the wrong key, an unanswered question, a value outside
      // 0..1 — are indistinguishable without seeing what arrived.
      'an unreadable verdict quotes the answer that could not be read',
      {
        error_code: 'invalid_response',
        failure_detail:
          '{"model":"jev-1.13.0","answers":{"pii":{"noul":{"probability":0.9}}}}',
      },
      'The audit model answered, but nothing in its answer could be read as a verdict. The audit model replied: {"model":"jev-1.13.0","answers":{"pii":{"noul":{"probability":0.9}}}}',
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
