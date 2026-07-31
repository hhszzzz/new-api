package controller

import (
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
