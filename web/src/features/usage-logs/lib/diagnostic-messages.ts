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
/**
 * Conversion-diagnostic messages the backend writes as a fixed English string,
 * recorded verbatim in the log and rendered as-is by the details dialog. Each
 * one is also an i18n key, so listing it here is what lets the dialog translate
 * it.
 *
 * A message carrying an interpolated value (a model name, a token budget, a
 * tool name) is deliberately absent: the text differs per request, so it cannot
 * be recognised by exact match. Those keep the recorded English, which is what
 * the dialog showed before this list existed.
 */
import type { ConversionDiagnostic } from '../types'

export const STATIC_DIAGNOSTIC_MESSAGES: readonly string[] = [
  'Chat Completions cannot preserve Claude max_uses',
  'Chat Completions cannot preserve caller or external-access constraints',
  'Chat Completions cannot preserve response-inclusion or return-token tuning',
  'Chat Completions cannot represent the source hosted tool choice',
  'Chat Completions expresses hosted search through web_search_options instead of tool_choice',
  'Chat Completions web_search_options cannot preserve domain access constraints',
  'Claude MCP history cannot preserve a Responses approval_request_id',
  'Claude MCP history has no input object',
  'Claude MCP history requires both name and server_name for Responses mapping',
  'Claude MCP output requires both name and server_name for Responses mapping',
  'Claude MCP response blocks cannot preserve a Responses approval_request_id',
  'Claude MCP tool use must include both name and server_name for Responses MCP mapping',
  'Claude cannot preserve OpenAI external-web access constraints',
  'Claude cannot preserve OpenAI return-token tuning',
  'Claude cannot represent the source complex tool-choice policy',
  'Claude encrypted thinking state cannot be represented by the target response',
  'Claude has no equivalent for OpenAI search_context_size; max_uses is deliberately not inferred',
  'Claude has no verified hosted tool-choice mapping',
  'Claude hosted-tool call has no id for pairing it with its result',
  'Claude hosted-tool result has no tool_use_id for pairing it with its call',
  'Claude pause_turn requires protocol-native continuation state that the target response cannot preserve',
  'Claude tool_choice could not be restored from its native payload',
  'Claude web-search result content has no field on a Responses web_search_call',
  'Claude web-search result content is provider-private and has no field on an OpenAI Responses web_search_call; completion status and citations remain available',
  "Claude's single MCP text block is normalized to a Responses output string",
  "Claude's single MCP text block was normalized to a Responses output string",
  'Gemini Google Search cannot preserve Claude max_uses',
  'Gemini Google Search cannot preserve source web-search access constraints',
  'Gemini Google Search cannot preserve source web-search location or result tuning',
  'Gemini cannot represent the source complex tool-choice policy',
  'Gemini cannot represent this OpenAI free-form or unknown tool; its preprocessed call history and definition were omitted',
  'Gemini does not expose OpenAI function strictness',
  'Gemini generateContent does not expose a request field equivalent to parallel_tool_calls',
  'Gemini generateContent does not expose an equivalent hosted-tool choice',
  'Gemini grounding byte offsets do not identify valid UTF-8 boundaries in the referenced part',
  'Gemini grounding chunks are missing or cannot be decoded',
  'Gemini grounding citations are preserved across stream chunks, but provider-specific search metadata has no target-protocol equivalent',
  'Gemini grounding citations are preserved, but provider-specific search metadata has no target-protocol equivalent',
  'Gemini grounding confirms a hosted web search, but the target converter cannot produce an OpenAI Responses web_search_call item',
  'Gemini grounding confirms a hosted web search, but the target stream converter cannot produce an OpenAI Responses web_search_call lifecycle',
  'Gemini grounding omitted partIndex while multiple text parts are present, so citation placement is ambiguous',
  'Gemini grounding segment text does not match the referenced part range',
  'Gemini grounding support does not reference a valid source URI',
  'OpenAI Responses cannot preserve Claude caller constraints',
  'OpenAI Responses cannot preserve Claude max_uses',
  'OpenAI Responses cannot preserve Claude response-inclusion tuning',
  'OpenAI Responses cannot reconstruct the source complex tool choice',
  'OpenAI Responses has no verified hosted tool-choice mapping',
  'OpenAI Responses web search supports allow filters but not Claude blocked_domains',
  "OpenAI Responses web_search_call and mcp_call items cannot preserve Claude's hosted-tool caller provenance",
  'Responses MCP history contains both output and error',
  'Responses MCP history has no id for pairing the call with its result',
  'Responses MCP history requires both name and server_label for Claude mapping',
  'Responses MCP output contains both output and error',
  'Responses MCP output must include both name and server_label for Claude MCP mapping',
  "Responses web-search execution cannot reconstruct Claude's required encrypted web_search_tool_result continuation state",
  "Responses web-search history cannot reconstruct Claude's encrypted web_search_tool_result continuation state",
  "Responses web-search source metadata cannot reconstruct Claude's encrypted web_search_tool_result",
  'URL context has no verified mapping from the source protocol',
  'code execution semantics differ across providers',
  'failed hosted-tool output has no error or output that Claude can preserve',
  'hosted and ordinary blocks share one source message, but Responses represents hosted calls as separate input items',
  'hosted and ordinary content cannot be merged after the intermediate converter coalesced source blocks',
  'hosted-tool blocks were interleaved with content that the target converter coalesced, so their original order cannot be reconstructed',
  'hosted-tool continuation state is preserved, but provider-specific item fields may differ',
  'hosted-tool execution is preserved, but provider-specific result fields may differ',
  'hosted-tool items were interleaved with content that the target converter coalesced, so their original order cannot be reconstructed',
  'hosted-tool output has no id for pairing the call with its result',
  'target protocol does not carry this display metadata',
]

const STATIC_DIAGNOSTIC_MESSAGE_SET = new Set<string>(
  STATIC_DIAGNOSTIC_MESSAGES
)

/**
 * The i18n key for a recorded diagnostic message, or null when the message
 * carries request-specific values and has no fixed translation. Callers render
 * the recorded message untouched in the null case so a backend wording change
 * degrades to the previous behaviour instead of printing a raw key.
 */
export function staticDiagnosticKey(message: string): string | null {
  return STATIC_DIAGNOSTIC_MESSAGE_SET.has(message) ? message : null
}

/**
 * The translatable text for one recorded diagnostic, as an i18n key. A message
 * with no translation returns its recorded English, which i18next renders
 * unchanged.
 */
export function conversionDiagnosticText(
  diagnostic: ConversionDiagnostic,
  logType: number
): string {
  // The presentation-metadata loss explains why the field is gone instead of
  // repeating the terse backend wording.
  if (logType === 2 && diagnostic.code === 'omitted_presentation_metadata') {
    return 'The target protocol does not support this display metadata; it was omitted.'
  }
  return staticDiagnosticKey(diagnostic.message) ?? diagnostic.message
}
