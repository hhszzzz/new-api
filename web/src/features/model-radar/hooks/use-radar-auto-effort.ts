/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { getUserProfile } from '@/features/profile/api'
import { parseUserSettings } from '@/features/profile/lib/format'
import type {
  RadarAutoEffortPolicy,
  RadarAutoEffortSetting,
} from '@/features/profile/types'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import { updateUserRadarAutoEffort } from '../api'

export const RADAR_AUTO_EFFORT_QUERY_KEY = ['radar-auto-effort']

export const DEFAULT_RADAR_AUTO_EFFORT: RadarAutoEffortSetting = {
  policy: 'highest_iq',
  min_iq_delta: 5,
  models: {},
}

function normalizeRadarAutoEffort(
  setting: RadarAutoEffortSetting | undefined
): RadarAutoEffortSetting {
  const policy = setting?.policy
  return {
    policy:
      policy === 'iq_per_cost' || policy === 'min_iq_delta'
        ? policy
        : DEFAULT_RADAR_AUTO_EFFORT.policy,
    min_iq_delta:
      typeof setting?.min_iq_delta === 'number' && setting.min_iq_delta > 0
        ? setting.min_iq_delta
        : DEFAULT_RADAR_AUTO_EFFORT.min_iq_delta,
    models: setting?.models ?? {},
  }
}

/**
 * Reads and updates the signed-in user's radar auto-effort preference. Writes
 * are optimistic: the local preference is applied first and rolled back when
 * the server rejects the change.
 */
export function useRadarAutoEffort(enabled: boolean) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: RADAR_AUTO_EFFORT_QUERY_KEY,
    enabled,
    staleTime: 5 * 60 * 1000,
    queryFn: async () => {
      const response = await getUserProfile()
      if (!response.success || !response.data) {
        throw createServerError(response, t('Failed to load settings'))
      }
      return normalizeRadarAutoEffort(
        parseUserSettings(response.data.setting).radar_auto_effort
      )
    },
  })

  const [setting, setSetting] = useState<RadarAutoEffortSetting>(
    DEFAULT_RADAR_AUTO_EFFORT
  )
  const [synced, setSynced] = useState<RadarAutoEffortSetting | undefined>(
    undefined
  )
  if (query.data && query.data !== synced) {
    setSynced(query.data)
    setSetting(query.data)
  }

  const save = useMutation({
    mutationFn: async (next: RadarAutoEffortSetting) => {
      const response = await updateUserRadarAutoEffort({
        policy: next.policy,
        min_iq_delta: next.min_iq_delta,
        models: next.models,
      })
      if (!response.success) {
        throw createServerError(response, t('Failed to update settings'))
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: RADAR_AUTO_EFFORT_QUERY_KEY,
      })
    },
    onError: (error) =>
      handleServerError(error, t('Failed to update settings')),
  })

  const update = async (next: RadarAutoEffortSetting): Promise<boolean> => {
    const previous = setting
    setSetting(next)
    try {
      await save.mutateAsync(next)
      return true
    } catch {
      setSetting(previous)
      return false
    }
  }

  return {
    setting,
    update,
    isSaving: save.isPending,
    isLoading: enabled && query.isLoading,
    setPolicy: (policy: RadarAutoEffortPolicy) =>
      update({ ...setting, policy }),
    setMinIQDelta: (minIQDelta: number) =>
      update({ ...setting, min_iq_delta: minIQDelta }),
    setModelEnabled: (model: string, modelEnabled: boolean) => {
      const key = model.trim().toLowerCase()
      const models = { ...setting.models }
      if (modelEnabled) {
        models[key] = { ...models[key], enabled: true }
      } else if (models[key]) {
        models[key] = { ...models[key], enabled: false }
      }
      return update({ ...setting, models })
    },
  }
}

export function isModelAutoEffortEnabled(
  setting: RadarAutoEffortSetting | undefined,
  model: string
): boolean {
  const entry = setting?.models?.[model.trim().toLowerCase()]
  return entry?.enabled === true
}
