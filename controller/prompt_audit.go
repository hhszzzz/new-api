package controller

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	globalsetting "github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"

	"github.com/gin-gonic/gin"
)

type promptAuditEndpointUpdate struct {
	ID          string   `json:"id"`
	OriginalID  string   `json:"original_id"`
	Name        string   `json:"name"`
	BaseURL     string   `json:"base_url"`
	Token       *string  `json:"token"`
	Model       string   `json:"model"`
	TimeoutMS   int      `json:"timeout_ms"`
	InputLimit  int      `json:"input_limit"`
	Concurrency int      `json:"concurrency"`
	Enabled     bool     `json:"enabled"`
	Purpose     string   `json:"purpose"`
	Directions  []string `json:"directions"`
}

type promptAuditConfigUpdate struct {
	ScopePolicies        *map[dto.PromptAuditScope]prompt_audit_setting.ScopePolicy `json:"scope_policies"`
	WordFilterEnabled    *bool                                                      `json:"word_filter_enabled"`
	Mode                 *string                                                    `json:"mode"`
	OutputMode           *string                                                    `json:"output_mode"`
	ManualWordlistAction *string                                                    `json:"manual_wordlist_action"`
	EnabledCategories    *[]string                                                  `json:"enabled_categories"`
	ControversialBlocks  *[]string                                                  `json:"controversial_block_categories"`
	ReviewEnabled        *bool                                                      `json:"review_enabled"`
	ReviewPrompt         *string                                                    `json:"review_prompt"`
	AllGroups            *bool                                                      `json:"all_groups"`
	Groups               *[]string                                                  `json:"groups"`
	Endpoints            *[]promptAuditEndpointUpdate                               `json:"endpoints"`
	TotalTimeoutMS       *int                                                       `json:"total_timeout_ms"`
	ChunkOverlap         *int                                                       `json:"chunk_overlap"`
	CacheTTLSeconds      *int                                                       `json:"cache_ttl_seconds"`
	WorkerCount          *int                                                       `json:"worker_count"`
	MaxAttempts          *int                                                       `json:"max_attempts"`
	RetentionDays        *int                                                       `json:"retention_days"`
	GlobalConcurrency    *int                                                       `json:"global_concurrency"`
	EndpointConcurrency  *int                                                       `json:"endpoint_concurrency"`
	OutputMaxBytes       *int                                                       `json:"output_max_bytes"`
	OutputMemoryBytes    *int                                                       `json:"output_memory_bytes"`
}

type promptAuditFilterRequest struct {
	IDs        []int64 `json:"ids"`
	Status     string  `json:"status"`
	Decision   string  `json:"decision"`
	Category   string  `json:"category"`
	UserID     int     `json:"user_id"`
	Group      string  `json:"group"`
	Protocol   string  `json:"protocol"`
	Model      string  `json:"model"`
	EndpointID string  `json:"endpoint_id"`
	PromptHash string  `json:"prompt_hash"`
	RequestID  string  `json:"request_id"`
	Direction  string  `json:"direction"`
	Detector   string  `json:"detector"`
	StartTime  int64   `json:"start_time"`
	EndTime    int64   `json:"end_time"`
	MaxID      int64   `json:"max_id"`
}

type promptAuditDeleteRequest struct {
	Filter        promptAuditFilterRequest `json:"filter"`
	ExpectedCount int64                    `json:"expected_count"`
	MaxID         int64                    `json:"max_id"`
}

func GetPromptAuditConfig(c *gin.Context) {
	setting := prompt_audit_setting.GetSetting()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": promptAuditConfigResponse(setting)})
}

func UpdatePromptAuditConfig(c *gin.Context) {
	var update promptAuditConfigUpdate
	if err := common.DecodeJson(c.Request.Body, &update); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	current := prompt_audit_setting.GetSetting()
	proposed := current
	values := map[string]string{}
	if update.ScopePolicies != nil {
		proposed.ScopePolicies = *update.ScopePolicies
		rows, err := model.ListPromptWordlists()
		if err != nil {
			common.ApiError(c, errors.New("wordlists are unavailable"))
			return
		}
		known := map[string]bool{prompt_audit_setting.ManualWordlistID: true}
		for _, row := range rows {
			known[strconv.FormatInt(row.ID, 10)] = true
		}
		for _, policy := range *update.ScopePolicies {
			for _, id := range policy.LibraryIDs {
				if !known[id] {
					c.JSON(400, gin.H{"success": false, "message": "unknown wordlist"})
					return
				}
			}
		}
		data, err := common.Marshal(*update.ScopePolicies)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		values["prompt_audit.scope_policies"] = string(data)
	}
	if update.WordFilterEnabled != nil {
		values["CheckSensitiveEnabled"] = strconv.FormatBool(*update.WordFilterEnabled)
		values["CheckSensitiveOnPromptEnabled"] = strconv.FormatBool(*update.WordFilterEnabled)
	}
	if update.Mode != nil {
		values["prompt_audit.mode"] = *update.Mode
		proposed.Mode = *update.Mode
	}
	if update.OutputMode != nil {
		values["prompt_audit.output_mode"] = *update.OutputMode
	}
	if update.ManualWordlistAction != nil {
		values["prompt_audit.manual_wordlist_action"] = *update.ManualWordlistAction
		proposed.ManualWordlistAction = *update.ManualWordlistAction
	}
	if update.EnabledCategories != nil {
		data, err := common.Marshal(*update.EnabledCategories)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		values["prompt_audit.enabled_categories"] = string(data)
	}
	if update.ControversialBlocks != nil {
		data, err := common.Marshal(*update.ControversialBlocks)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		values["prompt_audit.controversial_block_categories"] = string(data)
	}
	if update.ReviewEnabled != nil {
		values["prompt_audit.review_enabled"] = strconv.FormatBool(*update.ReviewEnabled)
	}
	if update.ReviewPrompt != nil {
		values["prompt_audit.review_prompt"] = *update.ReviewPrompt
	}
	if update.AllGroups != nil {
		values["prompt_audit.all_groups"] = strconv.FormatBool(*update.AllGroups)
	}
	if update.Groups != nil {
		data, err := common.Marshal(*update.Groups)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		values["prompt_audit.groups"] = string(data)
	}
	if update.Endpoints != nil {
		endpoints := mergePromptAuditEndpointUpdates(current.Endpoints, *update.Endpoints)
		proposed.Endpoints = endpoints
		data, err := common.Marshal(endpoints)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		values["prompt_audit.endpoints_secret"] = string(data)
	}
	promptAuditSetInt(values, "prompt_audit.total_timeout_ms", update.TotalTimeoutMS)
	promptAuditSetInt(values, "prompt_audit.chunk_overlap", update.ChunkOverlap)
	promptAuditSetInt(values, "prompt_audit.cache_ttl_seconds", update.CacheTTLSeconds)
	promptAuditSetInt(values, "prompt_audit.worker_count", update.WorkerCount)
	promptAuditSetInt(values, "prompt_audit.max_attempts", update.MaxAttempts)
	promptAuditSetInt(values, "prompt_audit.retention_days", update.RetentionDays)
	promptAuditSetInt(values, "prompt_audit.global_concurrency", update.GlobalConcurrency)
	promptAuditSetInt(values, "prompt_audit.endpoint_concurrency", update.EndpointConcurrency)
	promptAuditSetInt(values, "prompt_audit.output_max_bytes", update.OutputMaxBytes)
	promptAuditSetInt(values, "prompt_audit.output_memory_bytes", update.OutputMemoryBytes)
	if err := validatePromptWordlistReviewBindings(proposed); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if len(values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "no prompt audit fields were provided"})
		return
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	setting := prompt_audit_setting.GetSetting()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": promptAuditConfigResponse(setting)})
}

func validatePromptWordlistReviewBindings(configured prompt_audit_setting.PromptAuditSetting) error {
	rows, err := model.ListPromptWordlists()
	if err != nil {
		return errors.New("wordlists are unavailable")
	}
	actions := map[string]string{
		prompt_audit_setting.ManualWordlistID: prompt_audit_setting.NormalizeWordlistAction(configured.ManualWordlistAction),
	}
	for _, row := range rows {
		actions[strconv.FormatInt(row.ID, 10)] = prompt_audit_setting.NormalizeWordlistAction(row.Action)
	}
	hasClassifier := false
	for _, endpoint := range configured.Endpoints {
		if endpoint.Enabled && endpoint.Purpose != prompt_audit_setting.EndpointPurposeReview && (len(endpoint.Directions) == 0 || slices.Contains(endpoint.Directions, "input")) {
			hasClassifier = true
			break
		}
	}
	for _, scope := range dto.PromptAuditScopes() {
		policy := configured.PolicyFor(scope)
		for _, id := range policy.LibraryIDs {
			if actions[id] != prompt_audit_setting.WordlistActionReview {
				continue
			}
			if configured.Mode == prompt_audit_setting.ModeOff || !policy.ModelAudit || !hasClassifier {
				return fmt.Errorf("review wordlist %q requires model audit for source %q and an enabled classification node", id, scope)
			}
		}
	}
	return nil
}

func GetPromptAuditCategories(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": service.PromptAuditCategories()})
}

func TestPromptAuditNode(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	setting := prompt_audit_setting.GetSetting()
	var selected *prompt_audit_setting.Endpoint
	for index := range setting.Endpoints {
		if setting.Endpoints[index].ID == id {
			selected = &setting.Endpoints[index]
			break
		}
	}
	if selected == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "prompt audit node not found"})
		return
	}
	startedAt := time.Now()
	result, err := service.TestPromptAuditEndpoint(c.Request.Context(), *selected)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false, "message": "prompt audit node test failed",
			"data": gin.H{"endpoint_id": selected.ID, "latency_ms": time.Since(startedAt).Milliseconds()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true, "message": "",
		"data": gin.H{"endpoint_id": selected.ID, "purpose": selected.Purpose, "latency_ms": time.Since(startedAt).Milliseconds(), "safety": result.Safety, "decision": result.ReviewDecision},
	})
}

func TestPromptAuditPolicy(c *gin.Context) {
	var request struct {
		Direction string                   `json:"direction"`
		Segments  []dto.PromptAuditSegment `json:"segments"`
		Output    string                   `json:"output"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit test"})
		return
	}
	result, err := service.TestPromptAuditPolicy(c.Request.Context(), request.Direction, dto.PromptAuditSnapshot{Segments: request.Segments}, request.Output)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error(), "data": result})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func ReviewPromptAudit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit id"})
		return
	}
	var request struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 4096), &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit review"})
		return
	}
	if err := model.ReviewPromptAudit(id, c.GetInt("id"), c.GetString("username"), request.Status, request.Reason); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrPromptAuditNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, model.ErrPromptAuditNotReviewable) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func ListPromptAudits(c *gin.Context) {
	filter := promptAuditFilterFromQuery(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	audits, total, err := model.ListPromptAudits(filter, page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]model.PromptAuditResponse, 0, len(audits))
	for _, audit := range audits {
		items = append(items, audit.ToResponse(false))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true, "message": "",
		"data": gin.H{"items": items, "total": total, "page": page, "page_size": pageSize},
	})
}

func GetPromptAudit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit id"})
		return
	}
	audit, err := model.GetPromptAudit(id)
	if err != nil {
		if errors.Is(err, model.ErrPromptAuditNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	includeFull := len(audit.FullPrompt) > 0 && authz.Can(c.GetInt("id"), c.GetInt("role"), authz.PromptAuditViewFullPrompt)
	if includeFull {
		model.RecordOperationAuditLog(
			c.GetInt("id"),
			c.GetInt("role"),
			"Viewed full prompt audit content",
			c.ClientIP(),
			"prompt_audit.view_full_prompt",
			map[string]interface{}{"prompt_audit_id": audit.ID},
			auditOperatorInfo(c),
			&model.AuditRequestInfo{Method: http.MethodGet, Route: c.FullPath(), Path: c.Request.URL.Path, Status: http.StatusOK, Success: true},
			c,
		)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": audit.ToResponse(includeFull)})
}

func GetPromptAuditStats(c *gin.Context) {
	stats, err := model.GetPromptAuditStats(promptAuditFilterFromQuery(c), prompt_audit_setting.AllCategoryIDs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": stats})
}

func RetryPromptAudit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit id"})
		return
	}
	if err := model.RetryPromptAudit(id, prompt_audit_setting.GetSetting().MaxAttempts); err != nil {
		status := http.StatusConflict
		if errors.Is(err, model.ErrPromptAuditNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	service.NotifyPromptAuditWorkers()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": id, "status": model.PromptAuditStatusQueued}})
}

func PreviewDeletePromptAudits(c *gin.Context) {
	var request promptAuditDeleteRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	filter := request.Filter.toModel()
	eligible, active, maxID, err := model.PreviewPromptAuditDelete(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true, "message": "",
		"data": gin.H{"eligible_count": eligible, "active_count": active, "max_id": maxID},
	})
}

func DeletePromptAudits(c *gin.Context) {
	var request promptAuditDeleteRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	filter := request.Filter.toModel()
	filter.MaxID = request.MaxID
	_, active, _, err := model.PreviewPromptAuditDelete(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if active > 0 {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": model.ErrPromptAuditActive.Error()})
		return
	}
	deleted, err := model.DeletePromptAudits(request.Filter.toModel(), request.ExpectedCount, request.MaxID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrPromptAuditDeleteChanged) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"deleted_count": deleted}})
}

func promptAuditConfigResponse(setting prompt_audit_setting.PromptAuditSetting) gin.H {
	return gin.H{
		"scope_policies": setting.EffectiveScopePolicies(), "word_filter_enabled": globalsetting.ShouldCheckPromptSensitive(),
		"mode": setting.Mode, "output_mode": setting.OutputMode, "manual_wordlist_action": setting.ManualWordlistAction,
		"enabled_categories":             append([]string{}, setting.EnabledCategories...),
		"controversial_block_categories": append([]string{}, setting.ControversialBlocks...),
		"review_enabled":                 setting.ReviewEnabled, "review_prompt": setting.ReviewPrompt,
		"all_groups": setting.AllGroups, "groups": append([]string{}, setting.Groups...),
		"endpoints": setting.SanitizedEndpoints(), "total_timeout_ms": setting.TotalTimeoutMS,
		"chunk_overlap": setting.ChunkOverlap, "cache_ttl_seconds": setting.CacheTTLSeconds,
		"worker_count": setting.WorkerCount, "max_attempts": setting.MaxAttempts,
		"retention_days": setting.RetentionDays, "global_concurrency": setting.GlobalConcurrency,
		"endpoint_concurrency": setting.EndpointConcurrency, "output_max_bytes": setting.OutputMaxBytes,
		"output_memory_bytes": setting.OutputMemoryBytes, "config_version": setting.ConfigVersion,
	}
}

func mergePromptAuditEndpointUpdates(current []prompt_audit_setting.Endpoint, updates []promptAuditEndpointUpdate) []prompt_audit_setting.Endpoint {
	existingEndpoints := make(map[string]prompt_audit_setting.Endpoint, len(current))
	for _, endpoint := range current {
		existingEndpoints[endpoint.ID] = endpoint
	}
	endpoints := make([]prompt_audit_setting.Endpoint, 0, len(updates))
	for _, endpoint := range updates {
		lookupID := strings.TrimSpace(endpoint.OriginalID)
		if lookupID == "" {
			lookupID = strings.TrimSpace(endpoint.ID)
		}
		token := ""
		if existing, ok := existingEndpoints[lookupID]; ok &&
			strings.TrimRight(strings.TrimSpace(existing.BaseURL), "/") == strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/") {
			// A stored write-only token may follow an ID rename, but never an
			// endpoint URL change. Otherwise a manage-only administrator could
			// redirect the saved credential to a host they control.
			token = existing.Token
		}
		if endpoint.Token != nil {
			token = *endpoint.Token
		}
		endpoints = append(endpoints, prompt_audit_setting.Endpoint{
			ID: endpoint.ID, Name: endpoint.Name, BaseURL: endpoint.BaseURL, Token: token,
			Model: endpoint.Model, TimeoutMS: endpoint.TimeoutMS, InputLimit: endpoint.InputLimit,
			Concurrency: endpoint.Concurrency, Enabled: endpoint.Enabled, Purpose: endpoint.Purpose, Directions: append([]string(nil), endpoint.Directions...),
		})
	}
	return endpoints
}

func promptAuditSetInt(values map[string]string, key string, value *int) {
	if value != nil {
		values[key] = strconv.Itoa(*value)
	}
}

func promptAuditFilterFromQuery(c *gin.Context) model.PromptAuditFilter {
	userID, _ := strconv.Atoi(c.Query("user_id"))
	startTime, _ := strconv.ParseInt(c.Query("start_time"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_time"), 10, 64)
	return model.PromptAuditFilter{
		Status: strings.TrimSpace(c.Query("status")), Decision: strings.TrimSpace(c.Query("decision")),
		Category: strings.TrimSpace(c.Query("category")), UserID: userID,
		Group: strings.TrimSpace(c.Query("group")), Protocol: strings.TrimSpace(c.Query("protocol")),
		Model: strings.TrimSpace(c.Query("model")), EndpointID: strings.TrimSpace(c.Query("endpoint_id")),
		PromptHash: strings.TrimSpace(c.Query("prompt_hash")), RequestID: strings.TrimSpace(c.Query("request_id")),
		Direction: strings.TrimSpace(c.Query("direction")), Detector: strings.TrimSpace(c.Query("detector")),
		StartTime: startTime, EndTime: endTime,
	}
}

func (request promptAuditFilterRequest) toModel() model.PromptAuditFilter {
	return model.PromptAuditFilter{
		IDs: append([]int64(nil), request.IDs...), Status: strings.TrimSpace(request.Status),
		Decision: strings.TrimSpace(request.Decision), Category: strings.TrimSpace(request.Category),
		UserID: request.UserID, Group: strings.TrimSpace(request.Group), Protocol: strings.TrimSpace(request.Protocol),
		Model: strings.TrimSpace(request.Model), EndpointID: strings.TrimSpace(request.EndpointID),
		PromptHash: strings.TrimSpace(request.PromptHash), RequestID: strings.TrimSpace(request.RequestID),
		Direction: strings.TrimSpace(request.Direction), Detector: strings.TrimSpace(request.Detector),
		StartTime: request.StartTime, EndTime: request.EndTime, MaxID: request.MaxID,
	}
}
