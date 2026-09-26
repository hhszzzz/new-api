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
import type { TFunction } from 'i18next'

import type { PromptAuditDirection } from './types'

// What `POST /api/prompt-audit/nodes/:id/test` reports on failure. The backend
// fills every field once it reached the audit model; a transport failure
// carries only `error_code`.
export type PromptAuditTestFailure = {
  error_code?: string
  direction?: PromptAuditDirection | 'review'
  safety?: string
  /**
   * The address the probe was sent to, resolved from the node's base URL and
   * protocol. Absent when the base URL could not be resolved at all, which is
   * itself reported by the configuration failure.
   */
  request_url?: string
  /**
   * What the node said when it refused the request, as it worded it.
   */
  failure_detail?: string
}

const promptAuditHTTPStatus = /^endpoint_http_(\d{3})$/

export function auditDirectionLabel(
  t: TFunction,
  direction: string | undefined
): string {
  if (direction === 'output') {
    return t('Generated output')
  }
  if (direction === 'review') {
    return t('Gray-area review')
  }
  return t('Request input')
}

// The node test fails with one of fifteen-odd kinds, and the button used to
// collapse all but two of them into "test failed": a rejected token, a base
// URL that redirects, an unreadable verdict, and a model that ignores
// assistant messages were indistinguishable without opening the browser
// network tab. Each kind now names its cause with the value the backend
// measured, and a kind this build does not know still prints its raw code.
export function promptAuditTestFailureMessage(
  t: TFunction,
  failure: PromptAuditTestFailure
): string {
  const code = failure.error_code ?? ''
  // A review-purpose node reports the same failures under a `review_` prefix.
  const kind = code.startsWith('review_') ? code.slice('review_'.length) : code

  // A refusal is only actionable with the two things the status code withholds:
  // what the node said, and the address it was asked at. A 403 is an API key the
  // node never received when the body says so, and a base URL with a segment the
  // API does not serve reads exactly like a wrong host until the resolved path is
  // on screen. Neither sentence is invented here; both quote the backend.
  const annotated = (message: string): string => {
    let annotated = message
    if (failure.failure_detail) {
      annotated += ` ${t('The audit model replied: {{detail}}', {
        detail: failure.failure_detail,
      })}`
    }
    if (failure.request_url) {
      annotated += ` ${t('The audit model was called at {{url}}.', {
        url: failure.request_url,
      })}`
    }
    return annotated
  }

  const status = promptAuditHTTPStatus.exec(kind)?.[1]
  if (status) {
    return annotated(
      failure.direction
        ? t(
            'The audit model returned HTTP {{status}} during the {{direction}} check.',
            { status, direction: auditDirectionLabel(t, failure.direction) }
          )
        : t('The audit model returned HTTP {{status}}.', { status })
    )
  }
  if (kind === 'output_capability_unverified') {
    return t(
      'Output audit test failed: the model judged the assistant message as {{safety}}. It may be inspecting the request input instead of the assistant reply.',
      { safety: failure.safety ?? '' }
    )
  }
  if (kind === 'input_capability_unverified') {
    return t(
      'Input audit test failed: the model judged a harmless sample as {{safety}}.',
      { safety: failure.safety ?? '' }
    )
  }
  if (kind === 'invalid_response') {
    return t(
      'The audit model replied, but its answer had no readable Safety and Categories verdict.'
    )
  }
  if (kind === 'endpoint_timeout' || kind === 'total_timeout') {
    return t('The audit model did not answer in time.')
  }
  if (kind === 'network_error') {
    return annotated(
      t('The audit model could not be reached: the connection failed.')
    )
  }
  if (kind === 'redirect_not_allowed') {
    return annotated(
      t('The audit model base URL redirects, and redirects are not followed.')
    )
  }
  if (
    kind === 'configuration_invalid' ||
    kind === 'no_enabled_endpoint' ||
    kind === 'no_enabled_review_endpoint'
  ) {
    return annotated(
      t(
        'The audit model configuration is invalid: check the base URL and that at least one audit direction is enabled.'
      )
    )
  }
  return code
    ? t('Audit model test failed ({{code}}).', { code })
    : t('Audit model test failed')
}
