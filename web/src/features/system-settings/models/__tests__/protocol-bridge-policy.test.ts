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

import {
  DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM,
  parseProtocolBridgePolicy,
  protocolBridgePolicyFormSchema,
  serializeProtocolBridgePolicy,
} from '../protocol-bridge-policy'

describe('protocol bridge policy settings contract', () => {
  test('maps persisted bytes to a user-facing MiB value and back exactly', () => {
    const formValue = parseProtocolBridgePolicy(
      JSON.stringify({
        enabled: true,
        default_allow_conversion: false,
        state_ttl_seconds: 7200,
        max_state_turns: 256,
        max_state_bytes: 8 * 1024 * 1024,
      })
    )

    expect(formValue).toEqual({
      enabled: true,
      default_allow_conversion: false,
      state_ttl_seconds: 7200,
      max_state_turns: 256,
      max_state_mebibytes: 8,
    })
    expect(JSON.parse(serializeProtocolBridgePolicy(formValue))).toEqual({
      enabled: true,
      default_allow_conversion: false,
      state_ttl_seconds: 7200,
      max_state_turns: 256,
      max_state_bytes: 8 * 1024 * 1024,
    })
  })

  test('uses conservative defaults for absent or invalid persisted data', () => {
    expect(parseProtocolBridgePolicy(undefined)).toEqual(
      DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM
    )
    expect(parseProtocolBridgePolicy('{invalid')).toEqual(
      DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM
    )
    expect(
      parseProtocolBridgePolicy(
        JSON.stringify({ state_ttl_seconds: 1, max_state_bytes: 1 })
      )
    ).toEqual(DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM)
  })

  test('rejects limits outside the backend accepted ranges', () => {
    expect(
      protocolBridgePolicyFormSchema.safeParse({
        ...DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM,
        state_ttl_seconds: 59,
      }).success
    ).toBe(false)
    expect(
      protocolBridgePolicyFormSchema.safeParse({
        ...DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM,
        max_state_turns: 513,
      }).success
    ).toBe(false)
    expect(
      protocolBridgePolicyFormSchema.safeParse({
        ...DEFAULT_PROTOCOL_BRIDGE_POLICY_FORM,
        max_state_mebibytes: 16.1,
      }).success
    ).toBe(false)
  })
})
