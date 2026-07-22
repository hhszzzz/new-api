package service

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostWssConsumeQuotaRecordsPerformanceOutcome(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000)
	seedChannel(t, 1)
	require.NoError(t, model.DB.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricInstance{}))

	previousSetting := perf_metrics_setting.GetSetting()
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"perf_metrics_setting.enabled":     "true",
		"perf_metrics_setting.bucket_time": "hour",
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"perf_metrics_setting.enabled":     strconv.FormatBool(previousSetting.Enabled),
			"perf_metrics_setting.bucket_time": previousSetting.BucketTime,
		}))
	})

	modelNames := []string{
		"wss-success-performance-test-model",
		"wss-zero-token-performance-test-model",
		"wss-mapped-requested-performance-test-model",
		"wss-mapped-private-performance-test-model",
	}
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("model_name IN ?", modelNames).Delete(&model.PerfMetricInstance{}).Error)
		require.NoError(t, model.DB.Where("model_name IN ?", modelNames).Delete(&model.PerfMetric{}).Error)
	})

	tests := []struct {
		name             string
		modelName        string
		upstreamModel    string
		usage            dto.RealtimeUsage
		successRate      float64
		expectThroughput bool
	}{
		{
			name:      "successful request",
			modelName: modelNames[0],
			usage: dto.RealtimeUsage{
				TotalTokens:  100,
				InputTokens:  60,
				OutputTokens: 40,
				InputTokenDetails: dto.InputTokenDetails{
					TextTokens: 60,
				},
				OutputTokenDetails: dto.OutputTokenDetails{
					TextTokens: 40,
				},
			},
			successRate:      100,
			expectThroughput: true,
		},
		{
			name:        "zero-token upstream failure",
			modelName:   modelNames[1],
			usage:       dto.RealtimeUsage{},
			successRate: 0,
		},
		{
			name:          "mapped request keeps public performance identity",
			modelName:     modelNames[2],
			upstreamModel: modelNames[3],
			usage: dto.RealtimeUsage{
				TotalTokens:  100,
				InputTokens:  60,
				OutputTokens: 40,
				InputTokenDetails: dto.InputTokenDetails{
					TextTokens: 60,
				},
				OutputTokenDetails: dto.OutputTokenDetails{
					TextTokens: 40,
				},
			},
			successRate:      100,
			expectThroughput: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before, err := perfmetrics.QuerySummaryAll(1, []string{"default"})
			require.NoError(t, err)
			var beforeRequestCount int64
			for _, summary := range before.Models {
				if summary.ModelName == test.modelName {
					beforeRequestCount = summary.RequestCount
					break
				}
			}

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
			ctx.Set("username", "test_user")
			ctx.Set("token_name", "test_token")
			now := time.Now()
			relayInfo := &relaycommon.RelayInfo{
				UserId:            1,
				UsingGroup:        "default",
				OriginModelName:   test.modelName,
				StartTime:         now.Add(-2 * time.Second),
				FirstResponseTime: now.Add(-1500 * time.Millisecond),
				IsStream:          true,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:         1,
					IsModelMapped:     test.upstreamModel != "",
					UpstreamModelName: test.upstreamModel,
				},
				PriceData: types.PriceData{
					ModelRatio: 1,
					GroupRatioInfo: types.GroupRatioInfo{
						GroupRatio: 1,
					},
				},
			}

			PostWssConsumeQuota(ctx, relayInfo, &test.usage, "")

			var requestCount int64
			require.Eventually(t, func() bool {
				summaryResult, err := perfmetrics.QuerySummaryAll(1, []string{"default"})
				if err != nil {
					return false
				}
				requestCount = 0
				for _, summary := range summaryResult.Models {
					if summary.ModelName == test.modelName {
						requestCount = summary.RequestCount
						break
					}
				}
				return requestCount == beforeRequestCount+1
			}, time.Second, 10*time.Millisecond)
			assert.Equal(t, beforeRequestCount+1, requestCount)

			result, err := perfmetrics.Query(perfmetrics.QueryParams{
				Model: test.modelName,
				Group: "default",
				Hours: 1,
			})
			require.NoError(t, err)
			require.Len(t, result.Groups, 1)
			assert.Equal(t, test.modelName, result.ModelName)
			assert.Equal(t, "default", result.Groups[0].Group)
			assert.Equal(t, test.successRate, result.Groups[0].SuccessRate)
			assert.Positive(t, result.Groups[0].AvgLatencyMs)
			assert.Positive(t, result.Groups[0].AvgTtftMs)
			if test.expectThroughput {
				assert.Positive(t, result.Groups[0].AvgTps)
			} else {
				assert.Zero(t, result.Groups[0].AvgTps)
			}
			if test.upstreamModel != "" {
				upstreamResult, err := perfmetrics.Query(perfmetrics.QueryParams{
					Model: test.upstreamModel,
					Group: "default",
					Hours: 1,
				})
				require.NoError(t, err)
				assert.Empty(t, upstreamResult.Groups)
			}
		})
	}
}
