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
export const CLIENT_POLICY_MODE_LABEL_KEYS = {
  unrestricted: 'Unrestricted',
  allow: 'Allow only',
  deny: 'Deny',
} as const

export type ClientPolicyMode = keyof typeof CLIENT_POLICY_MODE_LABEL_KEYS

const CODEX_CLIENT_ALIASES = new Set([
  'codex_cli_rs',
  'codex-cli',
  'codex-tui',
  'codex_vscode',
  'codex_desktop',
  'codex-chatgpt-desktop',
  'codex_chatgpt_desktop',
  'codex_atlas',
])

export function normalizeClientId(value: string): string {
  const normalized = value.trim().toLowerCase()
  if (CODEX_CLIENT_ALIASES.has(normalized) || normalized.startsWith('codex ')) {
    return 'codex'
  }
  return normalized
}

export const CLIENT_MATCH_SOURCE_LABEL_KEYS = {
  path: 'Path',
  user_agent: 'User-Agent',
  header: 'Safe header',
} as const

export type ClientMatchSource = keyof typeof CLIENT_MATCH_SOURCE_LABEL_KEYS

export const CLIENT_MATCH_MODE_LABEL_KEYS = {
  exact: 'Exact',
  prefix: 'Prefix',
} as const

export type ClientMatchMode = keyof typeof CLIENT_MATCH_MODE_LABEL_KEYS
