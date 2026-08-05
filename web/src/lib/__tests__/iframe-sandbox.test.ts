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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import { getIframeSandbox } from '../iframe-sandbox'

const PARENT_ORIGIN = 'https://dashboard.example.com'

describe('iframe sandbox policy', () => {
  test('keeps same-origin and relative frames in an opaque origin', () => {
    assert.equal(
      getIframeSandbox('/chat', PARENT_ORIGIN),
      'allow-forms allow-popups allow-presentation allow-scripts'
    )
    assert.equal(
      getIframeSandbox(`${PARENT_ORIGIN}/chat`, PARENT_ORIGIN),
      'allow-forms allow-popups allow-presentation allow-scripts'
    )
  })

  test('preserves storage and cookie behavior for cross-origin web apps', () => {
    assert.equal(
      getIframeSandbox('https://chat.example.net/app', PARENT_ORIGIN),
      'allow-forms allow-popups allow-presentation allow-scripts allow-same-origin'
    )
  })

  test('does not grant same-origin capability to non-web or invalid URLs', () => {
    assert.equal(
      getIframeSandbox('data:text/html,hello', PARENT_ORIGIN),
      'allow-forms allow-popups allow-presentation allow-scripts'
    )
    assert.equal(
      getIframeSandbox('http://[invalid', PARENT_ORIGIN),
      'allow-forms allow-popups allow-presentation allow-scripts'
    )
  })
})
