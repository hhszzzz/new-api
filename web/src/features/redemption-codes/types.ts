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

// ============================================================================
// Redemption Schema & Types
// ============================================================================

export const redemptionSchema = z.object({
  id: z.number(),
  user_id: z.number(),
  name: z.string(),
  key: z.string(),
  status: z.number(), // 1: enabled, 2: disabled, 3: used
  quota: z.number(),
  type: z
    .enum(['balance', 'subscription', 'rate_limit', 'registration'])
    .optional(),
  batch_id: z.number().optional(),
  rpm_delta: z.number().optional(),
  concurrency_delta: z.number().optional(),
  validity_seconds: z.number().optional(),
  target_group: z.string().optional(),
  initial_rpm_limit: z.number().optional(),
  initial_concurrency_limit: z.number().optional(),
  created_time: z.number(),
  redeemed_time: z.number(),
  expired_time: z.number(), // 0 for never expires
  used_user_id: z.number(),
})

export type Redemption = z.infer<typeof redemptionSchema>

// ============================================================================
// API Request/Response Types
// ============================================================================

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
  batch_id?: number
}

export interface GetRedemptionsParams {
  p?: number
  page_size?: number
  type?: RedemptionType | ''
  batch_id?: number
  sort_by?: RedemptionSortBy
  sort_order?: SortOrder
}

export type RedemptionSortBy =
  | 'id'
  | 'name'
  | 'status'
  | 'quota'
  | 'created_time'
  | 'expired_time'
  | 'used_user_id'
  | 'redeemed_time'

export type RedemptionType =
  | 'balance'
  | 'subscription'
  | 'rate_limit'
  | 'registration'

export type SortOrder = 'asc' | 'desc'

export interface GetRedemptionsResponse {
  success: boolean
  message?: string
  data?: {
    items: Redemption[]
    total: number
    page: number
    page_size: number
  }
}

export interface SearchRedemptionsParams {
  keyword?: string
  status?: string
  type?: RedemptionType | ''
  batch_id?: number
  p?: number
  page_size?: number
  sort_by?: RedemptionSortBy
  sort_order?: SortOrder
}

export interface RedemptionFormData {
  id?: number
  name: string
  quota: number
  type: RedemptionType
  expired_time: number
  count?: number // Only for create
  status?: number // Only for status update
  plan_id?: number
  rpm_delta?: number
  concurrency_delta?: number
  validity_seconds?: number
  target_group?: string
  initial_rpm_limit?: number | null
  initial_concurrency_limit?: number | null
}

export interface RedemptionBatch {
  id: number
  name: string
  type: RedemptionType
  created_by: number
  created_at: number
  expired_time: number
  count: number
  quota: number
  rpm_delta: number
  concurrency_delta: number
  validity_seconds: number
  target_group?: string
  available_count: number
  used_count: number
  disabled_count: number
  expired_count: number
}

export interface RedemptionBatchDetail extends RedemptionBatch {
  redemptions: Redemption[]
}

// ============================================================================
// Dialog Types
// ============================================================================

export type RedemptionsDialogType = 'create' | 'update' | 'delete' | 'view'
