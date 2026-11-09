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
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect } from 'react'

import { statusQueryOptions } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'

import { isSiteClosureAdmin, parseSiteClosureStatus } from './lib'
import { SiteClosureView } from './site-closure-view'

export function SiteClosurePage() {
  const navigate = useNavigate()
  const statusQuery = useQuery({
    ...statusQueryOptions,
    // Keep the maintenance page self-healing: recover automatically once the
    // site is reopened, without requiring the visitor to reload.
    refetchInterval: 30_000,
    meta: { errorToast: false },
  })
  const closure = parseSiteClosureStatus(statusQuery.data)
  const userRole = useAuthStore((state) => state.auth.user?.role)

  useEffect(() => {
    if (!statusQuery.isFetched || statusQuery.isError) return
    if (!closure.web_closed || closure.bypass || isSiteClosureAdmin(userRole)) {
      void navigate({ to: '/', replace: true })
    }
  }, [
    closure.bypass,
    closure.web_closed,
    navigate,
    statusQuery.isError,
    statusQuery.isFetched,
    userRole,
  ])

  const status = statusQuery.data as Record<string, unknown> | null | undefined
  const systemName =
    typeof status?.system_name === 'string' ? status.system_name : ''
  const logo = typeof status?.logo === 'string' ? status.logo : ''

  return (
    <SiteClosureView
      systemName={systemName}
      logo={logo}
      title={closure.title}
      message={closure.message}
      expectedRecoveryAt={closure.expected_recovery_at}
      onRetry={() => void statusQuery.refetch()}
      retrying={statusQuery.isFetching}
    />
  )
}
