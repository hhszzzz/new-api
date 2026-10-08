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
import { useTranslation } from 'react-i18next'

import { SettingsSwitchField } from '@/features/system-settings/components/settings-form-layout'

import {
  FULL_PROMPT_DEFAULT_RUNES,
  FULL_PROMPT_MAX_RUNES,
  FULL_PROMPT_MIN_RUNES,
  FULL_PROMPT_NO_LIMIT,
} from '../lib'
import { NumberField } from './number-field'

type StoredPromptLimitFieldProps = {
  value: number | undefined
  onChange: (value: number) => void
}

/**
 * How much of the whole request an audit record keeps. The limit and the
 * unlimited mode are separate controls on purpose: the unlimited mode is stored
 * as 0, and a number input whose text has just been cleared reports 0, so one
 * field could not tell "keep everything" apart from "not filled in yet". The
 * switch only ever writes 0 or a real limit, and the number below only accepts a
 * real limit.
 */
export function StoredPromptLimitField(props: StoredPromptLimitFieldProps) {
  const { t } = useTranslation()
  const limit = props.value ?? FULL_PROMPT_DEFAULT_RUNES
  const keepsEverything = limit === FULL_PROMPT_NO_LIMIT
  return (
    <div className='flex flex-col gap-3'>
      <SettingsSwitchField
        controlId='prompt-audit-full-prompt-unlimited'
        checked={keepsEverything}
        onCheckedChange={(unlimited) =>
          props.onChange(
            unlimited ? FULL_PROMPT_NO_LIMIT : FULL_PROMPT_DEFAULT_RUNES
          )
        }
        label={t('Store all request content and model replies')}
        description={t(
          'Keep all request sources and generated replies. Records grow with stored content and stay for the retention period above.'
        )}
      />
      {/* Hidden rather than disabled while unlimited: the switch turning off is
          what puts the limit back, so there is nothing to edit underneath. */}
      {!keepsEverything && (
        <NumberField
          id='prompt-audit-full-prompt-limit'
          label={t('Stored characters per content block')}
          value={limit}
          min={FULL_PROMPT_MIN_RUNES}
          max={FULL_PROMPT_MAX_RUNES}
          onChange={props.onChange}
          description={t(
            'Maximum characters kept from each content block and generated reply. Turn on full storage above to keep all characters.'
          )}
        />
      )}
    </div>
  )
}
