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
import type { ConversionDiagnostic, LogOtherData } from '../types'

const protocolNames: Record<string, string> = {
  chat: 'OpenAI Chat Completions',
  openai: 'OpenAI Chat Completions',
  messages: 'Anthropic Messages',
  claude: 'Anthropic Messages',
  responses: 'OpenAI Responses',
  'openai-responses': 'OpenAI Responses',
  openai_responses_compaction: 'OpenAI Responses',
  gemini: 'Gemini GenerateContent',
  'OpenAI Compatible': 'OpenAI Chat Completions',
  'Claude Messages': 'Anthropic Messages',
  'OpenAI Responses': 'OpenAI Responses',
  'Google Gemini': 'Gemini GenerateContent',
}

// Lowercase protocol names: this map only feeds `getProtocolTranslationTarget`,
// which is interpolated as `{{protocol}} translation`, and the name reads as a
// common noun in that phrase (`messages translation`), not as a proper label.
const protocolShortNames: Record<string, string> = {
  chat: 'chat',
  openai: 'chat',
  messages: 'messages',
  claude: 'messages',
  responses: 'responses',
  'openai-responses': 'responses',
  openai_responses_compaction: 'responses',
  gemini: 'gemini',
  'OpenAI Compatible': 'chat',
  'Claude Messages': 'messages',
  'OpenAI Responses': 'responses',
  'Google Gemini': 'gemini',
}

export function getProtocolName(protocol: string): string {
  return protocolNames[protocol] ?? protocol
}

export function getConversionDiagnostics(
  other: LogOtherData | null
): ConversionDiagnostic[] {
  const diagnostics = other?.admin_info?.conversion_diagnostics
  if (!Array.isArray(diagnostics)) return []
  return diagnostics.filter(
    (item) =>
      item != null &&
      typeof item.code === 'string' &&
      typeof item.message === 'string' &&
      (item.severity === 'warning' || item.severity === 'error')
  )
}

export function hasProtocolFieldAdjustments(
  logType: number,
  other: LogOtherData | null
): boolean {
  // Planned losses and unrelated warnings do not prove a field was adjusted.
  return (
    logType === 2 &&
    getConversionDiagnostics(other).some(
      (item) =>
        item.severity === 'warning' && item.loss_class === 'presentation'
    )
  )
}

export function getProtocolTranslationTarget(
  logType: number,
  other: LogOtherData | null
): string | null {
  if (logType !== 2) return null
  const flow = getProtocolFlow(other)
  if (other?.admin_info?.compaction_mode === 'summary') {
    return protocolShortNames[flow.upstream] ?? null
  }
  if (!flow.upstream || flow.native) return null
  if (!flow.converter && (!flow.request || flow.request === flow.upstream)) {
    return null
  }
  return protocolShortNames[flow.upstream] ?? null
}

export function getProtocolFlow(other: LogOtherData | null) {
  const chain = Array.isArray(other?.request_conversion)
    ? other.request_conversion.filter(
        (value) => typeof value === 'string' && value
      )
    : []
  const protocols = chain.filter((value) => protocolNames[value])
  const request = other?.diagnostics?.request_protocol || protocols[0] || ''
  const upstream =
    other?.admin_info?.upstream_protocol ||
    other?.diagnostics?.upstream_protocol ||
    protocols.at(-1) ||
    ''
  const converter =
    other?.admin_info?.protocol_converter ||
    other?.diagnostics?.protocol_converter ||
    chain.find((value) => value.includes('_to_')) ||
    ''
  return {
    request,
    upstream,
    converter,
    path: other?.request_path || other?.diagnostics?.path || '',
    native: Boolean(
      request &&
      upstream &&
      getProtocolName(request) === getProtocolName(upstream) &&
      !converter &&
      other?.admin_info?.compaction_mode !== 'summary'
    ),
  }
}
