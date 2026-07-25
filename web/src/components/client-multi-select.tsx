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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { MultiSelect, type Option } from '@/components/multi-select'

interface ClientMultiSelectProps {
  selected: string[]
  onChange: (clients: string[]) => void
  availableClients?: string[]
  disabled?: boolean
  id?: string
}

const BUILTIN_CLIENT_OPTIONS: Option[] = [
  { label: 'Codex', value: 'codex' },
  { label: 'Claude Code', value: 'claude_code' },
]

function normalizeClientName(value: string): string {
  return value.trim().toLowerCase()
}

export function ClientMultiSelect(props: ClientMultiSelectProps) {
  const { t } = useTranslation()
  const options = useMemo(() => {
    const byValue = new Map(
      BUILTIN_CLIENT_OPTIONS.map((option) => [option.value, option])
    )
    for (const raw of props.availableClients ?? []) {
      const value = normalizeClientName(raw)
      if (!value || byValue.has(value)) continue
      byValue.set(value, { label: value, value })
    }
    return [...byValue.values()]
  }, [props.availableClients])

  return (
    <MultiSelect
      id={props.id}
      options={options}
      selected={props.selected.map(normalizeClientName).filter(Boolean)}
      onChange={(clients) =>
        props.onChange([
          ...new Set(clients.map(normalizeClientName).filter(Boolean)),
        ])
      }
      placeholder={t('Select clients or add custom names')}
      allowCreate
      createLabel='Add custom client "{{value}}"'
      maxVisibleChips={6}
      disabled={props.disabled}
    />
  )
}
