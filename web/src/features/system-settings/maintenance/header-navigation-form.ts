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
import * as z from 'zod'

import { isHttpUrl } from '@/lib/content-format'
import {
  normalizeReactIconName,
  REACT_ICON_NAME_MAX_LENGTH,
} from '@/lib/react-icon-name'

const customHeaderNavItemSchema = z.object({
  id: z.string().min(1),
  title: z
    .string()
    .trim()
    .min(1, 'Navigation name is required')
    .max(40, 'Navigation name must be 40 characters or fewer'),
  url: z
    .string()
    .trim()
    .min(1, 'URL is required')
    .max(2048, 'URL must be 2048 characters or fewer')
    .refine(isHttpUrl, 'Provide a valid URL starting with http:// or https://'),
  icon: z
    .string()
    .trim()
    .max(REACT_ICON_NAME_MAX_LENGTH, 'Icon name must be 80 characters or fewer')
    .refine(
      (value) => value.length === 0 || normalizeReactIconName(value) !== '',
      'Use a React Icons name such as LuRadar'
    ),
  enabled: z.boolean(),
})

export const headerNavSchema = z.object({
  home: z.boolean(),
  console: z.boolean(),
  pricingEnabled: z.boolean(),
  pricingRequireAuth: z.boolean(),
  modelStatusEnabled: z.boolean(),
  modelStatusRequireAuth: z.boolean(),
  modelRadarEnabled: z.boolean(),
  modelRadarRequireAuth: z.boolean(),
  rankingsEnabled: z.boolean(),
  rankingsRequireAuth: z.boolean(),
  docs: z.boolean(),
  about: z.boolean(),
  custom: z
    .array(customHeaderNavItemSchema)
    .max(20, 'You can add up to 20 custom navigation items'),
  order: z.array(z.string()),
})

export type HeaderNavFormValues = z.infer<typeof headerNavSchema>
export type HeaderNavBooleanField = Exclude<
  keyof HeaderNavFormValues,
  'custom' | 'order'
>
