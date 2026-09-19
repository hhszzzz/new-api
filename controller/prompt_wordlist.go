package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type promptWordlistResponse struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	SourceURL      string                 `json:"source_url"`
	Enabled        bool                   `json:"enabled"`
	AutoUpdate     bool                   `json:"auto_update"`
	Status         string                 `json:"status"`
	WordCount      int                    `json:"word_count"`
	FileCount      int                    `json:"file_count"`
	ContentHash    string                 `json:"content_hash"`
	SourceRevision string                 `json:"source_revision"`
	LastSuccessAt  int64                  `json:"last_success_at"`
	NextSyncAt     int64                  `json:"next_sync_at"`
	LastError      string                 `json:"last_error"`
	Scopes         []dto.PromptAuditScope `json:"scopes"`
}

func ListPromptWordlists(c *gin.Context) {
	rows, err := model.ListPromptWordlists()
	if err != nil {
		common.ApiError(c, errors.New("wordlists are unavailable"))
		return
	}
	configured := prompt_audit_setting.GetSetting()
	items := []promptWordlistResponse{{ID: "manual", Name: "Custom wordlist", Enabled: configured.ManualWordlistActive(), Status: "ready", WordCount: len(setting.SensitiveWordsSnapshot())}}
	for _, row := range rows {
		if row.RequestedAt > 0 {
			row.Status = "pending"
		}
		items = append(items, promptWordlistResponse{ID: strconv.FormatInt(row.ID, 10), Name: row.Name, SourceURL: row.SourceURL,
			Enabled: row.Enabled, AutoUpdate: row.AutoUpdate, Status: row.Status, WordCount: row.WordCount, FileCount: row.FileCount,
			ContentHash: row.ContentHash, SourceRevision: row.SourceRevision, LastSuccessAt: row.LastSuccessAt,
			NextSyncAt: row.NextSyncAt, LastError: row.LastError})
	}
	for index := range items {
		items[index].Scopes = []dto.PromptAuditScope{}
		for _, scope := range dto.PromptAuditScopes() {
			if slices.Contains(configured.PolicyFor(scope).LibraryIDs, items[index].ID) {
				items[index].Scopes = append(items[index].Scopes, scope)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

func CreatePromptWordlist(c *gin.Context) {
	var request struct {
		Name       string                 `json:"name"`
		SourceURL  string                 `json:"source_url"`
		Scopes     []dto.PromptAuditScope `json:"scopes"`
		AutoUpdate *bool                  `json:"auto_update"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(400, gin.H{"success": false, "message": "invalid wordlist request"})
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		c.JSON(400, gin.H{"success": false, "message": "wordlist name must contain 1 to 128 characters"})
		return
	}
	canonical, err := service.NormalizePromptWordlistURL(request.SourceURL)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	if request.Scopes == nil {
		request.Scopes = []dto.PromptAuditScope{dto.PromptScopeUser, dto.PromptScopeTask}
	}
	for _, scope := range request.Scopes {
		if !slices.Contains(dto.PromptAuditScopes(), scope) {
			c.JSON(400, gin.H{"success": false, "message": "invalid inspection source"})
			return
		}
	}
	digest := sha256.Sum256([]byte(canonical))
	row := &model.PromptWordlist{Name: name, SourceURL: canonical, SourceHash: hex.EncodeToString(digest[:]), Enabled: true, AutoUpdate: true}
	if request.AutoUpdate != nil {
		row.AutoUpdate = *request.AutoUpdate
	}
	if err := model.CreatePromptWordlist(row, request.Scopes...); err != nil {
		if errors.Is(err, model.ErrPromptWordlistExists) || errors.Is(err, model.ErrPromptWordlistLimit) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		} else {
			common.ApiError(c, errors.New("failed to create wordlist"))
		}
		return
	}
	if err := service.RefreshPromptWordlists(); err != nil {
		common.SysError("wordlist refresh failed after creation")
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"id": strconv.FormatInt(row.ID, 10), "status": "pending"}})
}

func UpdatePromptWordlist(c *gin.Context) {
	if c.Param("id") == prompt_audit_setting.ManualWordlistID {
		var request struct {
			Enabled *bool `json:"enabled"`
		}
		if common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 1024), &request) != nil || request.Enabled == nil {
			c.JSON(400, gin.H{"success": false, "message": "enabled is required"})
			return
		}
		if err := model.UpdateOption("prompt_audit.manual_wordlist_enabled", strconv.FormatBool(*request.Enabled)); err != nil {
			common.ApiError(c, err)
			return
		}
		c.JSON(200, gin.H{"success": true})
		return
	}
	row, ok := promptWordlistFromRequest(c)
	if !ok {
		return
	}
	var request struct {
		Name       *string `json:"name"`
		Enabled    *bool   `json:"enabled"`
		AutoUpdate *bool   `json:"auto_update"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if common.DecodeJson(c.Request.Body, &request) != nil {
		c.JSON(400, gin.H{"success": false, "message": "invalid wordlist request"})
		return
	}
	if request.Name != nil {
		*request.Name = strings.TrimSpace(*request.Name)
		if *request.Name == "" || utf8.RuneCountInString(*request.Name) > 128 {
			c.JSON(400, gin.H{"success": false, "message": "invalid wordlist name"})
			return
		}
	}
	if request.Name == nil && request.Enabled == nil && request.AutoUpdate == nil {
		c.JSON(400, gin.H{"success": false, "message": "no wordlist fields were provided"})
		return
	}
	if err := model.UpdatePromptWordlist(row.ID, model.PromptWordlistUpdate{Name: request.Name, Enabled: request.Enabled, AutoUpdate: request.AutoUpdate}); err != nil {
		common.ApiError(c, errors.New("failed to update wordlist"))
		return
	}
	if err := service.RefreshPromptWordlists(); err != nil {
		common.ApiError(c, errors.New("wordlist saved but runtime refresh failed"))
		return
	}
	c.JSON(200, gin.H{"success": true})
}

func SyncPromptWordlist(c *gin.Context) {
	row, ok := promptWordlistFromRequest(c)
	if !ok {
		return
	}
	if err := model.RequestPromptWordlistSync(row.ID); err != nil {
		common.ApiError(c, errors.New("failed to queue wordlist update"))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true})
}

func DeletePromptWordlist(c *gin.Context) {
	row, ok := promptWordlistFromRequest(c)
	if !ok {
		return
	}
	if err := model.DeletePromptWordlist(row.ID); err != nil {
		common.ApiError(c, errors.New("failed to delete wordlist"))
		return
	}
	if err := service.RefreshPromptWordlists(); err != nil {
		common.ApiError(c, errors.New("wordlist removed but runtime refresh failed"))
		return
	}
	c.JSON(200, gin.H{"success": true})
}

func promptWordlistFromRequest(c *gin.Context) (*model.PromptWordlist, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "message": "invalid wordlist id"})
		return nil, false
	}
	row, err := model.GetPromptWordlist(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"success": false, "message": "wordlist not found"})
		return nil, false
	}
	if err != nil {
		common.ApiError(c, errors.New("wordlist is unavailable"))
		return nil, false
	}
	return row, true
}

func GetManualPromptWordlist(c *gin.Context) {
	c.JSON(200, gin.H{"success": true, "data": gin.H{"words": setting.SensitiveWordsToString()}})
}

func UpdateManualPromptWordlist(c *gin.Context) {
	var request struct {
		Words string `json:"words"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024)
	if common.DecodeJson(c.Request.Body, &request) != nil || len(request.Words) > 1024*1024 || strings.ContainsRune(request.Words, 0) {
		c.JSON(400, gin.H{"success": false, "message": "invalid custom wordlist (maximum 1 MiB)"})
		return
	}
	if err := model.UpdateOption("SensitiveWords", request.Words); err != nil {
		common.ApiError(c, errors.New("failed to save custom wordlist"))
		return
	}
	c.JSON(200, gin.H{"success": true})
}

func TestPromptWordlists(c *gin.Context) {
	var request struct {
		Scope dto.PromptAuditScope `json:"scope"`
		Text  string               `json:"text"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
	if common.DecodeJson(c.Request.Body, &request) != nil || !slices.Contains(dto.PromptAuditScopes(), request.Scope) || utf8.RuneCountInString(request.Text) > 16384 {
		c.JSON(400, gin.H{"success": false, "message": "invalid inspection test"})
		return
	}
	match, modelAudit, err := service.TestPromptWordlists(request.Scope, request.Text)
	if err != nil {
		c.JSON(503, gin.H{"success": false, "message": "wordlist is unavailable"})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"match": match, "model_audit": modelAudit}})
}
