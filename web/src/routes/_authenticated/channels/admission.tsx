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
import { createFileRoute, getRouteApi, redirect } from '@tanstack/react-router'
import z from 'zod'

import { ChannelAdmissionMonitor } from '@/features/channels/components/channel-admission-monitor'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const route = getRouteApi('/_authenticated/channels/admission')
const admissionSearchSchema = z.object({
  channel_id: z.number().optional().catch(undefined),
})

function AdmissionMonitorRoute() {
  const { channel_id } = route.useSearch()
  return (
    <ChannelAdmissionMonitor key={channel_id} initialChannelId={channel_id} />
  )
}

export const Route = createFileRoute('/_authenticated/channels/admission')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: admissionSearchSchema,
  component: AdmissionMonitorRoute,
})
