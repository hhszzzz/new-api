export type ProtocolPolicy = {
  version: number
  conversion?: 'native_only' | 'lossless' | 'safe' | 'lossy'
  selection?: 'declared' | 'auto'
  upstream_protocols?: string[]
  request_mode?: 'structured' | 'passthrough'
  state_scope?: 'disabled' | 'bridge' | 'all'
  state_ttl_seconds?: number
  max_state_turns?: number
  max_state_bytes?: number
  rules?: Array<{
    model_pattern?: string
    request_protocol?: string
    target_protocol?: string
    upstream_protocols?: string[]
    conversion?: 'native_only' | 'lossless' | 'safe' | 'lossy'
    channel_ids?: number[]
    channel_types?: number[]
    require_structured?: boolean
    deny?: boolean
  }>
}

export type ProtocolCatalog = {
  version: number
  protocols: Array<{ id: string; format: string; name: string }>
  operations: Array<{
    id: string
    protocol?: string
    path: string
    transports: string[]
    convertible: boolean
  }>
  conversions: Array<{
    id: string
    aliases?: string[]
    from: string
    to: string
    request_steps: string[]
    response_steps: string[]
  }>
}

export type ProtocolCatalogResponse = {
  catalog: ProtocolCatalog
  defaults: ProtocolPolicy
  global_policy: ProtocolPolicy
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
