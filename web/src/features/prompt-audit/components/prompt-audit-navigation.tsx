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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

export function PromptAuditNavigation() {
  const { t } = useTranslation()
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
  return (
    <nav className='flex flex-wrap gap-2' aria-label={t('Prompt audit')}>
      {canRead && (
        <Button variant='outline' render={<Link to='/prompt-audit' />}>
          {t('Audit records')}
        </Button>
      )}
      {canManage && (
        <>
          <Button
            variant='outline'
            render={<Link to='/prompt-audit/settings' />}
          >
            {t('Inspection rules')}
          </Button>
          <Button
            variant='outline'
            render={<Link to='/prompt-audit/wordlists' />}
          >
            {t('Wordlists')}
          </Button>
          <Button
            variant='outline'
            render={<Link to='/prompt-audit/settings' hash='audit-nodes' />}
          >
            {t('Audit nodes')}
          </Button>
        </>
      )}
    </nav>
  )
}
