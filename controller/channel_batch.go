package controller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	channelBatchTargetSelected = "selected"
	channelBatchTargetFiltered = "filtered"
)

var errChannelBatchTargetChanged = errors.New("filtered channel target changed")

type channelBatchFilter struct {
	Keyword string `json:"keyword"`
	Group   string `json:"group"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Type    *int   `json:"type"`
}

type channelBatchTarget struct {
	Mode        string             `json:"mode"`
	IDs         []int              `json:"ids"`
	Filter      channelBatchFilter `json:"filter"`
	Fingerprint string             `json:"fingerprint"`
}

type channelBatchPreviewRequest struct {
	Filter channelBatchFilter `json:"filter"`
}

type channelBatchUpdateRequest struct {
	Target  channelBatchTarget       `json:"target"`
	Updates model.ChannelBatchUpdate `json:"updates"`
}

func PreviewChannelBatch(c *gin.Context) {
	request := channelBatchPreviewRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := resolveChannelBatchFilterIDs(request.Filter)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, gin.H{
		"count":       len(ids),
		"fingerprint": fingerprintChannelIDs(ids),
	})
}

func BatchUpdateChannels(c *gin.Context) {
	request := channelBatchUpdateRequest{}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Updates.Empty() {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "no batch update fields selected"})
		return
	}

	ids, err := resolveChannelBatchTargetIDs(request.Target)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errChannelBatchTargetChanged) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	updated, err := model.BatchUpdateChannels(ids, request.Updates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	recordManageAudit(c, "channel.batch_update", map[string]interface{}{
		"count":          updated,
		"target_mode":    request.Target.Mode,
		"changed_fields": request.Updates.ChangedFields(),
		"rate_limits": gin.H{
			"rpm_limit":         request.Updates.RpmLimit,
			"concurrency_limit": request.Updates.ConcurrencyLimit,
		},
	})
	common.ApiSuccess(c, gin.H{"updated": updated})
}

func resolveChannelBatchTargetIDs(target channelBatchTarget) ([]int, error) {
	switch target.Mode {
	case channelBatchTargetSelected:
		ids := normalizeControllerChannelIDs(target.IDs)
		if len(ids) == 0 {
			return nil, fmt.Errorf("no selected channels")
		}
		return ids, nil
	case channelBatchTargetFiltered:
		if target.Fingerprint == "" {
			return nil, fmt.Errorf("filtered target fingerprint is required")
		}
		ids, err := resolveChannelBatchFilterIDs(target.Filter)
		if err != nil {
			return nil, err
		}
		if fingerprintChannelIDs(ids) != target.Fingerprint {
			return nil, fmt.Errorf("%w; preview and confirm again", errChannelBatchTargetChanged)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("filtered target contains no channels")
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("unsupported channel batch target mode %q", target.Mode)
	}
}

func resolveChannelBatchFilterIDs(filter channelBatchFilter) ([]int, error) {
	status := strings.ToLower(strings.TrimSpace(filter.Status))
	if status != "" && status != "all" && status != "enabled" && status != "disabled" && status != "1" && status != "0" {
		return nil, fmt.Errorf("invalid channel status filter")
	}
	if filter.Type != nil && *filter.Type < 0 {
		return nil, fmt.Errorf("invalid channel type filter")
	}

	statusFilter := parseStatusFilter(status)
	var query *gorm.DB
	if strings.TrimSpace(filter.Keyword) != "" || strings.TrimSpace(filter.Model) != "" {
		query = model.BuildChannelSearchQuery(filter.Keyword, filter.Group, filter.Model)
		query = applyChannelStatusFilter(query, statusFilter)
		if filter.Type != nil {
			query = query.Where("type = ?", *filter.Type)
		}
	} else {
		typeFilter := -1
		if filter.Type != nil {
			typeFilter = *filter.Type
		}
		query = buildChannelListQuery(filter.Group, statusFilter, typeFilter)
	}

	ids := make([]int, 0)
	if err := query.Order("id ASC").Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return normalizeControllerChannelIDs(ids), nil
}

func normalizeControllerChannelIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	normalized := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	sort.Ints(normalized)
	return normalized
}

func fingerprintChannelIDs(ids []int) string {
	parts := make([]string, 0, len(ids))
	for _, id := range normalizeControllerChannelIDs(ids) {
		parts = append(parts, strconv.Itoa(id))
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, ","))))
}
