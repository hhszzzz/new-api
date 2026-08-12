# Ranking Timezone Fix

## Problem

Previously, the rankings charts displayed inconsistent time labels for users in different timezones:

1. **Backend** generated pre-formatted time labels (`label` field) using the server's local timezone in `service/rankings.go:1099`
2. **Frontend** sent custom date range timestamps based on the user's browser timezone
3. Users in different timezones would see different time labels for the same data point
4. Custom date ranges wouldn't match the displayed labels

### Example Issue

- Server in UTC+8, admin sees "Aug 12 20:00"
- User in UTC-5 views the same data, sees "Aug 12 07:00" (13-hour difference)
- User selects "2026-08-01 00:00" in their timezone, but chart shows server timezone labels

## Solution

**Frontend now generates localized time labels** based on the user's browser timezone, instead of using the backend's pre-formatted labels.

### Changes

#### Backend (`service/rankings.go`)

- **Line 1099**: `rankingBucketLabel` now uses `.UTC()` for consistency
  ```go
  func rankingBucketLabel(bucket int64, config rankingPeriodConfig) string {
      return time.Unix(bucket, 0).UTC().Format(config.labelLayout)
  }
  ```
- The `label` field in API responses is now always UTC (for backward compatibility)
- The `ts` field (RFC3339 UTC timestamp) remains the source of truth

#### Frontend

**New utility** (`web/src/features/rankings/lib/format.ts:71-90`):
```typescript
export function formatChartLabel(
  ts: string,
  bucket: 'hour' | 'day' | 'week'
): string
```
Converts the `ts` (RFC3339 UTC) to a localized label based on `bucket` granularity and the user's browser timezone.

**Updated components**:
- `ModelsSection` (`web/src/features/rankings/components/models-section.tsx`)
  - Added `bucket` prop
  - Generates `localLabel` from `ts` using `formatChartLabel`
  - Chart uses `xField: 'localLabel'` instead of `'label'`
  - Tooltip uses `localLabel`

- `MarketShareSection` (`web/src/features/rankings/components/market-share-section.tsx`)
  - Same changes as `ModelsSection`

- `Rankings` page (`web/src/features/rankings/index.tsx`)
  - Passes `snapshot.range.bucket` to both chart components

### How It Works

1. Backend returns `ts` (UTC RFC3339), `label` (UTC formatted, unused), and `bucket` (time granularity)
2. Frontend receives user's local timezone from browser
3. For each data point, frontend calls `formatChartLabel(point.ts, bucket)`:
   - Parses `ts` as UTC
   - Converts to user's local timezone
   - Formats based on `bucket`:
     - `hour` → "Aug 12 15:04"
     - `day` → "Aug 12"
     - `week` → "Aug 12"
4. Chart displays the localized `localLabel`

## Benefits

✅ **All users see consistent time labels** in their own timezone  
✅ **Custom date ranges match the displayed labels**  
✅ **No backend changes needed** for different user timezones  
✅ **Backward compatible** — `label` field still exists for API consumers  
✅ **No data migration required**

## Impact

- ✅ Models History chart
- ✅ Vendor Share History chart
- ✅ All time periods (today, week, month, year, custom)
- ✅ Tooltips
- ❌ No impact on data aggregation or user usage calculations

## Testing

Run backend tests:
```bash
go test ./service -run TestRankingBucketLabel
```

Run frontend type checking:
```bash
cd web && bun run typecheck
```

Build frontend:
```bash
cd web && bun run build
```
