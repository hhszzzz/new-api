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
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  SettingsFormGrid,
  SettingsFormGridItem,
} from '@/features/system-settings/components/settings-form-layout'

import { bytesToMB, mbToBytes } from '../lib'
import type { PromptAuditConfigUpdate } from '../types'
import { NumberField } from './number-field'

// Optional properties must be stripped first: PromptAuditConfigUpdate marks
// several numeric fields optional, and `number | undefined extends number` is
// false, which would silently drop them from this union.
type NumericConfigKey = {
  [K in keyof PromptAuditConfigUpdate]-?: NonNullable<
    PromptAuditConfigUpdate[K]
  > extends number
    ? K
    : never
}[keyof PromptAuditConfigUpdate]

type AdvancedNumericFieldSpec = {
  id: string
  key: NumericConfigKey
  label: string
  min: number
  max: number | ((config: PromptAuditConfigUpdate) => number)
  description: string
  full?: boolean
  display?: (value: number) => number
  persist?: (displayValue: number) => number
}

type AdvancedNumericGroupSpec = {
  title: string
  fields: AdvancedNumericFieldSpec[]
}

type AdvancedLimitsSectionProps = {
  config: PromptAuditConfigUpdate
  onChange: (patch: Partial<PromptAuditConfigUpdate>) => void
}

// Bounds mirror validatePromptAuditConfig() in ../lib.ts, which validates in
// bytes: the two MB fields convert through bytesToMB/mbToBytes, everything else
// is stored exactly as displayed. Keep both sides in sync.
function numericPatch<K extends NumericConfigKey>(
  key: K,
  value: number
): Partial<PromptAuditConfigUpdate> {
  return { [key]: value } as Pick<PromptAuditConfigUpdate, K>
}

// The group list is built inside the component so every label is an inline
// translation call the i18n key scraper can see.
export function AdvancedLimitsSection({
  config,
  onChange,
}: AdvancedLimitsSectionProps) {
  const { t } = useTranslation()
  const groups: AdvancedNumericGroupSpec[] = [
    {
      title: t('Request timeouts and cache'),
      fields: [
        {
          id: 'prompt-audit-total-timeout',
          key: 'total_timeout_ms',
          label: t('Total timeout (ms)'),
          min: 100,
          max: 120000,
          description: t('Deadline for reviewing the complete request.'),
        },
        {
          id: 'prompt-audit-overlap',
          key: 'chunk_overlap',
          label: t('Chunk overlap (characters)'),
          min: 0,
          max: 512,
          description: t(
            'Overlap between adjacent chunks to resist boundary bypasses.'
          ),
        },
        {
          id: 'prompt-audit-chunk-concurrency',
          key: 'chunk_concurrency',
          label: t('Chunk concurrency'),
          min: 1,
          max: 16,
          description: t(
            'Maximum parallel checks per prompt, within the global and node limits. A blocking result stops later batches.'
          ),
        },
        {
          id: 'prompt-audit-cache-ttl',
          key: 'cache_ttl_seconds',
          label: t('Cache TTL (seconds)'),
          min: 0,
          max: 86400,
          description: t('Use 0 to disable result caching.'),
          full: true,
        },
      ],
    },
    {
      title: t('Output buffering and retention'),
      fields: [
        {
          id: 'prompt-audit-output-memory',
          key: 'output_memory_bytes',
          label: t('Output memory threshold (MB)'),
          min: 1,
          max: (current) => Math.max(1, bytesToMB(current.output_max_bytes)),
          description: t('Larger buffered outputs spill to temporary storage.'),
          display: bytesToMB,
          persist: mbToBytes,
        },
        {
          id: 'prompt-audit-output-limit',
          key: 'output_max_bytes',
          label: t('Maximum output capture (MB)'),
          min: 1,
          max: 64,
          description: t(
            'Blocking stops delivery when this limit is exceeded.'
          ),
          display: bytesToMB,
          persist: mbToBytes,
        },
        {
          id: 'prompt-audit-retention',
          key: 'retention_days',
          label: t('Retention (days)'),
          min: 0,
          max: 3650,
          description: t('Use 0 to retain full prompt text permanently.'),
          full: true,
        },
      ],
    },
    {
      title: t('Queue workers and concurrency'),
      fields: [
        {
          id: 'prompt-audit-workers',
          key: 'worker_count',
          label: t('Async workers'),
          min: 1,
          max: 64,
          description: t('Durable queue worker count on the elected master.'),
        },
        {
          id: 'prompt-audit-attempts',
          key: 'max_attempts',
          label: t('Maximum attempts'),
          min: 1,
          max: 4,
          description: t('Includes the initial asynchronous audit attempt.'),
        },
        {
          id: 'prompt-audit-global-concurrency',
          key: 'global_concurrency',
          label: t('Global concurrency'),
          min: 1,
          max: 1024,
          description: t('Maximum simultaneous audit calls in this process.'),
        },
        {
          id: 'prompt-audit-node-concurrency',
          key: 'endpoint_concurrency',
          label: t('Default audit model concurrency'),
          min: 1,
          max: 256,
          description: t(
            'Fallback concurrency used by newly configured audit models.'
          ),
        },
      ],
    },
  ]

  return (
    <Collapsible className='flex flex-col gap-4'>
      <CollapsibleTrigger
        render={
          <Button
            type='button'
            variant='ghost'
            className='group justify-between px-0 hover:bg-transparent aria-expanded:bg-transparent dark:hover:bg-transparent'
          />
        }
      >
        <h3 className='text-base font-semibold'>{t('Advanced parameters')}</h3>
        <ChevronDown
          className='text-muted-foreground size-4 shrink-0 transition-transform duration-200 group-aria-expanded:rotate-180'
          aria-hidden='true'
        />
      </CollapsibleTrigger>
      <CollapsibleContent className='flex flex-col gap-6'>
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {t(
            'Limits are measured in Unicode characters, milliseconds, seconds, or concurrent requests as labeled.'
          )}
        </p>
        {groups.map((group) => (
          <div key={group.title} className='flex flex-col gap-3'>
            <div className='text-muted-foreground border-b pb-2 text-xs font-medium'>
              {group.title}
            </div>
            <SettingsFormGrid>
              {group.fields.map((spec) => {
                const stored = config[spec.key]
                return (
                  <SettingsFormGridItem
                    key={spec.id}
                    span={spec.full ? 'full' : undefined}
                  >
                    <NumberField
                      id={spec.id}
                      label={spec.label}
                      value={spec.display ? spec.display(stored) : stored}
                      min={spec.min}
                      max={
                        typeof spec.max === 'number'
                          ? spec.max
                          : spec.max(config)
                      }
                      description={spec.description}
                      onChange={(next) =>
                        onChange(
                          numericPatch(
                            spec.key,
                            spec.persist ? spec.persist(next) : next
                          )
                        )
                      }
                    />
                  </SettingsFormGridItem>
                )
              })}
            </SettingsFormGrid>
          </div>
        ))}
      </CollapsibleContent>
    </Collapsible>
  )
}
