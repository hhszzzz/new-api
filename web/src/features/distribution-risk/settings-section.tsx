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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import { getDistributionRiskConfig, updateDistributionRiskConfig } from './api'
import { DistributionRiskSettings } from './components/distribution-risk-settings'

export function DistributionRiskSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const configQuery = useQuery({
    queryKey: ['distribution-risk', 'config'],
    queryFn: getDistributionRiskConfig,
    refetchInterval: 60_000,
  })
  const updateMutation = useMutation({
    mutationFn: updateDistributionRiskConfig,
    onSuccess: async (config) => {
      queryClient.setQueryData(['distribution-risk', 'config'], config)
      await queryClient.invalidateQueries({
        queryKey: ['distribution-risk', 'events'],
      })
      toast.success(t('Distribution risk settings saved'))
    },
    onError: (error) =>
      toast.error(error.message || t('Failed to save settings')),
  })

  return (
    <SettingsSection title={t('Distribution risk detection')}>
      {configQuery.isPending && (
        <p className='text-muted-foreground py-4 text-sm'>{t('Loading...')}</p>
      )}
      {configQuery.isError && (
        <Alert variant='destructive'>
          <AlertDescription className='flex items-center justify-between gap-3'>
            <span>
              {configQuery.error.message ||
                t('Failed to load detection settings')}
            </span>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() => void configQuery.refetch()}
            >
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {configQuery.data && (
        <DistributionRiskSettings
          key={[
            configQuery.data.enabled,
            configQuery.data.started_at,
            configQuery.data.ip_floor,
            configQuery.data.user_agent_floor,
            configQuery.data.session_floor,
            configQuery.data.requests_per_minute_floor,
            configQuery.data.concurrency_floor,
            configQuery.data.min_requests,
            configQuery.data.min_active_days,
          ].join(':')}
          config={configQuery.data}
          isSaving={updateMutation.isPending}
          onSave={(update) => updateMutation.mutate(update)}
        />
      )}
    </SettingsSection>
  )
}
