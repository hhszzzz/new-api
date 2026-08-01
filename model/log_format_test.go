package model

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0, false)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestFormatUserLogsRestrictsModelRoutingByRole(t *testing.T) {
	tests := []struct {
		name                    string
		canViewModelRouting     bool
		other                   map[string]interface{}
		expectAdminRouting      bool
		expectLegacyRouting     bool
		expectUpstreamModelName string
	}{
		{
			name:                "common user cannot view new routing fields",
			canViewModelRouting: false,
			other: map[string]interface{}{
				"model_price": 1.25,
				"admin_info": map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": "gpt-upstream",
					"po":                  []string{"set model = gpt-upstream"},
					"quota_saturation":    map[string]interface{}{"kind": "overflow"},
				},
			},
		},
		{
			name:                "common user cannot view historical routing fields",
			canViewModelRouting: false,
			other: map[string]interface{}{
				"model_price":         1.25,
				"is_model_mapped":     true,
				"upstream_model_name": "legacy-upstream",
				"po":                  []string{"set model = legacy-upstream"},
			},
		},
		{
			name:                    "admin self view receives only new routing admin info",
			canViewModelRouting:     true,
			expectAdminRouting:      true,
			expectUpstreamModelName: "gpt-upstream",
			other: map[string]interface{}{
				"model_price": 1.25,
				"admin_info": map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": "gpt-upstream",
					"po":                  []string{"set model = gpt-upstream"},
					"quota_saturation":    map[string]interface{}{"kind": "overflow"},
					"use_channel":         []string{"channel-a"},
				},
			},
		},
		{
			name:                    "admin self view retains historical routing fields",
			canViewModelRouting:     true,
			expectLegacyRouting:     true,
			expectUpstreamModelName: "legacy-upstream",
			other: map[string]interface{}{
				"model_price":         1.25,
				"is_model_mapped":     true,
				"upstream_model_name": "legacy-upstream",
				"po":                  []string{"set model = legacy-upstream"},
				"admin_info":          map[string]interface{}{"use_channel": []string{"channel-a"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := []*Log{{Other: common.MapToJsonStr(tt.other)}}

			formatUserLogs(logs, 0, tt.canViewModelRouting)

			parsed, err := common.StrToMap(logs[0].Other)
			require.NoError(t, err)
			require.Contains(t, parsed, "model_price")
			if tt.expectAdminRouting {
				adminInfo, ok := parsed["admin_info"].(map[string]interface{})
				require.True(t, ok)
				require.Equal(t, true, adminInfo["is_model_mapped"])
				require.Equal(t, tt.expectUpstreamModelName, adminInfo["upstream_model_name"])
				require.Contains(t, adminInfo, "po")
				require.Len(t, adminInfo, 3, "admin self view must not expose unrelated admin fields")
			} else {
				require.NotContains(t, parsed, "admin_info")
			}
			if tt.expectLegacyRouting {
				require.Equal(t, true, parsed["is_model_mapped"])
				require.Equal(t, tt.expectUpstreamModelName, parsed["upstream_model_name"])
			} else {
				require.NotContains(t, parsed, "is_model_mapped")
				require.NotContains(t, parsed, "upstream_model_name")
			}
			if tt.canViewModelRouting && tt.expectLegacyRouting {
				require.Contains(t, parsed, "po")
			} else {
				require.NotContains(t, parsed, "po")
			}
		})
	}
}

func TestFormatUserLogsProtectsLegacyRealtimeModelName(t *testing.T) {
	tests := []struct {
		name              string
		modelName         string
		other             map[string]interface{}
		trustedRequested  bool
		expectPublicModel string
	}{
		{
			name:      "nested routing metadata hides routed model",
			modelName: "upstream-realtime-model",
			other: map[string]interface{}{
				"admin_info": map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": "upstream-realtime-model",
				},
			},
		},
		{
			name:      "historical top-level metadata hides routed model",
			modelName: "legacy-upstream-model",
			other: map[string]interface{}{
				"is_model_mapped":     true,
				"upstream_model_name": "legacy-upstream-model",
			},
		},
		{
			name:      "historical compact model hides routed billing variant",
			modelName: "legacy-private-model-openai-compact",
			other: map[string]interface{}{
				"admin_info": map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": "legacy-private-model",
				},
			},
		},
		{
			name:              "requested model remains visible",
			modelName:         "requested-model",
			trustedRequested:  true,
			expectPublicModel: "requested-model",
			other: map[string]interface{}{
				"admin_info": map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": "upstream-model",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.trustedRequested {
				adminInfo, ok := tt.other["admin_info"].(map[string]interface{})
				require.True(t, ok)
				adminInfo["model_name_scope"] = logModelNameScopeRequested
			}
			publicLogs := []*Log{{
				ModelName: tt.modelName,
				Other:     common.MapToJsonStr(tt.other),
			}}
			adminLogs := []*Log{{
				ModelName: tt.modelName,
				Other:     common.MapToJsonStr(tt.other),
			}}

			formatUserLogs(publicLogs, 0, false)
			formatUserLogs(adminLogs, 0, true)

			require.Equal(t, tt.expectPublicModel, publicLogs[0].ModelName)
			require.Equal(t, tt.modelName, adminLogs[0].ModelName)
			parsed, err := common.StrToMap(publicLogs[0].Other)
			require.NoError(t, err)
			require.NotContains(t, parsed, "admin_info")
			require.NotContains(t, parsed, "upstream_model_name")
		})
	}
}

func TestFormatUserLogsSanitizesMappedModelFromContent(t *testing.T) {
	tests := []struct {
		name              string
		modelName         string
		upstreamModelName string
		content           string
		expectContent     string
		legacyMetadata    bool
	}{
		{
			name:              "requested model replaces upstream model",
			modelName:         "requested-model",
			upstreamModelName: "private-upstream-model",
			content:           "model private-upstream-model is unavailable; retry private-upstream-model later",
			expectContent:     "model requested-model is unavailable; retry requested-model later",
		},
		{
			name:              "unrecoverable legacy model is removed",
			modelName:         "private-upstream-model",
			upstreamModelName: "private-upstream-model",
			content:           "model private-upstream-model is unavailable",
			expectContent:     "model  is unavailable",
			legacyMetadata:    true,
		},
		{
			name:              "longer model name is not partially rewritten",
			modelName:         "requested-model",
			upstreamModelName: "gpt-4",
			content:           "gpt-4o is available but gpt-4 is unavailable",
			expectContent:     "gpt-4o is available but requested-model is unavailable",
		},
		{
			name:              "unicode text is a boundary",
			modelName:         "requested-model",
			upstreamModelName: "private-upstream",
			content:           "模型private-upstream不可用",
			expectContent:     "模型requested-model不可用",
		},
		{
			name:              "quotes are boundaries",
			modelName:         "requested-model",
			upstreamModelName: "private-upstream",
			content:           "“private-upstream”",
			expectContent:     "“requested-model”",
		},
		{
			name:              "sentence period is a boundary",
			modelName:         "requested-model",
			upstreamModelName: "private-upstream",
			content:           "private-upstream.",
			expectContent:     "requested-model.",
		},
		{
			name:              "slash-delimited path is rewritten",
			modelName:         "requested-model",
			upstreamModelName: "private-upstream",
			content:           "projects/x/models/private-upstream/operations",
			expectContent:     "projects/x/models/requested-model/operations",
		},
		{
			name:              "colon is a boundary",
			modelName:         "requested-model",
			upstreamModelName: "private-upstream",
			content:           "model private-upstream: unavailable",
			expectContent:     "model requested-model: unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			otherMap := map[string]interface{}{
				"admin_info": map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": tt.upstreamModelName,
					"model_name_scope":    logModelNameScopeRequested,
				},
			}
			if tt.legacyMetadata {
				otherMap = map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": tt.upstreamModelName,
				}
			}
			other := common.MapToJsonStr(otherMap)
			publicLogs := []*Log{{ModelName: tt.modelName, Content: tt.content, Other: other}}
			adminLogs := []*Log{{ModelName: tt.modelName, Content: tt.content, Other: other}}

			formatUserLogs(publicLogs, 0, false)
			formatUserLogs(adminLogs, 0, true)

			require.Equal(t, tt.expectContent, publicLogs[0].Content)
			require.Equal(t, tt.content, adminLogs[0].Content)
		})
	}
}

func TestFormatUserLogsFailsClosedForMappedErrorContent(t *testing.T) {
	contents := []string{
		"model PRIVATE-UPSTREAM is unavailable",
		"model private%2Dupstream is unavailable",
		"models/private-upstream:generateContent failed",
	}
	for _, content := range contents {
		publicLogs := []*Log{{
			Type:      LogTypeError,
			ModelName: "requested-model",
			Content:   content,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": "private-upstream",
				},
			}),
		}}
		adminLogs := []*Log{{
			Type:      LogTypeError,
			ModelName: "requested-model",
			Content:   content,
			Other:     publicLogs[0].Other,
		}}

		formatUserLogs(publicLogs, 0, false)
		formatUserLogs(adminLogs, 0, true)

		require.Empty(t, publicLogs[0].Content)
		require.Equal(t, content, adminLogs[0].Content)
	}
}

func TestFormatUserLogsFailsClosedForLegacyParamOverrideError(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"po":          []string{"set model = private-override-model"},
		"error_code":  "private-override-model",
		"error_type":  "private-override-model-error",
		"status_code": 404,
	})
	publicLogs := []*Log{{
		Type:      LogTypeError,
		ModelName: "requested-model",
		Content:   "model private-override-model is unavailable",
		Other:     other,
	}}
	adminLogs := []*Log{{
		Type:      LogTypeError,
		ModelName: "requested-model",
		Content:   publicLogs[0].Content,
		Other:     other,
	}}

	formatUserLogs(publicLogs, 0, false)
	formatUserLogs(adminLogs, 0, true)

	require.Empty(t, publicLogs[0].Content)
	publicOther, err := common.StrToMap(publicLogs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, publicOther, "po")
	require.NotContains(t, publicOther, "error_code")
	require.NotContains(t, publicOther, "error_type")
	require.EqualValues(t, 404, publicOther["status_code"])

	require.Equal(t, "model private-override-model is unavailable", adminLogs[0].Content)
	adminOther, err := common.StrToMap(adminLogs[0].Other)
	require.NoError(t, err)
	require.Contains(t, adminOther, "po")
	require.Equal(t, "private-override-model", adminOther["error_code"])
	require.Equal(t, "private-override-model-error", adminOther["error_type"])
}

func TestFormatUserLogsRequiresTrustedModelNameProvenance(t *testing.T) {
	tests := []struct {
		name  string
		other string
	}{
		{name: "empty metadata", other: ""},
		{name: "empty object", other: `{}`},
		{name: "malformed metadata", other: `{not-json`},
		{name: "historical nonempty metadata", other: `{"model_ratio":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publicLogs := []*Log{{
				Type:      LogTypeError,
				ModelName: "possibly-routed-model",
				Content:   "historical upstream diagnostic",
				Other:     tt.other,
			}}
			adminLogs := []*Log{{
				Type:      LogTypeError,
				ModelName: "possibly-routed-model",
				Content:   "historical upstream diagnostic",
				Other:     tt.other,
			}}

			formatUserLogs(publicLogs, 0, false)
			formatUserLogs(adminLogs, 0, true)

			require.Empty(t, publicLogs[0].ModelName)
			require.Empty(t, publicLogs[0].Content)
			require.Equal(t, "possibly-routed-model", adminLogs[0].ModelName)
			require.Equal(t, "historical upstream diagnostic", adminLogs[0].Content)
		})
	}

	trustedLogs := []*Log{{
		ModelName: "requested-model",
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"model_name_scope": logModelNameScopeRequested,
			},
		}),
	}}
	formatUserLogs(trustedLogs, 0, false)
	require.Equal(t, "requested-model", trustedLogs[0].ModelName)
}

func TestFormatUserLogsFailsClosedForHistoricalErrorWithoutRoutingMetadata(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{"status_code": 503})
	publicLogs := []*Log{{
		Type:      LogTypeError,
		ModelName: "possibly-routed-model",
		Content:   "upstream diagnostic with an unknown model identity",
		Other:     other,
	}}
	adminLogs := []*Log{{
		Type:      LogTypeError,
		ModelName: "possibly-routed-model",
		Content:   publicLogs[0].Content,
		Other:     other,
	}}

	formatUserLogs(publicLogs, 0, false)
	formatUserLogs(adminLogs, 0, true)

	require.Empty(t, publicLogs[0].ModelName)
	require.Empty(t, publicLogs[0].Content)
	require.Equal(t, "possibly-routed-model", adminLogs[0].ModelName)
	require.Equal(t, "upstream diagnostic with an unknown model identity", adminLogs[0].Content)
}

func TestFormatUserLogsPreservesTrustedUnroutedErrorDiagnostics(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"status_code": 429,
		"error_code":  "rate_limit_exceeded",
		"error_type":  "requests",
		"admin_info": map[string]interface{}{
			"model_name_scope":      logModelNameScopeRequested,
			"model_routing_checked": true,
		},
	})
	logs := []*Log{{
		Type:      LogTypeError,
		ModelName: "requested-model",
		Content:   "request rate limit exceeded",
		Other:     other,
	}}

	formatUserLogs(logs, 0, false)

	require.Equal(t, "requested-model", logs[0].ModelName)
	require.Equal(t, "request rate limit exceeded", logs[0].Content)
	publicOther, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "rate_limit_exceeded", publicOther["error_code"])
	require.Equal(t, "requests", publicOther["error_type"])
	require.NotContains(t, publicOther, "admin_info")
}

func TestFormatUserLogsSanitizesTrustedRoutedErrorDiagnostics(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"status_code": 404,
		"error_code":  "private-upstream-model",
		"error_type":  "private-upstream-model-error",
		"admin_info": map[string]interface{}{
			"model_name_scope":      logModelNameScopeRequested,
			"model_routing_checked": true,
			"is_model_mapped":       true,
			"upstream_model_name":   "private-upstream-model",
		},
	})
	logs := []*Log{{
		Type:      LogTypeError,
		ModelName: "requested-model",
		Content:   "model private-upstream-model: unavailable",
		Other:     other,
	}}

	formatUserLogs(logs, 0, false)

	require.Equal(t, "model requested-model: unavailable", logs[0].Content)
	publicOther, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "requested-model", publicOther["error_code"])
	require.NotContains(t, publicOther, "error_type", "embedded route names must fail closed")
}

func TestFormatUserLogsKeepsSanitizedStreamStatusForOwner(t *testing.T) {
	logs := []*Log{{
		ModelName: "requested-model",
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"model_name_scope":      logModelNameScopeRequested,
				"model_routing_checked": true,
				"is_model_mapped":       true,
				"upstream_model_name":   "private-upstream-model",
			},
			"stream_status": map[string]interface{}{
				"status":      "error",
				"end_reason":  "timeout",
				"error_count": 2,
				"end_error":   "model private-upstream-model timed out",
				"errors": []string{
					"private-upstream-model frame failed",
					"safe retry warning",
				},
			},
		}),
	}}

	formatUserLogs(logs, 0, false)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, other, "admin_info")
	streamStatus, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "error", streamStatus["status"])
	require.Equal(t, "timeout", streamStatus["end_reason"])
	require.EqualValues(t, 2, streamStatus["error_count"])
	require.Equal(t, "model requested-model timed out", streamStatus["end_error"])
	require.Equal(t, []interface{}{
		"requested-model frame failed",
		"safe retry warning",
	}, streamStatus["errors"])
}

func TestFormatUserLogsKeepsStatusButDropsUntrustedStreamErrors(t *testing.T) {
	logs := []*Log{{
		Other: common.MapToJsonStr(map[string]interface{}{
			"stream_status": map[string]interface{}{
				"status":      "error",
				"end_reason":  "scanner_error",
				"error_count": 1,
				"end_error":   "unknown upstream detail",
				"errors":      []string{"unknown upstream frame"},
			},
		}),
	}}

	formatUserLogs(logs, 0, false)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	streamStatus, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "error", streamStatus["status"])
	require.Equal(t, "scanner_error", streamStatus["end_reason"])
	require.EqualValues(t, 1, streamStatus["error_count"])
	require.NotContains(t, streamStatus, "end_error")
	require.NotContains(t, streamStatus, "errors")
}

func TestFormatUserLogsDoesNotMistakeLongerModelForRoutedErrorModel(t *testing.T) {
	logs := []*Log{{
		Type:      LogTypeError,
		ModelName: "requested-model",
		Content:   "gpt-4o and gpt-4.1 are available but gpt-4 is unavailable",
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"model_name_scope":      logModelNameScopeRequested,
				"model_routing_checked": true,
				"is_model_mapped":       true,
				"upstream_model_name":   "gpt-4",
			},
		}),
	}}

	formatUserLogs(logs, 0, false)

	require.Equal(t, "gpt-4o and gpt-4.1 are available but requested-model is unavailable", logs[0].Content)
}

func TestFormatUserLogsAllowsRequestedModelContainingUpstreamName(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "upstream identity becomes public identity",
			content:  "model gpt-4 is unavailable",
			expected: "model openai/gpt-4 is unavailable",
		},
		{
			name:     "existing public identity remains idempotent",
			content:  "model openai/gpt-4 is unavailable",
			expected: "model openai/gpt-4 is unavailable",
		},
		{
			name:     "mixed public and upstream identities",
			content:  "model openai/gpt-4 is unavailable; upstream gpt-4 failed",
			expected: "model openai/gpt-4 is unavailable; upstream openai/gpt-4 failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := []*Log{{
				Type:      LogTypeError,
				ModelName: "openai/gpt-4",
				Content:   tt.content,
				Other: common.MapToJsonStr(map[string]interface{}{
					"admin_info": map[string]interface{}{
						"model_name_scope":      logModelNameScopeRequested,
						"model_routing_checked": true,
						"is_model_mapped":       true,
						"upstream_model_name":   "gpt-4",
					},
				}),
			}}

			formatUserLogs(logs, 0, false)

			require.Equal(t, tt.expected, logs[0].Content)
		})
	}
}

func TestFormatUserLogsFailsClosedForObfuscatedRoutedErrorModel(t *testing.T) {
	tests := []string{
		"model PRIVATE-UPSTREAM is unavailable",
		"private-upstream_not_found",
		"model private%2Dupstream is unavailable",
		"model private&#45;upstream is unavailable",
		"model private&#37;2Dupstream is unavailable",
		"model private%26%2337%3B2Dupstream is unavailable",
		"model private&#92;u002dupstream is unavailable",
		`model private\u002dupstream is unavailable`,
	}
	for _, content := range tests {
		logs := []*Log{{
			Type:      LogTypeError,
			ModelName: "requested-model",
			Content:   content,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"model_name_scope":      logModelNameScopeRequested,
					"model_routing_checked": true,
					"is_model_mapped":       true,
					"upstream_model_name":   "private-upstream",
				},
			}),
		}}

		formatUserLogs(logs, 0, false)
		require.Empty(t, logs[0].Content)
	}
}

func TestFormatUserLogsRejectsRoutedModelInDottedErrorPath(t *testing.T) {
	logs := []*Log{{
		Type:      LogTypeError,
		ModelName: "requested-model",
		Content:   "api.v1.gpt-4.not_found",
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"model_name_scope":      logModelNameScopeRequested,
				"model_routing_checked": true,
				"is_model_mapped":       true,
				"upstream_model_name":   "gpt-4",
			},
		}),
	}}

	formatUserLogs(logs, 0, false)

	require.Empty(t, logs[0].Content)
}

func TestModelLogWritersMarkRequestedModelNameProvenance(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       41,
		Username: "log-scope-user",
		Status:   common.UserStatusEnabled,
	}).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("username", "log-scope-user")

	RecordConsumeLog(ctx, 41, RecordConsumeLogParams{
		ModelName: "requested-consume-model",
		Quota:     1,
		Other:     map[string]interface{}{"model_ratio": 1},
	})
	RecordErrorLog(ctx, 41, 1, "requested-error-model", "", "diagnostic", 0, 0, false, "default", map[string]interface{}{"status_code": 500})
	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId:    41,
		LogType:   LogTypeRefund,
		ModelName: "requested-task-model",
		Other:     map[string]interface{}{"is_task": true},
	})

	var logs []*Log
	require.NoError(t, LOG_DB.Where("user_id = ?", 41).Order("id asc").Find(&logs).Error)
	require.Len(t, logs, 3)
	for _, log := range logs {
		other, err := common.StrToMap(log.Other)
		require.NoError(t, err)
		require.True(t, hasRequestedModelNameScope(other))
	}

	formatUserLogs(logs, 0, false)
	require.Equal(t, "requested-consume-model", logs[0].ModelName)
	require.Equal(t, "requested-error-model", logs[1].ModelName)
	require.Equal(t, "requested-task-model", logs[2].ModelName)
}

func TestUserLogModelFiltersUsePublicModelIdentity(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	trustedPublicOther := common.MapToJsonStr(map[string]interface{}{
		"model_ratio": 1,
		"admin_info": map[string]interface{}{
			"model_name_scope": logModelNameScopeRequested,
		},
	})
	require.NoError(t, DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "public-model",
		Quota:            10,
		PromptTokens:     2,
		CompletionTokens: 3,
		Other:            trustedPublicOther,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "private-model",
		Quota:            99,
		PromptTokens:     20,
		CompletionTokens: 30,
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"is_model_mapped":     true,
				"upstream_model_name": "private-model",
			},
		}),
	}).Error)

	// These historical rows represent requests for another model routed to a
	// raw upstream name that now collides with a public catalog model. The
	// former 10k candidate path could leak their existence through total/stat.
	legacyCollisionLogs := make([]Log, 10001)
	for i := range legacyCollisionLogs {
		legacyCollisionLogs[i] = Log{
			UserId:           1,
			Username:         "alice",
			CreatedAt:        now,
			Type:             LogTypeConsume,
			ModelName:        "public-model",
			Quota:            1,
			PromptTokens:     1,
			CompletionTokens: 1,
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"is_model_mapped":     true,
					"upstream_model_name": "public-model",
				},
			}),
		}
	}
	require.NoError(t, DB.CreateInBatches(&legacyCollisionLogs, 500).Error)
	require.NoError(t, DB.Create(&Log{
		UserId:    1,
		Username:  "alice",
		CreatedAt: now,
		Type:      LogTypeConsume,
		ModelName: "spoof-public-model",
		Quota:     1000,
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"modelXnameYscope": logModelNameScopeRequested,
			},
		}),
	}).Error)

	publicExact, total, err := GetUserLogs(1, LogTypeConsume, 0, 0, "private-model", "", 0, 10, "", "", "", false)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, publicExact)

	publicExact, total, err = GetUserLogs(1, LogTypeConsume, 0, 0, "public-model", "", 0, 10, "", "", "", false)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, publicExact, 1)
	require.Equal(t, "public-model", publicExact[0].ModelName)

	spoofedExact, total, err := GetUserLogs(1, LogTypeConsume, 0, 0, "spoof-public-model", "", 0, 10, "", "", "", false)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, spoofedExact)

	_, _, err = GetUserLogs(1, LogTypeConsume, 0, 0, "%model", "", 0, 1, "", "", "", false)
	require.EqualError(t, err, "model filter is not available")

	adminExact, total, err := GetUserLogs(1, LogTypeConsume, 0, 0, "private-model", "", 0, 10, "", "", "", true)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminExact, 1)
	require.Equal(t, "private-model", adminExact[0].ModelName)

	publicPrivateStat, err := SumUserUsedQuota(1, now-1, now+1, "private-model", "", 0, "")
	require.NoError(t, err)
	require.Equal(t, Stat{}, publicPrivateStat)

	publicExactStat, err := SumUserUsedQuota(1, now-1, now+1, "public-model", "", 0, "")
	require.NoError(t, err)
	require.Equal(t, Stat{Quota: 10, Rpm: 1, Tpm: 5}, publicExactStat)

	_, err = SumUserUsedQuota(1, now-1, now+1, "%model", "", 0, "")
	require.EqualError(t, err, "model filter is not available")

	adminPrivateStat, err := SumUsedQuota(LogTypeConsume, now-1, now+1, "private-model", "alice", "", 0, "")
	require.NoError(t, err)
	require.Equal(t, Stat{Quota: 99, Rpm: 1, Tpm: 50}, adminPrivateStat)
}
