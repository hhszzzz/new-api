package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/distribution_risk_setting"
	"github.com/gin-gonic/gin"
)

type distributionRiskEvidenceResponse struct {
	IPs        []string `json:"ips"`
	UserAgents []string `json:"user_agents"`
	Sessions   []string `json:"session_hashes"`
}

type distributionRiskTokenResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type distributionRiskEventResponse struct {
	ID             int64                             `json:"id"`
	UserID         int                               `json:"user_id"`
	Username       string                            `json:"username"`
	Tokens         []distributionRiskTokenResponse   `json:"tokens"`
	Score          int                               `json:"score"`
	Level          string                            `json:"level"`
	CategoryScores map[string]int                    `json:"category_scores"`
	Status         string                            `json:"status"`
	CreatedAt      int64                             `json:"created_at"`
	ReviewedAt     int64                             `json:"reviewed_at"`
	ReviewerID     int                               `json:"reviewer_id"`
	ReviewReason   string                            `json:"review_reason"`
	Evidence       *distributionRiskEvidenceResponse `json:"evidence,omitempty"`
}

type distributionRiskConfigUpdate struct {
	Enabled           *bool `json:"enabled"`
	IPFloor           *int  `json:"ip_floor"`
	UserAgentFloor    *int  `json:"user_agent_floor"`
	SessionFloor      *int  `json:"session_floor"`
	RequestsPerMinute *int  `json:"requests_per_minute_floor"`
	ConcurrencyFloor  *int  `json:"concurrency_floor"`
	MinRequests       *int  `json:"min_requests"`
	MinActiveDays     *int  `json:"min_active_days"`
}

func GetDistributionRiskConfig(c *gin.Context) {
	current := distribution_risk_setting.GetSnapshot()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"enabled": current.Enabled, "mode": distribution_risk_setting.CurrentMode(),
		"started_at": current.StartedAt, "shadow_days": distribution_risk_setting.ShadowDays,
		"ip_floor": current.IPFloor, "user_agent_floor": current.UserAgentFloor,
		"session_floor": current.SessionFloor, "requests_per_minute_floor": current.RequestsPerMinute,
		"concurrency_floor": current.ConcurrencyFloor, "min_requests": current.MinRequests,
		"min_active_days": current.MinActiveDays,
	}})
}

func UpdateDistributionRiskConfig(c *gin.Context) {
	var update distributionRiskConfigUpdate
	if err := c.ShouldBindJSON(&update); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid distribution risk settings"})
		return
	}
	current := distribution_risk_setting.GetSnapshot()
	proposed := current
	if update.Enabled != nil {
		proposed.Enabled = *update.Enabled
		if *update.Enabled && !current.Enabled {
			proposed.StartedAt = time.Now().Unix()
		}
	}
	if update.IPFloor != nil {
		proposed.IPFloor = *update.IPFloor
	}
	if update.UserAgentFloor != nil {
		proposed.UserAgentFloor = *update.UserAgentFloor
	}
	if update.SessionFloor != nil {
		proposed.SessionFloor = *update.SessionFloor
	}
	if update.RequestsPerMinute != nil {
		proposed.RequestsPerMinute = *update.RequestsPerMinute
	}
	if update.ConcurrencyFloor != nil {
		proposed.ConcurrencyFloor = *update.ConcurrencyFloor
	}
	if update.MinRequests != nil {
		proposed.MinRequests = *update.MinRequests
	}
	if update.MinActiveDays != nil {
		proposed.MinActiveDays = *update.MinActiveDays
	}
	if err := proposed.ValidateConfig(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	values := map[string]string{
		"distribution_risk_setting.enabled":                   strconv.FormatBool(proposed.Enabled),
		"distribution_risk_setting.started_at":                strconv.FormatInt(proposed.StartedAt, 10),
		"distribution_risk_setting.ip_floor":                  strconv.Itoa(proposed.IPFloor),
		"distribution_risk_setting.user_agent_floor":          strconv.Itoa(proposed.UserAgentFloor),
		"distribution_risk_setting.session_floor":             strconv.Itoa(proposed.SessionFloor),
		"distribution_risk_setting.requests_per_minute_floor": strconv.Itoa(proposed.RequestsPerMinute),
		"distribution_risk_setting.concurrency_floor":         strconv.Itoa(proposed.ConcurrencyFloor),
		"distribution_risk_setting.min_requests":              strconv.Itoa(proposed.MinRequests),
		"distribution_risk_setting.min_active_days":           strconv.Itoa(proposed.MinActiveDays),
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}
	GetDistributionRiskConfig(c)
}

func ListDistributionRiskEvents(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != "all" && status != model.DistributionRiskPending && status != model.DistributionRiskConfirmed && status != model.DistributionRiskFalsePositive {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid event status"})
		return
	}
	level := strings.TrimSpace(c.Query("level"))
	if level != "" && level != "all" && level != "medium" && level != "high" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid risk level"})
		return
	}
	scopeKind := strings.TrimSpace(c.Query("scope_kind"))
	if scopeKind != "" && scopeKind != "all" && scopeKind != "user" && scopeKind != "token" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid event scope"})
		return
	}
	filters := model.DistributionRiskEventFilters{Status: status, Level: level, ScopeKind: scopeKind}
	if rawUserID := strings.TrimSpace(c.Query("user_id")); rawUserID != "" {
		userID, err := strconv.Atoi(rawUserID)
		if err != nil || userID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid user ID"})
			return
		}
		filters.UserID = userID
	}
	filters.Username = strings.TrimSpace(c.Query("username"))
	startTimestamp := strings.TrimSpace(c.Query("start_timestamp"))
	if startTimestamp != "" {
		parsed, err := strconv.ParseInt(startTimestamp, 10, 64)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid event time range"})
			return
		}
		filters.StartTime = parsed
	}
	endTimestamp := strings.TrimSpace(c.Query("end_timestamp"))
	if endTimestamp != "" {
		parsed, err := strconv.ParseInt(endTimestamp, 10, 64)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid event time range"})
			return
		}
		filters.EndTime = parsed
	}
	if filters.StartTime > 0 && filters.EndTime > 0 && filters.StartTime > filters.EndTime {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid event time range"})
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	events, total, err := model.ListDistributionRiskEvents(filters, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pending, err := model.CountPendingDistributionRiskEvents()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	displayNames, err := model.GetDistributionRiskEventDisplayNames(events)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]distributionRiskEventResponse, 0, len(events))
	for index := range events {
		item, err := distributionRiskEventSummary(&events[index], displayNames)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items": items, "total": total, "pending_count": pending, "page": page, "page_size": pageSize,
	}})
}

func GetDistributionRiskEvent(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid event ID"})
		return
	}
	event, err := model.GetDistributionRiskEvent(id)
	if err != nil {
		writeDistributionRiskEventError(c, err)
		return
	}
	displayNames, err := model.GetDistributionRiskEventDisplayNames([]model.DistributionRiskEvent{*event})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response, err := distributionRiskEventSummary(event, displayNames)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var evidence distributionRiskEvidenceResponse
	if event.Evidence != "" {
		if err := common.UnmarshalJsonStr(string(event.Evidence), &evidence); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	response.Evidence = &evidence
	c.JSON(http.StatusOK, gin.H{"success": true, "data": response})
}

func ReviewDistributionRiskEvent(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid event ID"})
		return
	}
	var request struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid review request"})
		return
	}
	request.Decision = strings.TrimSpace(request.Decision)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 512 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Review reason is required and must not exceed 512 characters"})
		return
	}
	event, err := model.GetDistributionRiskEvent(id)
	if err != nil {
		writeDistributionRiskEventError(c, err)
		return
	}
	tokenIDs, err := model.ReviewDistributionRiskEvent(id, request.Decision, c.GetInt("id"), request.Reason)
	if err != nil {
		writeDistributionRiskEventError(c, err)
		return
	}
	service.InvalidateDistributionRiskHoldCache(tokenIDs)
	if request.Decision == model.DistributionRiskFalsePositive {
		service.SuppressDistributionRisk(event, tokenIDs)
	}
	model.RecordAuditLog(c, model.AuditLog{
		UserId: c.GetInt("id"), Username: c.GetString("username"), ActorRole: c.GetInt("role"),
		Category: model.AuditCategorySecurity, Action: "distribution_risk.review", Success: true,
		Content: fmt.Sprintf("event_id=%d decision=%s", id, request.Decision),
	})
	markAuditLogged(c)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Distribution risk event reviewed"})
}

func distributionRiskEventSummary(event *model.DistributionRiskEvent, displayNames model.DistributionRiskEventDisplayNames) (distributionRiskEventResponse, error) {
	tokenIDs, err := model.DistributionRiskEventTokenIDs(event)
	if err != nil {
		return distributionRiskEventResponse{}, err
	}
	var categories map[string]int
	if event.CategoryScores != "" {
		if err := common.UnmarshalJsonStr(string(event.CategoryScores), &categories); err != nil {
			return distributionRiskEventResponse{}, err
		}
	}
	tokens := make([]distributionRiskTokenResponse, 0, len(tokenIDs))
	for _, tokenID := range tokenIDs {
		tokens = append(tokens, distributionRiskTokenResponse{ID: tokenID, Name: displayNames.TokenNames[tokenID]})
	}
	return distributionRiskEventResponse{
		ID: event.ID, UserID: event.UserID, Username: displayNames.Usernames[event.UserID], Tokens: tokens,
		Score: event.Score,
		Level: event.Level, CategoryScores: categories, Status: event.Status, CreatedAt: event.CreatedAt,
		ReviewedAt: event.ReviewedAt, ReviewerID: event.ReviewerID, ReviewReason: event.ReviewReason,
	}, nil
}

func writeDistributionRiskEventError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, model.ErrDistributionRiskNotFound):
		status = http.StatusNotFound
	case errors.Is(err, model.ErrDistributionRiskNotPending), errors.Is(err, model.ErrDistributionRiskDecision):
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}
