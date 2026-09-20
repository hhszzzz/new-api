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
import { useLocation, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

export function PromptAuditNavigation() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const user = useAuthStore((state) => state.auth.user)
  const canManage = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
    ADMIN_PERMISSION_ACTIONS.MANAGE
  )
  const canRead = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.PROMPT_AUDIT,
    ADMIN_PERMISSION_ACTIONS.READ
  )
  let active = 'records'
  if (location.pathname === '/prompt-audit/wordlists') {
    active = 'wordlists'
  } else if (location.pathname === '/prompt-audit/settings') {
    active = location.hash.includes('audit-nodes') ? 'nodes' : 'rules'
  }

  return (
    <nav aria-label={t('Prompt audit')}>
      <Tabs
        value={active}
        onValueChange={(value) => {
          if (value === 'records') {
            void navigate({ to: '/prompt-audit' })
          } else if (value === 'wordlists') {
            void navigate({ to: '/prompt-audit/wordlists' })
          } else if (value === 'nodes') {
            void navigate({
              to: '/prompt-audit/settings',
              hash: 'audit-nodes',
            })
          } else {
            void navigate({ to: '/prompt-audit/settings' })
          }
        }}
      >
        <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
          {canRead && (
            <TabsTrigger value='records'>{t('Audit records')}</TabsTrigger>
          )}
          {canManage && (
            <>
              <TabsTrigger value='rules'>{t('Inspection rules')}</TabsTrigger>
              <TabsTrigger value='wordlists'>{t('Wordlists')}</TabsTrigger>
              <TabsTrigger value='nodes'>{t('Audit nodes')}</TabsTrigger>
            </>
          )}
        </TabsList>
      </Tabs>
    </nav>
  )
}
