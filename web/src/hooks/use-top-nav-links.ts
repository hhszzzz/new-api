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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import type { TopNavLink } from '@/components/layout/types'
import { useStatus } from '@/hooks/use-status'
import {
  getCustomHeaderNavOrderKey,
  getCustomHeaderNavPath,
  parseHeaderNavModulesFromStatus,
} from '@/lib/nav-modules'
import { TOP_NAV_ICONS } from '@/lib/top-nav-icons'
import { useAuthStore } from '@/stores/auth-store'

/**
 * Generate top navigation links based on HeaderNavModules configuration from backend /api/status
 * Backend format example (stringified JSON):
 * {
 *   home: true,
 *   console: true,
 *   pricing: { enabled: true, requireAuth: false },
 *   modelStatus: { enabled: true, requireAuth: false },
 *   modelRadar: { enabled: true, requireAuth: false },
 *   rankings: { enabled: true, requireAuth: false },
 *   docs: true,
 *   about: true
 * }
 */
export function useTopNavLinks(): TopNavLink[] {
  const { t } = useTranslation()
  const { status } = useStatus()
  const { auth } = useAuthStore()

  // Parse HeaderNavModules
  const modules = useMemo(() => {
    return parseHeaderNavModulesFromStatus(
      status as Record<string, unknown> | null
    )
  }, [status])

  // Documentation link (may be external)
  const docsLink: string | undefined = status?.docs_link as string | undefined

  const isAuthed = !!auth?.user

  const linksByKey = new Map<string, TopNavLink>()

  // Console -> /dashboard (new console path)
  if (modules?.console !== false) {
    linksByKey.set('console', {
      title: t('Console'),
      href: '/dashboard',
      icon: TOP_NAV_ICONS.console,
    })
  }

  // Pricing
  const pricing = modules?.pricing
  if (pricing && typeof pricing === 'object' && pricing.enabled) {
    const requiresAuth = pricing.requireAuth && !isAuthed
    linksByKey.set('pricing', {
      title: t('Model Square'),
      href: '/pricing',
      requiresAuth,
      icon: TOP_NAV_ICONS.pricing,
    })
  }

  // Model status
  const modelStatus = modules?.modelStatus
  if (modelStatus && typeof modelStatus === 'object' && modelStatus.enabled) {
    const requiresAuth = modelStatus.requireAuth && !isAuthed
    linksByKey.set('modelStatus', {
      title: t('Model Status'),
      href: '/model-status',
      requiresAuth,
      icon: TOP_NAV_ICONS.modelStatus,
    })
  }

  // Model radar
  const modelRadar = modules?.modelRadar
  if (modelRadar && typeof modelRadar === 'object' && modelRadar.enabled) {
    const requiresAuth = modelRadar.requireAuth && !isAuthed
    linksByKey.set('modelRadar', {
      title: t('Model Radar'),
      href: '/model-radar',
      requiresAuth,
      icon: TOP_NAV_ICONS.modelRadar,
    })
  }

  // Rankings
  const rankings = modules?.rankings
  if (rankings && typeof rankings === 'object' && rankings.enabled) {
    const requiresAuth = rankings.requireAuth && !isAuthed
    linksByKey.set('rankings', {
      title: t('Rankings'),
      href: '/rankings',
      requiresAuth,
      icon: TOP_NAV_ICONS.rankings,
    })
  }

  // Docs (supports external links)
  if (modules?.docs !== false) {
    if (docsLink) {
      linksByKey.set('docs', {
        title: t('Docs'),
        href: docsLink,
        external: true,
        icon: TOP_NAV_ICONS.docs,
      })
    } else {
      linksByKey.set('docs', {
        title: t('Docs'),
        href: '/docs',
        icon: TOP_NAV_ICONS.docs,
      })
    }
  }

  // About
  if (modules?.about !== false) {
    linksByKey.set('about', {
      title: t('About'),
      href: '/about',
      icon: TOP_NAV_ICONS.about,
    })
  }

  for (const item of modules.custom) {
    if (!item.enabled) continue
    linksByKey.set(getCustomHeaderNavOrderKey(item.id), {
      title: item.title,
      href: getCustomHeaderNavPath(item.id),
      icon: item.icon || TOP_NAV_ICONS.custom,
    })
  }

  return modules.order.flatMap((key) => {
    const link = linksByKey.get(key)
    return link ? [link] : []
  })
}
