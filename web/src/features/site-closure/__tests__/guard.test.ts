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
import { describe, expect, it } from 'vitest'

import { ROLE } from '@/lib/roles'

import {
  isSiteClosureAdmin,
  isSiteClosureBlocked,
  isSiteClosurePathAllowed,
  parseSiteClosureStatus,
} from '../lib'

describe('parseSiteClosureStatus', () => {
  it('returns safe defaults for missing or malformed data', () => {
    expect(parseSiteClosureStatus(undefined)).toEqual({
      web_closed: false,
      api_paused: false,
      title: '',
      message: '',
      expected_recovery_at: 0,
      bypass: false,
    })
    expect(parseSiteClosureStatus('closed')).toEqual({
      web_closed: false,
      api_paused: false,
      title: '',
      message: '',
      expected_recovery_at: 0,
      bypass: false,
    })
  })

  it('parses a valid payload', () => {
    expect(
      parseSiteClosureStatus({
        site_closure: {
          web_closed: true,
          api_paused: true,
          title: 'Maintenance',
          message: 'Back soon',
          expected_recovery_at: 1_760_000_000,
          bypass: true,
        },
      })
    ).toEqual({
      web_closed: true,
      api_paused: true,
      title: 'Maintenance',
      message: 'Back soon',
      expected_recovery_at: 1_760_000_000,
      bypass: true,
    })
  })

  it('drops non-positive or non-numeric recovery timestamps', () => {
    expect(
      parseSiteClosureStatus({
        site_closure: { expected_recovery_at: -1, web_closed: true },
      }).expected_recovery_at
    ).toBe(0)
    expect(
      parseSiteClosureStatus({
        site_closure: { expected_recovery_at: 'soon', web_closed: true },
      }).expected_recovery_at
    ).toBe(0)
  })
})

describe('isSiteClosurePathAllowed', () => {
  it.each([
    '/maintenance',
    '/maintenance/',
    '/setup',
    '/sign-in',
    '/otp',
    '/401',
    '/403',
    '/404',
    '/500',
    '/503',
    '/oauth',
    '/oauth/github',
  ])('allows %s', (path) => {
    expect(isSiteClosurePathAllowed(path)).toBe(true)
  })

  it.each(['/', '/dashboard', '/pricing', '/user/self', '/register'])(
    'blocks %s',
    (path) => {
      expect(isSiteClosurePathAllowed(path)).toBe(false)
    }
  )
})

describe('isSiteClosureBlocked', () => {
  const closed = parseSiteClosureStatus({
    site_closure: { web_closed: true },
  })

  it('never blocks when the website is open', () => {
    expect(
      isSiteClosureBlocked(
        parseSiteClosureStatus({ site_closure: { web_closed: false } }),
        ROLE.USER,
        '/dashboard'
      )
    ).toBe(false)
  })

  it('blocks non-admin users outside the allowlist', () => {
    expect(isSiteClosureBlocked(closed, ROLE.USER, '/dashboard')).toBe(true)
    expect(isSiteClosureBlocked(closed, undefined, '/')).toBe(true)
    expect(isSiteClosureBlocked(closed, ROLE.USER, '/maintenance')).toBe(false)
  })

  it('lets administrators and server-approved bypasses through', () => {
    expect(isSiteClosureBlocked(closed, ROLE.ADMIN, '/dashboard')).toBe(false)
    expect(
      isSiteClosureBlocked(closed, ROLE.SUPER_ADMIN, '/dashboard')
    ).toBe(false)
    expect(
      isSiteClosureBlocked(
        parseSiteClosureStatus({
          site_closure: { web_closed: true, bypass: true },
        }),
        ROLE.USER,
        '/dashboard'
      )
    ).toBe(false)
  })
})

describe('isSiteClosureAdmin', () => {
  it('uses the backend admin threshold', () => {
    expect(isSiteClosureAdmin(undefined)).toBe(false)
    expect(isSiteClosureAdmin(ROLE.USER)).toBe(false)
    expect(isSiteClosureAdmin(ROLE.ADMIN)).toBe(true)
    expect(isSiteClosureAdmin(ROLE.SUPER_ADMIN)).toBe(true)
  })
})
