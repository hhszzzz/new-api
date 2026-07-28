package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type realtimeBillingRecorder struct {
	preConsumed int
	reserves    []int
	settles     []int
}

func (r *realtimeBillingRecorder) Settle(actualQuota int) error {
	r.settles = append(r.settles, actualQuota)
	return nil
}

func (r *realtimeBillingRecorder) Refund(*gin.Context) {}

func (r *realtimeBillingRecorder) NeedsRefund() bool { return false }

func (r *realtimeBillingRecorder) GetPreConsumedQuota() int { return r.preConsumed }

func (r *realtimeBillingRecorder) Reserve(targetQuota int) error {
	r.reserves = append(r.reserves, targetQuota)
	if targetQuota > r.preConsumed {
		r.preConsumed = targetQuota
	}
	return nil
}

func TestPreWssConsumeQuotaUsesFrozenAudioRatios(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recorder := &realtimeBillingRecorder{preConsumed: 50}
	relayInfo := &relaycommon.RelayInfo{
		Billing: recorder,
		PriceData: types.PriceData{
			ModelRatio:           2,
			CompletionRatio:      3,
			AudioRatio:           4,
			AudioCompletionRatio: 5,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 0.5},
		},
	}
	usage := &dto.RealtimeUsage{
		TotalTokens:  37,
		InputTokens:  15,
		OutputTokens: 22,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens:  10,
			AudioTokens: 5,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens:  20,
			AudioTokens: 2,
		},
	}

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, usage))

	// (10 + 20*3 + 5*4 + 2*4*5) * 2 * 0.5 = 130.
	assert.Equal(t, []int{130}, recorder.reserves)
	assert.Empty(t, recorder.settles)
}

func TestPreWssConsumeQuotaUsesFrozenQuotaPerUnitForFixedPrice(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	previousQuotaPerUnit := common.GetQuotaPerUnit()
	require.NoError(t, common.SetQuotaPerUnit(10_000))
	t.Cleanup(func() { require.NoError(t, common.SetQuotaPerUnit(previousQuotaPerUnit)) })

	recorder := &realtimeBillingRecorder{}
	relayInfo := &relaycommon.RelayInfo{
		Billing: recorder,
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.25,
			QuotaPerUnit:   100,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2},
		},
	}

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{TotalTokens: 1}))

	assert.Equal(t, []int{50}, recorder.reserves)
	assert.Empty(t, recorder.settles)
}

func TestPreWssConsumeQuotaUsesTieredSnapshot(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	expr := `tier("base", p + c * 2 + cr * 0.5 + ai * 3 + ao * 4)`
	recorder := &realtimeBillingRecorder{}
	relayInfo := &relaycommon.RelayInfo{
		Billing: recorder,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   expr,
			ExprHash:     billingexpr.ExprHashString(expr),
			GroupRatio:   1,
			QuotaPerUnit: 1_000_000,
			ExprVersion:  1,
		},
	}
	usage := &dto.RealtimeUsage{
		TotalTokens:  150,
		InputTokens:  100,
		OutputTokens: 50,
		InputTokenDetails: dto.InputTokenDetails{
			CachedTokens: 20,
			AudioTokens:  10,
		},
		OutputTokenDetails: dto.OutputTokenDetails{AudioTokens: 5},
	}

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, usage))

	// p=70, c=45 after separately priced audio/cache categories are removed.
	assert.Equal(t, []int{220}, recorder.reserves)
	assert.Empty(t, recorder.settles)
}
