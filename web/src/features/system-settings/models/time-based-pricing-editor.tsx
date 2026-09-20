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
import { ChevronDown, Plus, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import type { PricingCurrency } from '@/features/model-pricing/currency'
import { COMMON_TIMEZONES } from '@/features/pricing/lib/billing-expr'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import { BillingTimeRangeFields } from './billing-time-fields'
import { TierPriceFields } from './tier-price-fields'
import {
  buildTimePricingExpression,
  createHourWindow,
  hasAbsolutePeakPrices,
  parseTimePricing,
  peakMultipliersFromPrices,
  peakPricesFromMultipliers,
  type HourWindow,
  type TimePricingConfig,
} from './time-pricing'

type TimeBasedPricingEditorProps = {
  currency: PricingCurrency
  billingExpr: string
  onBillingExprChange: (next: string) => void
}

const TIMEZONE_OPTIONS = COMMON_TIMEZONES.map((zone) => ({
  value: zone.value,
  label: zone.value,
}))

export function TimeBasedPricingEditor({
  currency,
  billingExpr,
  onBillingExprChange,
}: TimeBasedPricingEditorProps) {
  const { t, i18n } = useTranslation()
  const language = i18n.resolvedLanguage ?? i18n.language
  const [config, setConfig] = useState<TimePricingConfig>(() =>
    parseTimePricing(billingExpr)
  )
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const lastEmitted = useRef(billingExpr)

  useEffect(() => {
    if (billingExpr === lastEmitted.current) return
    lastEmitted.current = billingExpr
    setConfig(parseTimePricing(billingExpr))
  }, [billingExpr])

  const emit = (next: TimePricingConfig) => {
    setConfig(next)
    const expression = buildTimePricingExpression(next)
    lastEmitted.current = expression
    onBillingExprChange(expression)
  }

  const weekdayLabels = Array.from({ length: 7 }, (_, day) =>
    new Intl.DateTimeFormat(toIntlLocale(language), {
      weekday: 'short',
      timeZone: 'UTC',
    }).format(new Date(Date.UTC(2026, 0, 4 + day)))
  )

  const updateOffPeakPrice = (variable: string, value: string) => {
    const offPeak = { ...config.offPeak, [variable]: value }
    const peak = config.enabled
      ? peakPricesFromMultipliers(
          peakMultipliersFromPrices(config.peak, config.offPeak),
          offPeak
        )
      : config.peak
    emit({ ...config, offPeak, peak })
  }

  const updatePeakMultiplier = (variable: string, value: string) => {
    // Rebuild only the edited price from its multiplier so untouched peak
    // prices keep their exact saved values instead of drifting through the
    // rounded display ratio.
    const ratio = Number(value)
    const base = Number(config.offPeak[variable])
    const peak = { ...config.peak }
    if (
      Number.isFinite(ratio) &&
      ratio >= 0 &&
      Number.isFinite(base) &&
      base > 0
    ) {
      peak[variable] = String(Math.round(base * ratio * 10000) / 10000)
    } else {
      delete peak[variable]
    }
    emit({ ...config, peak })
  }

  const updatePeakPrice = (variable: string, value: string) => {
    emit({ ...config, peak: { ...config.peak, [variable]: value } })
  }

  const addWindow = () => {
    emit({
      ...config,
      windows: [...config.windows, createHourWindow('9', '18')],
    })
  }

  const updateWindow = (id: string, next: Partial<HourWindow>) => {
    emit({
      ...config,
      windows: config.windows.map((window) =>
        window.id === id ? { ...window, ...next } : window
      ),
    })
  }

  const peakMultipliers = peakMultipliersFromPrices(config.peak, config.offPeak)
  const showAdvancedPrices =
    advancedOpen ||
    hasAbsolutePeakPrices(peakMultipliers, config.peak, config.offPeak)

  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between gap-3 rounded-lg border p-3'>
        <div className='space-y-0.5'>
          <Label>{t('Peak hours pricing')}</Label>
          <p className='text-muted-foreground text-xs'>
            {t(
              'When off, the standard price is billed around the clock. When on, the peak multiplier applies inside the configured hours.'
            )}
          </p>
        </div>
        <Switch
          aria-label={t('Peak hours pricing')}
          checked={config.enabled}
          onCheckedChange={(checked) =>
            emit({
              ...config,
              enabled: checked,
              // Enabling starts at 1x so peak hours never silently bill zero.
              peak:
                checked && Object.keys(config.peak).length === 0
                  ? peakPricesFromMultipliers(
                      { p: '1', c: '1', cr: '1', cc: '1' },
                      config.offPeak
                    )
                  : config.peak,
            })
          }
        />
      </div>

      {config.enabled && (
        <div className='space-y-3 rounded-lg border p-3'>
          <div className='space-y-1'>
            <Label>{t('Peak hours')}</Label>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Start > end spans midnight. Leave every weekday unselected to apply to all days.'
              )}
            </p>
          </div>

          {config.preservedCondition ? (
            <Alert>
              <AlertDescription className='space-y-2 text-xs'>
                <p>
                  {t(
                    'This condition is too complex for the schedule editor. It is preserved as-is; edit it in expression mode.'
                  )}
                </p>
                <code className='block break-all'>
                  {config.preservedCondition}
                </code>
              </AlertDescription>
            </Alert>
          ) : (
            <>
              <div className='flex flex-wrap items-center gap-3'>
                <div className='w-56 max-w-full min-w-0'>
                  <Combobox
                    aria-label={t('Timezone')}
                    options={TIMEZONE_OPTIONS}
                    value={config.timezone}
                    allowCustomValue
                    onValueChange={(timezone) =>
                      timezone !== null && emit({ ...config, timezone })
                    }
                    className='w-full'
                  />
                </div>
                <div className='flex flex-wrap gap-1'>
                  {weekdayLabels.map((label, day) => {
                    const active = config.weekdays.includes(day)
                    return (
                      <Button
                        key={label}
                        type='button'
                        variant={active ? 'default' : 'outline'}
                        size='sm'
                        className='h-8 w-10 px-0 text-xs'
                        aria-pressed={active}
                        onClick={() =>
                          emit({
                            ...config,
                            weekdays: active
                              ? config.weekdays.filter((item) => item !== day)
                              : [...config.weekdays, day],
                          })
                        }
                      >
                        {label}
                      </Button>
                    )
                  })}
                </div>
              </div>

              <div className='space-y-2'>
                {config.windows.map((window) => (
                  <div key={window.id} className='flex items-center gap-2'>
                    <BillingTimeRangeFields
                      probe='hour'
                      start={window.start}
                      end={window.end}
                      onChange={(start, end) =>
                        updateWindow(window.id, { start, end })
                      }
                    />
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      aria-label={t('Remove time window')}
                      className='ml-auto'
                      onClick={() =>
                        emit({
                          ...config,
                          windows: config.windows.filter(
                            (item) => item.id !== window.id
                          ),
                        })
                      }
                    >
                      <Trash2 className='text-destructive h-4 w-4' />
                    </Button>
                  </div>
                ))}
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={addWindow}
                >
                  <Plus className='mr-1.5 h-3.5 w-3.5' />
                  {t('Add time window')}
                </Button>
              </div>
            </>
          )}
        </div>
      )}

      <div className='space-y-2 rounded-lg border p-3'>
        {/* The base section is the standard price: off-peak hours simply bill
            it unchanged, so there is no separate "off-peak price" concept. */}
        <Label>{t('Standard price')}</Label>
        <TierPriceFields
          currency={currency}
          prices={config.offPeak}
          onChange={updateOffPeakPrice}
        />
      </div>

      {config.enabled && (
        <div className='space-y-2 rounded-lg border p-3'>
          <Label>{t('Peak price')}</Label>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Peak multiplier of the standard price. The advanced option can override it with absolute prices.'
            )}
          </p>
          <div className='flex flex-wrap gap-x-4 gap-y-2'>
            {(['p', 'c', 'cr', 'cc'] as const).map((key) => (
              <MultiplierField
                key={key}
                label={t(
                  {
                    p: 'Input price',
                    c: 'Completion price',
                    cr: 'Cache read price',
                    cc: 'Cache create price',
                  }[key]
                )}
                value={peakMultipliers[key] ?? ''}
                onChange={(value) => updatePeakMultiplier(key, value)}
              />
            ))}
          </div>
          <div className='space-y-1.5'>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='h-7 px-2 text-xs'
              onClick={() => setAdvancedOpen((prev) => !prev)}
            >
              <ChevronDown
                className={cn(
                  'mr-1 h-3 w-3 transition-transform',
                  showAdvancedPrices && 'rotate-180'
                )}
              />
              {t('Advanced absolute prices')}
            </Button>
            {showAdvancedPrices && (
              <>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Absolute prices override the standard price multipliers.'
                  )}
                </p>
                <TierPriceFields
                  currency={currency}
                  prices={config.peak}
                  onChange={updatePeakPrice}
                />
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function MultiplierField(props: {
  label: string
  value: string
  onChange: (next: string) => void
}) {
  return (
    <div className='w-36 space-y-0.5'>
      <Label className='text-muted-foreground text-xs'>{props.label}</Label>
      <div className='flex h-8 w-full items-center rounded-md border px-2'>
        <input
          type='text'
          inputMode='decimal'
          aria-label={props.label}
          value={props.value}
          onChange={(event) => props.onChange(event.target.value)}
          className='h-full w-full min-w-0 bg-transparent text-sm outline-none'
        />
        <span className='text-muted-foreground shrink-0 text-xs'>x</span>
      </div>
    </div>
  )
}
