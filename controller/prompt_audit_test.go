package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPromptAuditEndpointTokensAreWriteOnlyAndSurviveIDRename(t *testing.T) {
	current := []prompt_audit_setting.Endpoint{{
		ID: "old-id", BaseURL: "https://guard.example.com", Token: "top-secret-token",
	}}
	updates := []promptAuditEndpointUpdate{{
		ID: "new-id", OriginalID: "old-id", Name: "renamed", BaseURL: "https://guard.example.com",
		Model: "guard", TimeoutMS: 1000, InputLimit: 4000, Concurrency: 2, Enabled: true,
	}}
	merged := mergePromptAuditEndpointUpdates(current, updates)
	require.Len(t, merged, 1)
	assert.Equal(t, "new-id", merged[0].ID)
	assert.Equal(t, "top-secret-token", merged[0].Token)

	response := promptAuditConfigResponse(prompt_audit_setting.PromptAuditSetting{Endpoints: merged})
	encoded, err := common.Marshal(response)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "top-secret-token")
	assert.Contains(t, string(encoded), `"has_token":true`)
	assert.Contains(t, string(encoded), `"enabled_categories":[]`)
	assert.Contains(t, string(encoded), `"groups":[]`)

	clear := ""
	updates[0].Token = &clear
	cleared := mergePromptAuditEndpointUpdates(current, updates)
	require.Len(t, cleared, 1)
	assert.Empty(t, cleared[0].Token)
}

func TestPromptAuditEndpointURLChangeCannotForwardStoredToken(t *testing.T) {
	current := []prompt_audit_setting.Endpoint{{
		ID: "primary", BaseURL: "https://guard.example.com/v1", Token: "top-secret-token",
	}}
	updates := []promptAuditEndpointUpdate{{
		ID: "primary", Name: "redirected", BaseURL: "https://attacker.example.com/v1",
		Model: "guard", TimeoutMS: 1000, InputLimit: 4000, Concurrency: 2, Enabled: true,
	}}

	merged := mergePromptAuditEndpointUpdates(current, updates)
	require.Len(t, merged, 1)
	assert.Empty(t, merged[0].Token)

	replacement := "new-explicit-token"
	updates[0].Token = &replacement
	merged = mergePromptAuditEndpointUpdates(current, updates)
	require.Len(t, merged, 1)
	assert.Equal(t, replacement, merged[0].Token)
}

// TestPromptAuditRetiredFiltersAreRefused covers the filters the records screen
// no longer offers. A client that still sends one — a page loaded before the
// upgrade, a script — must be refused: run with the filter dropped, a preview
// and the deletion that trusts it would cover every user's records.
func TestPromptAuditRetiredFiltersAreRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name    string
		handler gin.HandlerFunc
		method  string
		target  string
		body    string
	}{
		{name: "listing", handler: ListPromptAudits, method: http.MethodGet, target: "/api/prompt-audit/events?user_id=42"},
		{name: "statistics", handler: GetPromptAuditStats, method: http.MethodGet, target: "/api/prompt-audit/stats?prompt_hash=abc"},
		{name: "deletion preview", handler: PreviewDeletePromptAudits, method: http.MethodPost, target: "/api/prompt-audit/events/delete-preview", body: `{"filter":{"user_id":42}}`},
		{name: "deletion", handler: DeletePromptAudits, method: http.MethodDelete, target: "/api/prompt-audit/events", body: `{"filter":{"endpoint_id":"guard"},"expected_count":1,"max_id":9}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(testCase.method, testCase.target, strings.NewReader(testCase.body))
			testCase.handler(context)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "no longer supported")
		})
	}

	// The filters the screen does offer parse from both paths, the detector
	// among them, and the collapse switch never becomes a predicate.
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/api/prompt-audit/events?collapse_repeats=true&model=guarded-model&detector=wordlist", nil)
	filter, retired := promptAuditFilterFromQuery(context)
	assert.Empty(t, retired)
	assert.Equal(t, model.PromptAuditFilter{Model: "guarded-model", Detector: "wordlist"}, filter)
	assert.Equal(t, model.PromptAuditFilter{GroupIDs: []int64{3}, Detector: "model", Username: "alice"},
		promptAuditFilterRequest{GroupIDs: []int64{3}, Detector: " model ", Username: "alice"}.toModel())
}

// TestListPromptAuditsCollapsedListingAndGroupExpansion covers what the
// controller adds to the model's grouping: which decision a collapsed row
// reports and the response shape the records screen reads.
func TestListPromptAuditsCollapsedListingAndGroupExpansion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.PromptAudit{}, &model.User{}))
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		_ = sqlDB.Close()
	})

	// One audited text resent twice under async observation: the first request
	// passed, the second was judged a block but — as async observation does —
	// only marked. A single async block stands alone next to them.
	hash := strings.Repeat("c", 64)
	for index, outcome := range []struct{ action, decision string }{
		{"allow", "pass"},
		{"mark", "block"},
	} {
		require.NoError(t, model.CreatePromptAudit(&model.PromptAudit{
			RequestID: fmt.Sprintf("collapsed-%d", index), UserID: 5, Username: "collapsed-user",
			ModelName: "guarded-model", Status: model.PromptAuditStatusDone, PromptHash: hash,
			Action: outcome.action, Decision: outcome.decision, CreatedAt: 2000 + int64(index),
		}))
	}
	require.NoError(t, model.CreatePromptAudit(&model.PromptAudit{
		RequestID: "collapsed-other", UserID: 5, Username: "collapsed-user", ModelName: "guarded-model",
		Status: model.PromptAuditStatusDone, PromptHash: strings.Repeat("d", 64), Action: "mark", Decision: "block", CreatedAt: 2100,
	}))

	type responseBody struct {
		Success bool `json:"success"`
		Data    struct {
			Items    []model.PromptAuditResponse `json:"items"`
			Total    int64                       `json:"total"`
			Page     int                         `json:"page"`
			PageSize int                         `json:"page_size"`
		} `json:"data"`
	}
	list := func(query string) (responseBody, string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/prompt-audit/events?"+query, nil)
		ListPromptAudits(context)
		require.Equal(t, http.StatusOK, recorder.Code)
		var body responseBody
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
		require.True(t, body.Success)
		return body, recorder.Body.String()
	}

	collapsed, collapsedBody := list("collapse_repeats=true&username=collapsed-user")
	assert.EqualValues(t, 2, collapsed.Data.Total)
	assert.NotContains(t, collapsedBody, "records_total")
	require.Len(t, collapsed.Data.Items, 2)
	merged, single := collapsed.Data.Items[1], collapsed.Data.Items[0]
	require.Equal(t, hash, merged.PromptHash)

	// The merged row reports the most severe decision its group holds — the
	// observed block — while its own action and its request stay the
	// representative's: nothing in the group was actually refused.
	require.NotNil(t, merged.Repeat)
	assert.EqualValues(t, 2, merged.Repeat.Count)
	assert.Equal(t, "block", merged.Repeat.WorstDecision)
	assert.Equal(t, "block", merged.Decision)
	assert.Equal(t, "allow", merged.Action)
	assert.Equal(t, "collapsed-0", merged.RequestID)
	assert.Zero(t, merged.Repeat.Blocks)

	// A row that stands alone keeps its own verdict: an observed block stays a
	// block rather than being read back from its mark action.
	require.NotNil(t, single.Repeat)
	assert.EqualValues(t, 1, single.Repeat.Count)
	assert.Equal(t, "block", single.Decision)
	assert.Equal(t, "mark", single.Action)

	// The plain listing is untouched.
	plain, _ := list("username=collapsed-user")
	assert.EqualValues(t, 3, plain.Data.Total)
	require.Len(t, plain.Data.Items, 3)
	assert.Nil(t, plain.Data.Items[0].Repeat)

	// A collapsed row expands into exactly the requests it counted, and total
	// counts the whole group.
	expanded, _ := list(fmt.Sprintf("collapse_repeats=true&group_id=%d&username=collapsed-user", merged.ID))
	require.Len(t, expanded.Data.Items, 2)
	assert.EqualValues(t, 2, expanded.Data.Total)
	assert.Equal(t, "collapsed-1", expanded.Data.Items[0].RequestID)
	assert.Equal(t, "collapsed-0", expanded.Data.Items[1].RequestID)

	// A representative that is gone expands to nothing instead of failing.
	gone, _ := list(fmt.Sprintf("collapse_repeats=true&group_id=%d&username=collapsed-user", merged.ID+1000))
	assert.Empty(t, gone.Data.Items)
}
