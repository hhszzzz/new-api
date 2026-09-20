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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { SettingsSection } from '../components/settings-section'

type RequestChecksSectionProps = {
  defaultValues: {
    CheckSensitiveEnabled: boolean
    CheckSensitiveOnPromptEnabled: boolean
    SensitiveWords?: string
  }
}

export function RequestChecksSection(_props: RequestChecksSectionProps) {
  const { t } = useTranslation()
  return (
    <SettingsSection title={t('Sensitive Words')}>
      <div className='space-y-4'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Sensitive words are now managed with prompt inspection rules and wordlists.'
          )}
        </p>
        <div className='flex flex-wrap gap-2'>
          <Button render={<Link to='/prompt-audit/settings' />}>
            {t('Inspection rules')}
          </Button>
          <Button
            variant='outline'
            render={<Link to='/prompt-audit/wordlists' />}
          >
            {t('Wordlists')}
          </Button>
        </div>
      </div>
    </SettingsSection>
  )
}
