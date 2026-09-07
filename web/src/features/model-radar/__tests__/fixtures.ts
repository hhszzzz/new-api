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
import type { ModelRadarConfiguration } from '../types'

export const configurationFixture: ModelRadarConfiguration = {
  model: 'gpt-radar',
  effort: 'medium',
  harness: 'codex',
  runs_24h: 3,
  runs_48h: 6,
  average_price_usd_by_band: null,
  iq: 93.75,
  passed: 7,
  valid_tasks: 10,
  average_price_usd: 1.25,
  price_samples: 10,
  average_minutes: 4.5,
  duration_samples: 9,
  incomplete_cost_samples: 1,
  total_runs: 12,
  latest_graded_at: 1_800_000_000,
  average_agent_steps: 22,
  agent_steps_samples: 8,
  average_total_tokens: 12_345,
  token_samples: 7,
  cache_hit_rate: 0.75,
  cache_token_samples: 6,
  combined_cost_index: 45,
}
