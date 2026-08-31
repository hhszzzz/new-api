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
import { z } from 'zod'

export const accountPoolSettingsSchema = z
  .object({
    enabled: z.boolean(),
    hide_email_from_non_admins: z.boolean(),
    allowed_groups: z.array(z.string().min(1).max(64)),
    regular_refresh_seconds: z.coerce.number().int().min(60).max(3600),
    near_reset_threshold_seconds: z.coerce.number().int().min(60).max(3600),
    near_reset_refresh_seconds: z.coerce.number().int().min(30).max(600),
    post_reset_delay_seconds: z.coerce.number().int().min(0).max(120),
    manual_refresh_cooldown_seconds: z.coerce.number().int().min(30).max(600),
  })
  .refine(
    (values) =>
      values.near_reset_refresh_seconds <= values.regular_refresh_seconds,
    {
      path: ['near_reset_refresh_seconds'],
      message: 'Near-reset refresh must not exceed the regular interval',
    }
  )

export type AccountPoolSettingsValues = z.infer<
  typeof accountPoolSettingsSchema
>
