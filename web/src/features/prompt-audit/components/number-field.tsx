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
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

type NumberFieldProps = {
  id: string
  label: string
  value: number
  min: number
  max: number
  description: string
  onChange: (value: number) => void
}

// Label + control + description, no bordered tile: spacing between fields comes
// from the surrounding grid, so rows stay readable when several sit side by side.
export function NumberField({
  id,
  label,
  value,
  min,
  max,
  description,
  onChange,
}: NumberFieldProps) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type='number'
        min={min}
        max={max}
        value={value}
        className='font-mono'
        onChange={(event) => onChange(Number(event.target.value || '0'))}
      />
      <FieldDescription>{description}</FieldDescription>
    </Field>
  )
}
