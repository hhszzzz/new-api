import type { ProtocolCatalogResponse } from '../types'

export const protocolCatalogFixture: ProtocolCatalogResponse = {
  defaults: {
    version: 1,
    conversion: 'safe',
    selection: 'declared',
    request_mode: 'structured',
    state_scope: 'bridge',
  },
  global_policy: {
    version: 1,
    conversion: 'lossless',
    selection: 'declared',
    request_mode: 'structured',
    state_scope: 'bridge',
  },
  catalog: {
    version: 1,
    protocols: [
      { id: 'chat', format: 'openai', name: 'OpenAI Chat Completions' },
      { id: 'messages', format: 'claude', name: 'Anthropic Messages' },
      { id: 'responses', format: 'openai-responses', name: 'OpenAI Responses' },
    ],
    operations: [
      {
        id: 'generate',
        protocol: 'chat',
        path: '/v1/chat/completions',
        transports: ['http', 'sse'],
        convertible: true,
      },
      {
        id: 'generate',
        protocol: 'messages',
        path: '/v1/messages',
        transports: ['http', 'sse'],
        convertible: true,
      },
      {
        id: 'generate',
        protocol: 'responses',
        path: '/v1/responses',
        transports: ['http', 'sse', 'websocket'],
        convertible: true,
      },
      {
        id: 'compact',
        protocol: 'responses',
        path: '/v1/responses/compact',
        transports: ['http'],
        convertible: true,
      },
      {
        id: 'models',
        path: '/v1/models',
        transports: ['http'],
        convertible: false,
      },
    ],
    conversions: [
      {
        id: 'messages_to_responses',
        aliases: ['claude_messages_to_openai_responses'],
        from: 'messages',
        to: 'responses',
        request_steps: ['messages_to_responses'],
        response_steps: ['responses_to_messages'],
      },
      {
        id: 'responses_to_messages',
        aliases: ['openai_responses_to_claude_messages'],
        from: 'responses',
        to: 'messages',
        request_steps: ['responses_to_messages'],
        response_steps: ['messages_to_responses'],
      },
      {
        id: 'chat_to_messages',
        from: 'chat',
        to: 'messages',
        request_steps: ['chat_to_messages'],
        response_steps: ['messages_to_chat'],
      },
    ],
  },
}
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
