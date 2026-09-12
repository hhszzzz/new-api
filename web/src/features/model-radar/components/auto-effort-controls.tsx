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
import { GaugeIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type {
  RadarAutoEffortPolicy,
  RadarAutoEffortSetting,
} from '@/features/profile/types'

import { AUTO_EFFORT_POLICIES, MIN_IQ_DELTA_RANGE } from '../lib/model-radar'

const DEFAULT_MIN_IQ_DELTA = 5

/**
 * Compact control that lives in the page header, on the same row as the
 * "Model Radar" title: the strategy selector plus a one-line explanation. It
 * only configures the strategy; per-model switches in the tier grid decide
 * which models it applies to.
 *
 * Writes are optimistic, so the control never renders a saving state. That
 * keeps the row from shifting or dimming while a switch is toggled; edits that
 * arrive mid-flight are ignored instead.
 */
export function AutoEffortControls(props: {
  setting: RadarAutoEffortSetting
  isSaving: boolean
  onPolicyChange: (policy: RadarAutoEffortPolicy) => void
  onMinIQDeltaChange: (minIQDelta: number) => void
}) {
  const { t } = useTranslation()
  const [activeOption, setActiveOption] = useState<string | null>(null)
  const configuredDelta = props.setting.min_iq_delta ?? DEFAULT_MIN_IQ_DELTA
  const [draft, setDraft] = useState(String(configuredDelta))
  const [syncedDelta, setSyncedDelta] = useState(configuredDelta)
  if (syncedDelta !== configuredDelta) {
    setSyncedDelta(configuredDelta)
    setDraft(String(configuredDelta))
  }

  const policy = props.setting.policy ?? 'highest_iq'
  const options = AUTO_EFFORT_POLICIES.map((item) => ({
    value: item.value,
    label: t(item.labelKey),
    description: t(item.descriptionKey),
  }))

  const commitDraft = () => {
    if (props.isSaving) return
    const value = Number(draft)
    if (
      draft.trim() === '' ||
      !Number.isFinite(value) ||
      value < MIN_IQ_DELTA_RANGE.min ||
      value > MIN_IQ_DELTA_RANGE.max
    ) {
      setDraft(String(configuredDelta))
      return
    }
    if (value === configuredDelta) return
    props.onMinIQDeltaChange(value)
  }

  return (
    <div className='flex flex-col items-end gap-1'>
      <div className='flex flex-wrap items-center justify-end gap-2'>
        <span
          className='text-muted-foreground inline-flex shrink-0'
          title={t(
            'Tiers come from the model radar and are only applied while its data is fresh. Use the switches next to each model to choose which models are affected.'
          )}
        >
          <HugeiconsIcon
            icon={GaugeIcon}
            className='size-4'
            strokeWidth={2}
            aria-hidden='true'
          />
        </span>
        <span className='text-muted-foreground text-sm font-medium'>
          {t('Automatic reasoning tier')}
        </span>
        <Label htmlFor='radar-auto-effort-policy' className='sr-only'>
          {t('Selection strategy')}
        </Label>
        <Select
          items={options}
          value={policy}
          onValueChange={(value) => {
            if (props.isSaving) return
            props.onPolicyChange(value as RadarAutoEffortPolicy)
          }}
        >
          <SelectTrigger id='radar-auto-effort-policy' className='w-44'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent
            align='start'
            alignItemWithTrigger={false}
            sideOffset={6}
            className='min-w-44'
            onMouseLeave={() => setActiveOption(null)}
          >
            <TooltipProvider>
              {options.map((option) => (
                <SelectItem
                  key={option.value}
                  value={option.value}
                  className='hover:bg-accent'
                  onMouseEnter={() => setActiveOption(option.value)}
                  onMouseLeave={() =>
                    setActiveOption((current) =>
                      current === option.value ? null : current
                    )
                  }
                  onFocus={() => setActiveOption(option.value)}
                  onBlur={() =>
                    setActiveOption((current) =>
                      current === option.value ? null : current
                    )
                  }
                >
                  <Tooltip open={activeOption === option.value}>
                    <TooltipTrigger
                      render={
                        <span className='block w-full truncate'>
                          {option.label}
                        </span>
                      }
                    />
                    <TooltipContent
                      side='left'
                      sideOffset={10}
                      className='max-w-xs leading-relaxed shadow-lg [&>[data-slot=tooltip-arrow]]:hidden'
                    >
                      {option.description}
                    </TooltipContent>
                  </Tooltip>
                </SelectItem>
              ))}
            </TooltipProvider>
          </SelectContent>
        </Select>

        {policy === 'min_iq_delta' ? (
          <>
            <Label
              htmlFor='radar-auto-effort-min-iq-delta'
              className='text-muted-foreground text-sm font-medium'
            >
              {t('IQ gap')}
            </Label>
            <Input
              id='radar-auto-effort-min-iq-delta'
              type='number'
              min={MIN_IQ_DELTA_RANGE.min}
              max={MIN_IQ_DELTA_RANGE.max}
              step={1}
              inputMode='decimal'
              value={draft}
              className='w-20'
              title={t(
                'The tier only changes when the best tier beats the one you sent by at least this IQ gap.'
              )}
              onChange={(event) => setDraft(event.target.value)}
              onBlur={commitDraft}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  commitDraft()
                }
              }}
            />
          </>
        ) : null}
      </div>
      <p className='text-muted-foreground max-w-md text-right text-xs'>
        {t(
          'Automatic reasoning tiers adjust to a suitable reasoning effort based on the strategy.'
        )}
      </p>
    </div>
  )
}
