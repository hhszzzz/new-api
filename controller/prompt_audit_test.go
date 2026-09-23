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

// TestPromptAuditListFilterParsesCollapseAndGroup covers the query contract the
// records screen depends on: the collapse switch is a display mode, so it must
// never turn into a predicate, and it must survive the request-body path that the
// deletion preview shares.
func TestPromptAuditListFilterParsesCollapseAndGroup(t *testing.T) {
	parse := func(query string) model.PromptAuditFilter {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodGet, "/api/prompt_audit"+query, nil)
		return promptAuditFilterFromQuery(context)
	}

	assert.True(t, parse("?collapse_repeats=true").CollapseRepeats)
	assert.False(t, parse("").CollapseRepeats)
	// An unparsable value must neither collapse nor widen the listing.
	assert.False(t, parse("?collapse_repeats=maybe").CollapseRepeats)
	assert.Equal(t, "guarded-model", parse("?collapse_repeats=true&model=guarded-model").Model)
	assert.True(t, promptAuditFilterRequest{CollapseRepeats: true, Model: "guarded-model"}.toModel().CollapseRepeats)
}

func TestListPromptAuditsCollapsedListingAndGroupExpansion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.PromptAudit{}))
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		_ = sqlDB.Close()
	})

	// One audited text resent twice with a mixed outcome — the first request
	// passed and the second was blocked — plus a hashless row that must stay a
	// group of its own.
	hash := strings.Repeat("c", 64)
	for index, outcome := range []struct{ action, decision string }{
		{"allow", "pass"},
		{"block", "block"},
	} {
		require.NoError(t, model.CreatePromptAudit(&model.PromptAudit{
			RequestID: fmt.Sprintf("collapsed-%d", index), UserID: 5, Username: "collapsed-user",
			ModelName: "guarded-model", Status: model.PromptAuditStatusDone, PromptHash: hash,
			Action: outcome.action, Decision: outcome.decision, CreatedAt: 2000 + int64(index),
		}))
	}
	require.NoError(t, model.CreatePromptAudit(&model.PromptAudit{
		RequestID: "collapsed-other", UserID: 5, Username: "collapsed-user", ModelName: "guarded-model",
		Status: model.PromptAuditStatusDone, Action: "allow", Decision: "pass", CreatedAt: 2100,
	}))

	type responseBody struct {
		Success bool `json:"success"`
		Data    struct {
			Items        []model.PromptAuditResponse `json:"items"`
			Total        int64                       `json:"total"`
			RecordsTotal int64                       `json:"records_total"`
			Page         int                         `json:"page"`
			PageSize     int                         `json:"page_size"`
		} `json:"data"`
	}
	list := func(query string) (responseBody, string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/prompt_audit?"+query, nil)
		ListPromptAudits(context)
		require.Equal(t, http.StatusOK, recorder.Code)
		var body responseBody
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
		require.True(t, body.Success)
		return body, recorder.Body.String()
	}

	collapsed, _ := list("collapse_repeats=true&username=collapsed-user")
	assert.EqualValues(t, 2, collapsed.Data.Total)
	assert.EqualValues(t, 3, collapsed.Data.RecordsTotal)
	require.Len(t, collapsed.Data.Items, 2)

	var merged, single *model.PromptAuditResponse
	for index := range collapsed.Data.Items {
		if collapsed.Data.Items[index].PromptHash == hash {
			merged = &collapsed.Data.Items[index]
		} else {
			single = &collapsed.Data.Items[index]
		}
	}
	require.NotNil(t, merged)
	require.NotNil(t, single)

	// The merged row reports what its whole group decided, not the verdict of the
	// request that happens to represent it: this one was let through, but the text
	// was blocked when it was resent.
	require.NotNil(t, merged.Repeat)
	assert.EqualValues(t, 2, merged.Repeat.Count)
	assert.EqualValues(t, 2000, merged.Repeat.FirstAt)
	assert.EqualValues(t, 2001, merged.Repeat.LastAt)
	assert.Equal(t, "block", merged.Decision)
	assert.Equal(t, "block", merged.Action)
	assert.Equal(t, "block", merged.Repeat.WorstAction)
	assert.EqualValues(t, 1, merged.Repeat.Blocks)
	assert.EqualValues(t, 0, merged.Repeat.Unavailable)

	// A row that stands alone keeps its own verdict.
	require.NotNil(t, single.Repeat)
	assert.EqualValues(t, 1, single.Repeat.Count)
	assert.Equal(t, "allow", single.Repeat.WorstAction)
	assert.Equal(t, "pass", single.Decision)
	assert.Equal(t, "allow", single.Action)

	// The plain listing is untouched, and only the collapsed one carries the
	// record count the screen pairs with its group count.
	plain, plainBody := list("username=collapsed-user")
	assert.EqualValues(t, 3, plain.Data.Total)
	require.Len(t, plain.Data.Items, 3)
	assert.Nil(t, plain.Data.Items[0].Repeat)
	assert.NotContains(t, plainBody, "records_total")

	// A collapsed row expands into exactly the requests it counted.
	expanded, _ := list(fmt.Sprintf("collapse_repeats=true&group_id=%d&username=collapsed-user", merged.ID))
	require.Len(t, expanded.Data.Items, 2)
	assert.Equal(t, "collapsed-1", expanded.Data.Items[0].RequestID)
	assert.Equal(t, "collapsed-0", expanded.Data.Items[1].RequestID)

	// A group id the filter no longer covers expands to nothing rather than to
	// rows the operator filtered away.
	hidden, _ := list(fmt.Sprintf("collapse_repeats=true&group_id=%d&username=collapsed-user&model=another-model", merged.ID))
	assert.Empty(t, hidden.Data.Items)
}
