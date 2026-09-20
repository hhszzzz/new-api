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

import type { PromptAuditScope, PromptScopePolicies } from './types'

export const PROMPT_AUDIT_SCOPES: PromptAuditScope[] = [
  'system',
  'developer',
  'user',
  'assistant',
  'tool_call',
  'tool_result',
  'task',
]

export function promptAuditScopeLabel(
  scope: PromptAuditScope,
  t: TFunction
): string {
  switch (scope) {
    case 'system':
      return t('System instructions')
    case 'developer':
      return t('Developer instructions')
    case 'user':
      return t('User messages')
    case 'assistant':
      return t('Historical assistant messages')
    case 'tool_call':
      return t('Tool call arguments')
    case 'tool_result':
      return t('Tool results')
    case 'task':
      return t('Task and standalone input')
  }
}

export function defaultPromptScopePolicies(): PromptScopePolicies {
  return Object.fromEntries(
    PROMPT_AUDIT_SCOPES.map((scope) => [
      scope,
      {
        library_ids: scope === 'user' || scope === 'task' ? ['manual'] : [],
        model_audit: true,
      },
    ])
  ) as PromptScopePolicies
}

export function promptWordlistError(code: string, t: TFunction): string {
  if (code.startsWith('download_http_')) {
    return t('Source returned HTTP {{status}}', {
      status: code.slice('download_http_'.length),
    })
  }
  switch (code) {
    case 'source_rate_limited':
      return t('Source rate limit reached. Try again later.')
    case 'empty_wordlist':
    case 'no_wordlist_files':
      return t('No supported wordlist entries were found.')
    case 'invalid_encoding':
      return t('The wordlist must use UTF-8 encoding.')
    case 'unsupported_format':
    case 'invalid_word':
      return t('Unsupported wordlist format or invalid entry.')
    case 'file_too_large':
      return t('Each wordlist file must not exceed 10 MiB.')
    case 'source_too_large':
      return t('All data downloaded from one source must not exceed 30 MiB.')
    case 'too_many_words':
      return t('A wordlist can contain at most 500,000 unique entries.')
    case 'too_many_files':
      return t(
        'A GitHub source can contain at most 100 supported wordlist files.'
      )
    case 'github_tree_too_large':
      return t(
        'The GitHub directory is too large to scan. Select a smaller directory or a single file.'
      )
    default:
      return t('The source could not be downloaded or processed.')
  }
}

export function promptWordlistStatus(status: string, t: TFunction): string {
  switch (status) {
    case 'ready':
      return t('Ready')
    case 'failed':
      return t('Failed')
    case 'updating':
      return t('Updating...')
    default:
      return t('Queued')
  }
}
