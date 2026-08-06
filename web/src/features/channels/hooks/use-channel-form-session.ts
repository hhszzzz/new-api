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
import { useCallback, useEffect, useRef } from 'react'

type ResettableForm<TValues> = {
  reset: (values: TValues) => void
}

type ChannelFormSessionOptions<TValues, TRecord> = {
  defaultValues: TValues
  form: ResettableForm<TValues>
  isDirty: boolean
  isEditing: boolean
  open: boolean
  onHydrate?: (values: TValues, record: TRecord) => void
  onReset?: () => void
  record: TRecord | null | undefined
  recordId: number | null
  transform: (record: TRecord) => TValues
}

export function useChannelFormSession<TValues, TRecord>(
  options: ChannelFormSessionOptions<TValues, TRecord>
) {
  const {
    defaultValues,
    form,
    isDirty,
    isEditing,
    onHydrate,
    onReset,
    open,
    record,
    recordId,
    transform,
  } = options
  const hydratedChannelIdRef = useRef<number | 'create' | null>(null)
  const previousOpenRef = useRef(open)

  const resetSession = useCallback(() => {
    hydratedChannelIdRef.current = isEditing ? null : 'create'
    form.reset(defaultValues)
    onReset?.()
  }, [defaultValues, form, isEditing, onReset])

  useEffect(() => {
    const wasOpen = previousOpenRef.current
    previousOpenRef.current = open
    if (!open) {
      if (wasOpen) resetSession()
      return
    }

    if (!isEditing) {
      if (hydratedChannelIdRef.current !== 'create') resetSession()
      return
    }
    if (!record || recordId === null) return

    const targetChanged = hydratedChannelIdRef.current !== recordId
    if (!targetChanged && isDirty) return

    const values = transform(record)
    form.reset(values)
    hydratedChannelIdRef.current = recordId
    onHydrate?.(values, record)
  }, [
    form,
    isDirty,
    isEditing,
    onHydrate,
    open,
    record,
    recordId,
    resetSession,
    transform,
  ])

  return { resetSession }
}
