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

import { formatModelName, getModelRouteInfo } from '../lib/model-route'
import { resolveLogsViewPermissions } from '../lib/view-permissions'

describe('usage log model route visibility', () => {
  test('shows nested administrator routing metadata to administrators', () => {
    const route = getModelRouteInfo(
      {
        admin_info: {
          is_model_mapped: true,
          upstream_model_name: 'gpt-4.1',
        },
      },
      true
    )

    assert.deepEqual(route, {
      isMapped: true,
      actualModel: 'gpt-4.1',
    })
  })

  test('falls back to historical top-level routing metadata', () => {
    const route = getModelRouteInfo(
      {
        is_model_mapped: true,
        upstream_model_name: 'claude-sonnet-4',
      },
      true
    )

    assert.deepEqual(route, {
      isMapped: true,
      actualModel: 'claude-sonnet-4',
    })
  })

  test('prefers nested routing metadata when both formats are present', () => {
    const route = getModelRouteInfo(
      {
        admin_info: {
          is_model_mapped: true,
          upstream_model_name: 'nested-model',
        },
        is_model_mapped: true,
        upstream_model_name: 'legacy-model',
      },
      true
    )

    assert.equal(route.actualModel, 'nested-model')
  })

  test('hides routing metadata from regular users even if the payload contains it', () => {
    const route = getModelRouteInfo(
      {
        admin_info: {
          is_model_mapped: true,
          upstream_model_name: 'nested-model',
        },
        is_model_mapped: true,
        upstream_model_name: 'legacy-model',
      },
      false
    )

    assert.deepEqual(route, { isMapped: false })
  })

  test('omits the actual model from regular-user list formatting', () => {
    const model = formatModelName(
      'requested-model',
      {
        admin_info: {
          is_model_mapped: true,
          upstream_model_name: 'actual-model',
        },
      },
      false
    )

    assert.deepEqual(model, {
      name: 'requested-model',
      isMapped: false,
      actualModel: undefined,
    })
  })

  test('keeps model routing visible in an administrator self view', () => {
    const permissions = resolveLogsViewPermissions(true, 'self')

    assert.equal(permissions.isAdminView, false)
    assert.equal(permissions.canViewModelRoute, true)
  })

  test('does not grant model routing visibility to a regular user', () => {
    const permissions = resolveLogsViewPermissions(false, 'self')

    assert.equal(permissions.isAdminView, false)
    assert.equal(permissions.canViewModelRoute, false)
  })

  test('keeps an unexpected routing payload hidden in a regular-user all scope', () => {
    const permissions = resolveLogsViewPermissions(false, 'all')
    const model = formatModelName(
      'requested-model',
      {
        admin_info: {
          is_model_mapped: true,
          upstream_model_name: 'actual-model',
        },
      },
      permissions.canViewModelRoute
    )

    assert.equal(permissions.isAdminView, false)
    assert.deepEqual(model, {
      name: 'requested-model',
      isMapped: false,
      actualModel: undefined,
    })
  })
})
