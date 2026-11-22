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
import { Download, Loader2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { getDefaultTimeRange } from '@/features/usage-logs/lib/utils'
import dayjs from '@/lib/dayjs'
import { handleServerError } from '@/lib/handle-server-error'

import { listDistributionRiskEvents } from '../api'
import {
  buildDistributionRiskCsv,
  downloadCsv,
  fetchAllDistributionRiskEvents,
} from '../lib/export'
import type { DistributionRiskEventQuery } from '../types'

const MAX_RANGE_DAYS = 365
const DAY_MS = 24 * 60 * 60 * 1000

export function DistributionRiskExportDialog(props: {
  filters: DistributionRiskEventQuery
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [range, setRange] = useState<{ start?: Date; end?: Date }>({})
  const [exportedCount, setExportedCount] = useState<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  useEffect(() => () => abortRef.current?.abort(), [])

  const startMs = range.start?.getTime()
  const endMs = range.end?.getTime()
  let rangeError: string | null = null
  if (startMs !== undefined && endMs !== undefined) {
    if (endMs < startMs) {
      rangeError = t('End time must be after start time')
    } else if (endMs - startMs > MAX_RANGE_DAYS * DAY_MS) {
      rangeError = t('The time range cannot exceed {{days}} days', {
        days: MAX_RANGE_DAYS,
      })
    }
  }
  const exporting = exportedCount !== null
  const canExport =
    startMs !== undefined && endMs !== undefined && !rangeError && !exporting

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      const defaults = getDefaultTimeRange()
      setRange({
        start:
          props.filters.start_timestamp !== undefined
            ? new Date(props.filters.start_timestamp * 1000)
            : defaults.start,
        end:
          props.filters.end_timestamp !== undefined
            ? new Date(props.filters.end_timestamp * 1000)
            : defaults.end,
      })
    } else {
      abortRef.current?.abort()
    }
    setOpen(nextOpen)
  }

  const handleExport = async () => {
    if (!range.start || !range.end || !canExport || abortRef.current) return

    const controller = new AbortController()
    abortRef.current = controller
    const filters: DistributionRiskEventQuery = {
      ...props.filters,
      start_timestamp: Math.floor(range.start.getTime() / 1000),
      end_timestamp: Math.floor(range.end.getTime() / 1000),
    }
    setExportedCount(0)

    try {
      const events = await fetchAllDistributionRiskEvents(
        (page, pageSize, signal) =>
          listDistributionRiskEvents(filters, page, pageSize, signal),
        controller.signal,
        setExportedCount
      )

      controller.signal.throwIfAborted()
      const lines = buildDistributionRiskCsv(events, t)
      downloadCsv(
        `risk-logs-${dayjs(range.start).format('YYYYMMDD')}-${dayjs(range.end).format('YYYYMMDD')}.csv`,
        lines
      )
      toast.success(t('Exported {{count}} logs', { count: events.length }))
      setOpen(false)
    } catch (error) {
      if (!controller.signal.aborted) {
        handleServerError(error, t('Failed to export logs'))
      }
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = null
        setExportedCount(null)
      }
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      trigger={
        <Button type='button' variant='outline'>
          <Download data-icon='inline-start' />
          {t('Export')}
        </Button>
      }
      title={t('Export logs')}
      description={t(
        'Choose a time range of up to {{days}} days. Applied filters on this page also apply.',
        { days: MAX_RANGE_DAYS }
      )}
      contentClassName='sm:max-w-md'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={handleExport}
            disabled={!canExport}
            aria-busy={exporting}
          >
            {exporting && <Loader2 className='animate-spin' />}
            {t('Export')}
          </Button>
        </>
      }
    >
      <div className='space-y-2'>
        <CompactDateTimeRangePicker
          start={range.start}
          end={range.end}
          onChange={(nextRange) => {
            setRange(nextRange)
          }}
        />
        {rangeError && (
          <p role='alert' className='text-destructive text-sm'>
            {rangeError}
          </p>
        )}
        {exporting && (
          <p aria-live='polite' className='text-muted-foreground text-sm'>
            {t('Exported {{count}} logs', { count: exportedCount })}
          </p>
        )}
      </div>
    </Dialog>
  )
}
