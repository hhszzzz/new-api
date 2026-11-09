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
import { createFileRoute, redirect } from '@tanstack/react-router'

import {
  isSiteClosureAdmin,
  parseSiteClosureStatus,
} from '@/features/site-closure/lib'
import { SiteClosurePage } from '@/features/site-closure/site-closure-page'
import { ensureStatus } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/maintenance')({
  beforeLoad: async ({ context }) => {
    const status = await ensureStatus(context.queryClient).catch(() => null)
    if (!status) return
    const closure = parseSiteClosureStatus(status)
    const { auth } = useAuthStore.getState()
    if (
      !closure.web_closed ||
      closure.bypass ||
      isSiteClosureAdmin(auth.user?.role)
    ) {
      throw redirect({ to: '/', replace: true })
    }
  },
  component: SiteClosurePage,
})
