package controller

import (
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyUserBatchListMode(t *testing.T) {
	current := []string{"gpt-5.4", "claude-4.5"}
	tests := []struct {
		name string
		op   userBatchListOp
		want []string
	}{
		{
			name: "append merges without duplicates",
			op:   userBatchListOp{Mode: userBatchListModeAppend, Models: []string{"claude-4.5", "gemini-3"}},
			want: []string{"gpt-5.4", "claude-4.5", "gemini-3"},
		},
		{
			name: "remove drops listed models only",
			op:   userBatchListOp{Mode: userBatchListModeRemove, Models: []string{"gpt-5.4", "not-present"}},
			want: []string{"claude-4.5"},
		},
		{
			name: "replace swaps the whole list",
			op:   userBatchListOp{Mode: userBatchListModeReplace, Models: []string{"gemini-3"}},
			want: []string{"gemini-3"},
		},
		{
			name: "replace with empty list clears it",
			op:   userBatchListOp{Mode: userBatchListModeReplace},
			want: []string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, applyUserBatchListMode(current, test.op))
		})
	}
	// The source list must never be mutated by any mode.
	assert.Equal(t, []string{"gpt-5.4", "claude-4.5"}, current)
}

func TestValidateAndBuildUserBatchRateLimits(t *testing.T) {
	tests := []struct {
		name        string
		op          *userBatchRateLimitsOp
		wantChanged bool
		wantErr     string
	}{
		{name: "nil", op: nil},
		{name: "keep only", op: &userBatchRateLimitsOp{RpmLimit: &userBatchRateLimitOp{Mode: userBatchCheckinKeep}}},
		{name: "clear", op: &userBatchRateLimitsOp{ConcurrencyLimit: &userBatchRateLimitOp{Mode: userBatchRateLimitClear}}, wantChanged: true},
		{name: "custom", op: &userBatchRateLimitsOp{StreamTpsLimit: &userBatchRateLimitOp{Mode: userBatchCheckinCustom, Value: common.GetPointer(12)}}, wantChanged: true},
		{name: "first text delay custom", op: &userBatchRateLimitsOp{FirstTokenDelayMs: &userBatchRateLimitOp{Mode: userBatchCheckinCustom, Value: common.GetPointer(1500)}}, wantChanged: true},
		{name: "first text delay clear", op: &userBatchRateLimitsOp{FirstTokenDelayMs: &userBatchRateLimitOp{Mode: userBatchRateLimitClear}}, wantChanged: true},
		{name: "first text delay zero", op: &userBatchRateLimitsOp{FirstTokenDelayMs: &userBatchRateLimitOp{Mode: userBatchCheckinCustom, Value: common.GetPointer(0)}}, wantErr: "首个文本延迟 限制必须在 1 到 2147483647 之间"},
		{name: "custom missing value", op: &userBatchRateLimitsOp{RpmLimit: &userBatchRateLimitOp{Mode: userBatchCheckinCustom}}, wantErr: "RPM 自定义限制需要提供数值"},
		{name: "custom zero", op: &userBatchRateLimitsOp{RpmLimit: &userBatchRateLimitOp{Mode: userBatchCheckinCustom, Value: common.GetPointer(0)}}, wantErr: "RPM 限制必须在 1 到 2147483647 之间"},
		{name: "keep with value", op: &userBatchRateLimitsOp{RpmLimit: &userBatchRateLimitOp{Mode: userBatchCheckinKeep, Value: common.GetPointer(1)}}, wantErr: "RPM 保持不变时不得提供数值"},
		{name: "clear with value", op: &userBatchRateLimitsOp{RpmLimit: &userBatchRateLimitOp{Mode: userBatchRateLimitClear, Value: common.GetPointer(1)}}, wantErr: "RPM 清除覆盖时不得提供数值"},
		{name: "unknown", op: &userBatchRateLimitsOp{RpmLimit: &userBatchRateLimitOp{Mode: "inherit"}}, wantErr: "无效的RPM限制模式：inherit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed, err := validateUserBatchRateLimitsOp(test.op)
			assert.Equal(t, test.wantChanged, changed)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
		})
	}

	partial := buildUserBatchPolicyPartial(nil, userBatchPolicyRequest{RateLimits: &userBatchRateLimitsOp{
		RpmLimit:          &userBatchRateLimitOp{Mode: userBatchCheckinCustom, Value: common.GetPointer(60)},
		ConcurrencyLimit:  &userBatchRateLimitOp{Mode: userBatchRateLimitClear},
		StreamTpsLimit:    &userBatchRateLimitOp{Mode: userBatchCheckinKeep},
		FirstTokenDelayMs: &userBatchRateLimitOp{Mode: userBatchCheckinCustom, Value: common.GetPointer(1500)},
	}})
	assert.True(t, partial.SetRpmLimit)
	require.NotNil(t, partial.RpmLimit)
	assert.Equal(t, 60, *partial.RpmLimit)
	assert.True(t, partial.SetConcurrencyLimit)
	assert.Nil(t, partial.ConcurrencyLimit)
	assert.False(t, partial.SetStreamTpsLimit)
	assert.True(t, partial.SetFirstTokenDelayMs)
	require.NotNil(t, partial.FirstTokenDelayMs)
	assert.Equal(t, 1500, *partial.FirstTokenDelayMs)
}

func TestNormalizeUserBatchIdsAllowsAtMostOneThousandUniqueUsers(t *testing.T) {
	ids := make([]int, 1000)
	for index := range ids {
		ids[index] = index + 1
	}
	normalized, err := normalizeUserBatchIds(append(append([]int(nil), ids...), ids[0]))
	require.NoError(t, err)
	assert.True(t, slices.Equal(ids, normalized))

	_, err = normalizeUserBatchIds(append(ids, 1001))
	require.EqualError(t, err, "单次批量操作最多支持 1000 个用户")
}

func TestValidateUserBatchCheckinOp(t *testing.T) {
	tests := []struct {
		name    string
		op      userBatchCheckinOp
		wantErr string
	}{
		{name: "deny only", op: userBatchCheckinOp{Mode: userBatchCheckinDeny}},
		{name: "quota follow global only", op: userBatchCheckinOp{QuotaMode: userBatchCheckinGlobal}},
		{
			name: "custom quota with valid range",
			op:   userBatchCheckinOp{Mode: userBatchCheckinAllow, QuotaMode: userBatchCheckinCustom, MinQuota: common.GetPointer(10), MaxQuota: common.GetPointer(20)},
		},
		{
			name:    "keep everything is rejected",
			op:      userBatchCheckinOp{Mode: userBatchCheckinKeep, QuotaMode: userBatchCheckinKeep},
			wantErr: "签到部分未包含任何修改",
		},
		{
			name:    "custom quota requires both bounds",
			op:      userBatchCheckinOp{QuotaMode: userBatchCheckinCustom, MinQuota: common.GetPointer(10)},
			wantErr: "自定义签到额度需要同时提供最小值和最大值",
		},
		{
			name:    "custom quota rejects inverted range",
			op:      userBatchCheckinOp{QuotaMode: userBatchCheckinCustom, MinQuota: common.GetPointer(30), MaxQuota: common.GetPointer(20)},
			wantErr: "签到最大额度不能小于最小额度",
		},
		{
			name:    "unknown mode is rejected",
			op:      userBatchCheckinOp{Mode: "sometimes"},
			wantErr: "无效的签到限制模式：sometimes",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateUserBatchCheckinOp(&test.op)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
		})
	}
}

func TestValidateUserCheckinOverride(t *testing.T) {
	assert.NoError(t, validateUserCheckinOverride(nil, nil))
	assert.NoError(t, validateUserCheckinOverride(common.GetPointer(0), common.GetPointer(0)))
	assert.Error(t, validateUserCheckinOverride(common.GetPointer(1), nil))
	assert.Error(t, validateUserCheckinOverride(common.GetPointer(-1), common.GetPointer(5)))
	assert.Error(t, validateUserCheckinOverride(common.GetPointer(1), common.GetPointer(maxUserCheckinQuotaOverride+1)))
}
