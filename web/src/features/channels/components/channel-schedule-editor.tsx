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
import { CalendarClock, Plus, Trash2 } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldContent,
  FieldGroup,
  FieldLabel,
  FieldSet,
  FieldTitle,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import {
  CHANNEL_SCHEDULE_WEEKDAYS,
  formatShanghaiTimestamp,
  normalizeChannelSchedule,
  shanghaiDateTimeLocalToUnix,
  unixToShanghaiDateTimeLocal,
} from '../lib/channel-schedule'
import type {
  ChannelSchedule,
  ChannelScheduleState,
  ChannelScheduleWindow,
} from '../types'

const DEFAULT_WINDOW: ChannelScheduleWindow = {
  start: '09:00',
  end: '18:00',
}

const WEEKDAY_LABELS = {
  monday: 'Monday',
  tuesday: 'Tuesday',
  wednesday: 'Wednesday',
  thursday: 'Thursday',
  friday: 'Friday',
  saturday: 'Saturday',
  sunday: 'Sunday',
} as const

const SCHEDULE_REASON_LABELS = {
  none: 'Available now',
  before_start: 'Not started',
  paused: 'Temporarily paused',
  expired: 'Expired',
  outside_weekly_window: 'Outside weekly window',
} as const

type ScheduleTimestampField = 'starts_at' | 'expires_at' | 'paused_until'

const TIMESTAMP_INPUT_LABELS: Record<ScheduleTimestampField, string> = {
  starts_at: 'Enable from date and time',
  expires_at: 'Disable after date and time',
  paused_until: 'Pause until date and time',
}

const DEFAULT_TIMESTAMP_OFFSET_SECONDS: Record<ScheduleTimestampField, number> =
  {
    starts_at: 60 * 60,
    expires_at: 30 * 24 * 60 * 60,
    paused_until: 60 * 60,
  }

type TimestampScheduleRuleProps = {
  field: ScheduleTimestampField
  label: 'Enable from' | 'Disable after' | 'Pause until'
  value: number | null | undefined
  onChange: (value: number | null) => void
  disabled?: boolean
}

function TimestampScheduleRule(props: TimestampScheduleRuleProps) {
  const { t } = useTranslation()
  const inputId = `channel-schedule-${props.field.replace('_', '-')}`
  const enabled = Boolean(props.value)
  const inputLabel = t(TIMESTAMP_INPUT_LABELS[props.field])

  return (
    <FieldSet disabled={props.disabled} className='gap-3 py-3'>
      <Field orientation='horizontal' data-disabled={props.disabled}>
        <FieldContent>
          <FieldTitle>{t(props.label)}</FieldTitle>
        </FieldContent>
        <Switch
          checked={enabled}
          onCheckedChange={(checked) => {
            props.onChange(
              checked
                ? Math.floor(Date.now() / 1000) +
                    DEFAULT_TIMESTAMP_OFFSET_SECONDS[props.field]
                : null
            )
          }}
          disabled={props.disabled}
          aria-label={t(props.label)}
        />
      </Field>

      {enabled && (
        <Field data-disabled={props.disabled}>
          <FieldLabel htmlFor={inputId} className='sr-only'>
            {inputLabel}
          </FieldLabel>
          <Input
            id={inputId}
            type='datetime-local'
            value={unixToShanghaiDateTimeLocal(props.value)}
            onChange={(event) =>
              props.onChange(shanghaiDateTimeLocalToUnix(event.target.value))
            }
            disabled={props.disabled}
          />
        </Field>
      )}
    </FieldSet>
  )
}

type WeeklyScheduleEditorProps = {
  enabled: boolean
  windows: Record<string, ChannelScheduleWindow[]>
  onChange: (
    enabled: boolean,
    windows: Record<string, ChannelScheduleWindow[]>
  ) => void
  disabled?: boolean
}

function cloneWeeklyWindows(
  windows: Record<string, ChannelScheduleWindow[]>
): Record<string, ChannelScheduleWindow[]> {
  return Object.fromEntries(
    Object.entries(windows).map(([weekday, values]) => [
      weekday,
      values.map((window) => ({ ...window })),
    ])
  )
}

export function WeeklyScheduleEditor(props: WeeklyScheduleEditorProps) {
  const { t } = useTranslation()
  const idPrefix = useId()
  const weeklyLabelId = `${idPrefix}-weekly-label`

  const updateDay = (
    weekday: string,
    windows: ChannelScheduleWindow[]
  ): void => {
    props.onChange(props.enabled, {
      ...cloneWeeklyWindows(props.windows),
      [weekday]: windows,
    })
  }

  const handleEnabledChange = (enabled: boolean): void => {
    props.onChange(enabled, cloneWeeklyWindows(props.windows))
  }

  return (
    <FieldSet
      disabled={props.disabled}
      aria-labelledby={weeklyLabelId}
      className='gap-3'
    >
      <Field orientation='horizontal' data-disabled={props.disabled}>
        <FieldContent>
          <FieldTitle id={weeklyLabelId}>{t('Weekly availability')}</FieldTitle>
        </FieldContent>
        <Switch
          checked={props.enabled}
          onCheckedChange={handleEnabledChange}
          disabled={props.disabled}
          aria-labelledby={weeklyLabelId}
        />
      </Field>

      {props.enabled && (
        <div
          data-testid='weekly-schedule-days'
          className='border-border bg-muted/20 ml-3 flex flex-col rounded-lg border-l-2 px-3 sm:ml-5 sm:px-4'
        >
          {CHANNEL_SCHEDULE_WEEKDAYS.map((weekday, dayIndex) => {
            const dayWindows = props.windows[weekday] || []
            const isDayEnabled = dayWindows.length > 0
            const isAllDay = dayWindows.some((window) => window.all_day)
            const dayLabelId = `${idPrefix}-${weekday}-label`
            const allDayId = `${idPrefix}-${weekday}-all-day`

            return (
              <div key={weekday} role='group' aria-labelledby={dayLabelId}>
                {dayIndex > 0 && <Separator />}
                <div className='py-3'>
                  <Field
                    orientation='horizontal'
                    data-disabled={props.disabled}
                  >
                    <FieldContent>
                      <FieldTitle id={dayLabelId}>
                        {t(WEEKDAY_LABELS[weekday])}
                      </FieldTitle>
                    </FieldContent>
                    <Switch
                      checked={isDayEnabled}
                      onCheckedChange={(checked) =>
                        updateDay(
                          weekday,
                          checked ? [{ ...DEFAULT_WINDOW }] : []
                        )
                      }
                      disabled={props.disabled}
                      aria-labelledby={dayLabelId}
                    />
                  </Field>

                  {isDayEnabled && (
                    <div className='border-border/60 mt-3 flex min-w-0 flex-col gap-2 border-l pl-3 sm:pl-4'>
                      <Field orientation='horizontal' className='w-fit'>
                        <Checkbox
                          id={allDayId}
                          checked={isAllDay}
                          onCheckedChange={(checked) =>
                            updateDay(
                              weekday,
                              checked
                                ? [{ all_day: true }]
                                : [{ ...DEFAULT_WINDOW }]
                            )
                          }
                          disabled={props.disabled}
                        />
                        <FieldLabel
                          htmlFor={allDayId}
                          className='text-muted-foreground font-normal'
                        >
                          {t('All day')}
                        </FieldLabel>
                      </Field>

                      {!isAllDay && (
                        <>
                          {dayWindows.map((window, windowIndex) => (
                            <div
                              // Schedule windows have no persisted identity and are fully controlled.
                              // eslint-disable-next-line react/no-array-index-key
                              key={`${weekday}-${windowIndex}`}
                              className='grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_2rem] items-center gap-2'
                            >
                              <Input
                                type='time'
                                value={window.start || ''}
                                onChange={(event) => {
                                  const next = dayWindows.map((item, index) =>
                                    index === windowIndex
                                      ? { ...item, start: event.target.value }
                                      : item
                                  )
                                  updateDay(weekday, next)
                                }}
                                aria-label={t('{{day}} start time', {
                                  day: t(WEEKDAY_LABELS[weekday]),
                                })}
                                className='min-w-0'
                                disabled={props.disabled}
                              />
                              <span className='text-muted-foreground text-xs'>
                                {t('to')}
                              </span>
                              <Input
                                type='time'
                                value={window.end || ''}
                                onChange={(event) => {
                                  const next = dayWindows.map((item, index) =>
                                    index === windowIndex
                                      ? { ...item, end: event.target.value }
                                      : item
                                  )
                                  updateDay(weekday, next)
                                }}
                                aria-label={t('{{day}} end time', {
                                  day: t(WEEKDAY_LABELS[weekday]),
                                })}
                                className='min-w-0'
                                disabled={props.disabled}
                              />
                              <Button
                                type='button'
                                variant='ghost'
                                size='icon'
                                onClick={() =>
                                  updateDay(
                                    weekday,
                                    dayWindows.filter(
                                      (_, index) => index !== windowIndex
                                    )
                                  )
                                }
                                disabled={props.disabled}
                                aria-label={t('Remove time window')}
                                title={t('Remove time window')}
                              >
                                <Trash2 aria-hidden='true' />
                              </Button>
                            </div>
                          ))}

                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            className='w-fit'
                            onClick={() =>
                              updateDay(weekday, [
                                ...dayWindows,
                                { ...DEFAULT_WINDOW },
                              ])
                            }
                            disabled={props.disabled || dayWindows.length >= 24}
                          >
                            <Plus data-icon='inline-start' aria-hidden='true' />
                            {t('Add time window')}
                          </Button>
                        </>
                      )}
                    </div>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </FieldSet>
  )
}

type ChannelScheduleEditorProps = {
  value: ChannelSchedule
  onChange: (schedule: ChannelSchedule) => void
  state?: ChannelScheduleState
  disabled?: boolean
}

export function ChannelScheduleEditor(props: ChannelScheduleEditorProps) {
  const { t, i18n } = useTranslation()
  const schedule = normalizeChannelSchedule(props.value)

  return (
    <div className='flex flex-col gap-5'>
      {props.state && (
        <Alert>
          <CalendarClock aria-hidden='true' />
          <AlertTitle>
            {t(SCHEDULE_REASON_LABELS[props.state.reason])}
          </AlertTitle>
          {props.state.next_transition_at && (
            <AlertDescription>
              {t('Next status change')}:{' '}
              {formatShanghaiTimestamp(
                props.state.next_transition_at,
                i18n.resolvedLanguage || i18n.language
              )}
            </AlertDescription>
          )}
        </Alert>
      )}

      <FieldGroup className='gap-0'>
        <TimestampScheduleRule
          field='starts_at'
          label='Enable from'
          value={schedule.starts_at}
          onChange={(value) =>
            props.onChange({ ...schedule, starts_at: value })
          }
          disabled={props.disabled}
        />
        <Separator />
        <TimestampScheduleRule
          field='expires_at'
          label='Disable after'
          value={schedule.expires_at}
          onChange={(value) =>
            props.onChange({ ...schedule, expires_at: value })
          }
          disabled={props.disabled}
        />
        <Separator />
        <TimestampScheduleRule
          field='paused_until'
          label='Pause until'
          value={schedule.paused_until}
          onChange={(value) =>
            props.onChange({ ...schedule, paused_until: value })
          }
          disabled={props.disabled}
        />
        <Separator />
        <div className='py-3'>
          <WeeklyScheduleEditor
            enabled={schedule.weekly_enabled}
            windows={schedule.weekly_windows}
            onChange={(enabled, windows) =>
              props.onChange({
                ...schedule,
                weekly_enabled: enabled,
                weekly_windows: windows,
              })
            }
            disabled={props.disabled}
          />
        </div>
      </FieldGroup>
    </div>
  )
}
