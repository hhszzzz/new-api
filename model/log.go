package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		condition, pattern, err := buildLogLikeCondition(column, value)
		if err != nil {
			return nil, err
		}
		return tx.Where(condition, pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

func buildLogLikeCondition(column string, value string) (string, string, error) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		pattern, err := sanitizeClickHouseLikePattern(value)
		if err != nil {
			return "", "", err
		}
		return column + " LIKE ?", pattern, nil
	}

	pattern, err := sanitizeLikePattern(value)
	if err != nil {
		return "", "", err
	}
	return column + " LIKE ? ESCAPE '!'", pattern, nil
}

func sanitizeClickHouseLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, `\`, `\\`)
	input = strings.ReplaceAll(input, `_`, `\_`)

	if err := validateLikePattern(input); err != nil {
		return "", err
	}
	return input, nil
}

type Log struct {
	Id                int    `json:"id" gorm:"index:idx_created_at_id,priority:2;index:idx_user_id_id,priority:2"`
	UserId            int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:1;index:idx_created_at_type"`
	Type              int    `json:"type" gorm:"index:idx_created_at_type"`
	Content           string `json:"content"`
	Username          string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName         string `json:"token_name" gorm:"index;default:''"`
	ModelName         string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota             int    `json:"quota" gorm:"default:0"`
	PromptTokens      int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int    `json:"completion_tokens" gorm:"default:0"`
	UseTime           int    `json:"use_time" gorm:"default:0"`
	IsStream          bool   `json:"is_stream"`
	ChannelId         int    `json:"channel" gorm:"index"`
	ChannelName       string `json:"channel_name" gorm:"->"`
	TokenId           int    `json:"token_id" gorm:"default:0;index"`
	Group             string `json:"group" gorm:"index"`
	Ip                string `json:"ip" gorm:"index;default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	Other             string `json:"other"`
}

type LogSortOptions struct {
	SortBy    string
	SortOrder string
}

var logSortColumns = map[string]string{
	"created_at":    "created_at",
	"channel":       "channel_id",
	"user":          "username",
	"token_name":    "token_name",
	"model_name":    "model_name",
	"is_stream":     "is_stream",
	"prompt_tokens": "prompt_tokens",
	"quota":         "quota",
	"use_time":      "use_time",
}

func NewLogSortOptions(sortBy string, sortOrder string) LogSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := logSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = "created_at"
		normalizedSortOrder = "desc"
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}
	return LogSortOptions{SortBy: normalizedSortBy, SortOrder: normalizedSortOrder}
}

func (options LogSortOptions) Apply(query *gorm.DB) *gorm.DB {
	columnName, ok := logSortColumns[options.SortBy]
	if !ok {
		columnName = "created_at"
	}
	query = query.Order(clause.OrderByColumn{
		Column: clause.Column{Table: "logs", Name: columnName},
		Desc:   options.SortOrder != "asc",
	})
	if columnName != "created_at" {
		query = query.Order(clause.OrderByColumn{
			Column: clause.Column{Table: "logs", Name: "created_at"},
			Desc:   true,
		})
	}
	tieBreakColumn := "id"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		tieBreakColumn = "request_id"
	}
	if columnName != tieBreakColumn {
		query = query.Order(clause.OrderByColumn{
			Column: clause.Column{Table: "logs", Name: tieBreakColumn},
			Desc:   true,
		})
	}
	return query
}

func resolveLogSortOptions(sortOptions []LogSortOptions) LogSortOptions {
	if len(sortOptions) == 0 {
		return NewLogSortOptions("", "")
	}
	return sortOptions[0]
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
	LogTypeLogin   = 7
)

func ensureLogRequestId(log *Log) {
	if log != nil && log.RequestId == "" {
		log.RequestId = common.NewRequestId()
	}
}

func createLog(log *Log) error {
	ensureLogRequestId(log)
	return LOG_DB.Create(log).Error
}

const (
	maxDiagnosticHeaderValueBytes = 512
	maxDiagnosticHeaderBytes      = 4 * 1024
)

var diagnosticHeaderAllowlist = map[string]struct{}{
	"user-agent":                  {},
	"content-type":                {},
	"anthropic-version":           {},
	"anthropic-beta":              {},
	"x-app":                       {},
	"originator":                  {},
	"traceparent":                 {},
	"tracestate":                  {},
	"x-request-id":                {},
	"x-client-request-id":         {},
	"x-codex-beta-features":       {},
	"x-codex-parent-thread-id":    {},
	"x-codex-turn-metadata":       {},
	"x-codex-turn-state":          {},
	"x-codex-window-id":           {},
	"x-openai-memgen-request":     {},
	"x-openai-subagent":           {},
	"x-stainless-arch":            {},
	"x-stainless-lang":            {},
	"x-stainless-os":              {},
	"x-stainless-package-version": {},
	"x-stainless-retry-count":     {},
	"x-stainless-runtime":         {},
	"x-stainless-runtime-version": {},
	"x-stainless-timeout":         {},
}

func ensureOtherMap(other map[string]interface{}) map[string]interface{} {
	if other == nil {
		return make(map[string]interface{})
	}
	return other
}

func ensureAdminInfo(other map[string]interface{}) map[string]interface{} {
	other = ensureOtherMap(other)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = map[string]interface{}{}
		other["admin_info"] = adminInfo
	}
	return adminInfo
}

func diagnosticHeaderValue(name, value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(name, "session") || strings.Contains(name, "thread") {
		digest := sha256.Sum256([]byte(value))
		return "sha256:" + hex.EncodeToString(digest[:])[:16]
	}
	if len(value) > maxDiagnosticHeaderValueBytes {
		value = value[:maxDiagnosticHeaderValueBytes]
	}
	return value
}

func collectDiagnosticHeaders(c *gin.Context) map[string]string {
	result := make(map[string]string)
	if c == nil || c.Request == nil {
		return result
	}
	setting := operation_setting.GetLogDiagnosticSettingSnapshot()
	allowed := make(map[string]struct{}, len(diagnosticHeaderAllowlist)+len(setting.ExtraHeaders))
	for name := range diagnosticHeaderAllowlist {
		allowed[name] = struct{}{}
	}
	for _, name := range operation_setting.NormalizeLogDiagnosticHeaders(setting.ExtraHeaders) {
		if operation_setting.IsDiagnosticHeaderAllowed(name) {
			allowed[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(c.Request.Header))
	for name := range c.Request.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	used := 0
	for _, headerName := range names {
		values := c.Request.Header.Values(headerName)
		name := strings.ToLower(strings.TrimSpace(headerName))
		if _, ok := allowed[name]; !ok || !operation_setting.IsDiagnosticHeaderAllowed(name) {
			continue
		}
		value := ""
		if len(values) > 0 {
			value = diagnosticHeaderValue(name, values[0])
		}
		if value == "" {
			continue
		}
		cost := len(name) + len(value) + 2
		if used+cost > maxDiagnosticHeaderBytes {
			break
		}
		result[name] = value
		used += cost
	}
	return result
}

func channelSnapshot(channelId int) map[string]interface{} {
	if channelId <= 0 {
		return nil
	}
	channel, err := CacheGetChannel(channelId)
	if err != nil || channel == nil {
		channel, err = GetChannelById(channelId, false)
		if err != nil || channel == nil {
			return nil
		}
	}
	actualName := strings.TrimSpace(channel.Name)
	aggregateName := strings.TrimSpace(channel.AggregateName)
	if aggregateName == "" {
		_ = HydrateChannelAggregateSnapshots([]*Channel{channel})
		aggregateName = strings.TrimSpace(channel.AggregateName)
	}
	snapshot := map[string]interface{}{
		"actual_channel_id":   channel.Id,
		"actual_channel_name": actualName,
	}
	if aggregateName != "" {
		snapshot["aggregate_name"] = aggregateName
	}
	return snapshot
}

func routePoolAggregateName(c *gin.Context) string {
	if c == nil || DB == nil || common.GetContextKeyInt(c, constant.ContextKeyUserModelRouteId) <= 0 {
		return ""
	}
	channelIds, ok := common.GetContextKeyType[[]int](c, constant.ContextKeyUserModelRouteChannel)
	if !ok || len(channelIds) == 0 {
		return ""
	}
	seen := make(map[int]struct{}, len(channelIds))
	normalizedIds := make([]int, 0, len(channelIds))
	for _, channelId := range channelIds {
		if channelId <= 0 {
			return ""
		}
		if _, exists := seen[channelId]; exists {
			continue
		}
		seen[channelId] = struct{}{}
		normalizedIds = append(normalizedIds, channelId)
	}
	if len(normalizedIds) == 0 {
		return ""
	}

	channels := make([]*Channel, 0, len(normalizedIds))
	if err := DB.Where("id IN ?", normalizedIds).Find(&channels).Error; err != nil || len(channels) != len(normalizedIds) {
		return ""
	}
	if err := HydrateChannelAggregateSnapshots(channels); err != nil {
		return ""
	}

	aggregateId := 0
	aggregateName := ""
	for _, channel := range channels {
		if channel == nil || channel.AggregateId == nil || *channel.AggregateId <= 0 {
			return ""
		}
		name := strings.TrimSpace(channel.AggregateName)
		if name == "" {
			return ""
		}
		if aggregateId == 0 {
			aggregateId = *channel.AggregateId
			aggregateName = name
			continue
		}
		if aggregateId != *channel.AggregateId {
			return ""
		}
	}
	return aggregateName
}

func appendLogDiagnostics(c *gin.Context, channelId int, other map[string]interface{}) map[string]interface{} {
	other = ensureOtherMap(other)
	adminInfo := ensureAdminInfo(other)
	diagnostics := map[string]interface{}{}
	if c != nil && c.Request != nil {
		diagnostics["method"] = c.Request.Method
		if c.Request.URL != nil {
			diagnostics["path"] = c.Request.URL.Path
		}
		if c.Request.ContentLength >= 0 {
			diagnostics["request_size"] = c.Request.ContentLength
		}
	}
	if c != nil && c.Writer != nil {
		diagnostics["status_code"] = c.Writer.Status()
		if size := c.Writer.Size(); size >= 0 {
			diagnostics["response_size"] = size
		}
	}
	if c != nil {
		if value := common.GetContextKeyString(c, constant.ContextKeyClientName); value != "" {
			diagnostics["client"] = value
		}
		for key, target := range map[constant.ContextKey]string{
			constant.ContextKeyRequestProtocol:    "request_protocol",
			constant.ContextKeyUserModelRoutePool: "route_pool_name",
			constant.ContextKeyUserModelRouteId:   "route_rule_id",
		} {
			if key == constant.ContextKeyUserModelRouteId {
				if id := common.GetContextKeyInt(c, key); id > 0 {
					diagnostics[target] = id
				}
				continue
			}
			if value := common.GetContextKeyString(c, key); value != "" {
				diagnostics[target] = value
			}
		}
		for key, target := range map[constant.ContextKey]string{
			constant.ContextKeyUpstreamProtocol:  "upstream_protocol",
			constant.ContextKeyProtocolConverter: "protocol_converter",
			constant.ContextKeyProtocolStateMode: "protocol_state_mode",
		} {
			if value := common.GetContextKeyString(c, key); value != "" {
				adminInfo[target] = value
			}
		}
		if started := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime); !started.IsZero() {
			diagnostics["duration_ms"] = time.Since(started).Milliseconds()
		}
		if operation_setting.GetLogDiagnosticSettingSnapshot().RecordIP {
			diagnostics["ip"] = c.ClientIP()
		}
		if upstreamRequestSize, ok := common.GetContextKeyType[int64](c, constant.ContextKeyUpstreamRequestSize); ok && upstreamRequestSize >= 0 {
			diagnostics["upstream_request_size"] = upstreamRequestSize
		}
		if usedChannels := c.GetStringSlice("use_channel"); len(usedChannels) > 0 {
			adminInfo["retry_chain"] = append([]string(nil), usedChannels...)
		}
	}
	if nodeName := strings.TrimSpace(common.NodeName); nodeName != "" {
		diagnostics["node"] = nodeName
	}
	if firstResponse, ok := other["frt"].(float64); ok && firstResponse >= 0 {
		diagnostics["first_response_ms"] = firstResponse
	}
	if snapshot := channelSnapshot(channelId); snapshot != nil {
		for key, value := range snapshot {
			adminInfo[key] = value
		}
		if aggregateName := routePoolAggregateName(c); aggregateName != "" {
			adminInfo["surface_channel_name"] = aggregateName
		} else if routePool, ok := diagnostics["route_pool_name"].(string); ok && strings.TrimSpace(routePool) != "" {
			adminInfo["surface_channel_name"] = strings.TrimSpace(routePool)
		} else if aggregateName, ok := snapshot["aggregate_name"].(string); ok && aggregateName != "" {
			adminInfo["surface_channel_name"] = aggregateName
		} else {
			adminInfo["surface_channel_name"] = snapshot["actual_channel_name"]
		}
	}
	if len(diagnostics) > 0 {
		other["diagnostics"] = diagnostics
	}
	setting := operation_setting.GetLogDiagnosticSettingSnapshot()
	if setting.RecordHeaders {
		if headers := collectDiagnosticHeaders(c); len(headers) > 0 {
			adminInfo["request_headers"] = headers
		}
	}
	if len(adminInfo) == 0 {
		delete(other, "admin_info")
	}
	return other
}

// logIPForStorage applies the global diagnostic switch. The historical
// per-user flag remains in the compatibility schema but no longer controls
// request or error log collection.
func logIPForStorage(c *gin.Context) string {
	if c == nil || !operation_setting.GetLogDiagnosticSettingSnapshot().RecordIP {
		return ""
	}
	return c.ClientIP()
}

func adminLogSurfaceName(log *Log, other map[string]interface{}) string {
	if adminInfo, ok := other["admin_info"].(map[string]interface{}); ok {
		for _, key := range []string{"surface_channel_name", "aggregate_name", "actual_channel_name"} {
			if name, ok := adminInfo[key].(string); ok && strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name)
			}
		}
	}
	if log == nil {
		return ""
	}
	return strings.TrimSpace(log.ChannelName)
}

// sanitizeAdminSelfLogInfo keeps the user-scoped administrator view limited to
// model-routing fields. The global administrator log endpoint returns the
// complete diagnostic snapshot separately; personal log views must not expose
// unrelated audit, header, IP, or quota internals merely because the viewer is
// an administrator.
func sanitizeAdminSelfLogInfo(other map[string]interface{}) {
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		return
	}
	allowed := map[string]struct{}{
		"is_model_mapped":     {},
		"upstream_model_name": {},
		"po":                  {},
	}
	filtered := make(map[string]interface{}, len(allowed))
	for key := range allowed {
		if value, exists := adminInfo[key]; exists {
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		delete(other, "admin_info")
		return
	}
	other["admin_info"] = filtered
}

const logModelNameScopeRequested = "requested"

// withRequestedModelNameScope marks model_name as the client-requested model.
// The marker is written only at the final server-side log boundary, so older
// rows without it are treated as having unknown provenance in user views.
func withRequestedModelNameScope(other map[string]interface{}, modelName string) map[string]interface{} {
	if modelName == "" {
		return other
	}

	scopedOther := make(map[string]interface{}, len(other)+1)
	for key, value := range other {
		scopedOther[key] = value
	}
	adminInfo := map[string]interface{}{}
	if existingAdminInfo, ok := other["admin_info"].(map[string]interface{}); ok {
		adminInfo = make(map[string]interface{}, len(existingAdminInfo)+1)
		for key, value := range existingAdminInfo {
			adminInfo[key] = value
		}
	}
	adminInfo["model_name_scope"] = logModelNameScopeRequested
	scopedOther["admin_info"] = adminInfo
	return scopedOther
}

func hasRequestedModelNameScope(other map[string]interface{}) bool {
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		return false
	}
	modelNameScope, ok := adminInfo["model_name_scope"].(string)
	return ok && modelNameScope == logModelNameScopeRequested
}

func applyUserVisibleModelFilter(tx *gorm.DB, columnPrefix string, modelName string) (*gorm.DB, error) {
	if modelName == "" {
		return tx, nil
	}
	if strings.Contains(modelName, "%") {
		return nil, errors.New("model filter is not available")
	}

	// Requiring the server-written provenance marker prevents historical rows
	// whose raw model_name was an upstream route from matching a public name.
	markerPattern := `%"model_name_scope":"` + logModelNameScopeRequested + `"%`
	markerCondition, markerPattern, err := buildLogLikeCondition(columnPrefix+"other", markerPattern)
	if err != nil {
		return nil, err
	}
	return tx.Where(columnPrefix+"model_name = ?", modelName).
		Where(markerCondition, markerPattern), nil
}

func clickHouseLogOrder(prefix string) string {
	return prefix + "created_at desc, " + prefix + "request_id desc"
}

func assignDisplayLogIds(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].Id = startIdx + i + 1
	}
}

func isASCIIAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9'
}

func blocksModelReferenceBoundary(content string, adjacentIndex int, direction int) bool {
	if adjacentIndex < 0 || adjacentIndex >= len(content) {
		return false
	}
	adjacent := content[adjacentIndex]
	if isASCIIAlphaNumeric(adjacent) || adjacent == '_' || adjacent == '-' {
		return true
	}
	if adjacent != '.' {
		return false
	}
	beyondIndex := adjacentIndex + direction
	return beyondIndex >= 0 && beyondIndex < len(content) && isASCIIAlphaNumeric(content[beyondIndex])
}

func replaceExactModelReferences(content string, upstreamModelName string, publicModelName string) string {
	if content == "" || upstreamModelName == "" {
		return content
	}

	type matchRange struct {
		start int
		end   int
	}
	matches := make([]matchRange, 0, 1)
	for searchFrom := 0; searchFrom < len(content); {
		relativeStart := strings.Index(content[searchFrom:], upstreamModelName)
		if relativeStart == -1 {
			break
		}
		start := searchFrom + relativeStart
		end := start + len(upstreamModelName)
		leftBoundary := !blocksModelReferenceBoundary(content, start-1, -1)
		rightBoundary := !blocksModelReferenceBoundary(content, end, 1)
		if leftBoundary && rightBoundary {
			matches = append(matches, matchRange{start: start, end: end})
		}
		searchFrom = end
	}
	if len(matches) == 0 {
		return content
	}

	var sanitized strings.Builder
	sanitized.Grow(len(content))
	lastEnd := 0
	for _, match := range matches {
		sanitized.WriteString(content[lastEnd:match.start])
		sanitized.WriteString(publicModelName)
		lastEnd = match.end
	}
	sanitized.WriteString(content[lastEnd:])
	return sanitized.String()
}

func containsFoldedModelReference(content string, modelName string) bool {
	if content == "" || modelName == "" || len(content) < len(modelName) {
		return false
	}
	for start := 0; start+len(modelName) <= len(content); start++ {
		end := start + len(modelName)
		if !strings.EqualFold(content[start:end], modelName) {
			continue
		}
		leftBlocked := blocksSensitiveModelReferenceBoundary(content, start-1, -1)
		rightBlocked := blocksSensitiveModelReferenceBoundary(content, end, 1)
		if !leftBlocked && !rightBlocked {
			return true
		}
	}
	return false
}

func blocksSensitiveModelReferenceBoundary(content string, adjacentIndex int, direction int) bool {
	if adjacentIndex < 0 || adjacentIndex >= len(content) {
		return false
	}
	adjacent := content[adjacentIndex]
	if isASCIIAlphaNumeric(adjacent) {
		return true
	}
	if adjacent != '.' {
		return false
	}
	beyondIndex := adjacentIndex + direction
	return direction > 0 && beyondIndex >= 0 && beyondIndex < len(content) &&
		content[beyondIndex] >= '0' && content[beyondIndex] <= '9'
}

func hasPercentEscape(value string) bool {
	isHex := func(char byte) bool {
		return char >= '0' && char <= '9' ||
			char >= 'a' && char <= 'f' ||
			char >= 'A' && char <= 'F'
	}
	for i := 0; i+2 < len(value); i++ {
		if value[i] == '%' && isHex(value[i+1]) && isHex(value[i+2]) {
			return true
		}
	}
	return false
}

func containsObfuscatedModelReference(content string, upstreamModelName string) bool {
	canonical := content
	for range 4 {
		if containsFoldedModelReference(canonical, upstreamModelName) {
			return true
		}
		changed := false
		if hasPercentEscape(canonical) {
			unescaped, err := url.PathUnescape(canonical)
			if err != nil || unescaped == canonical {
				return true
			}
			canonical = unescaped
			changed = true
		}
		unescaped := html.UnescapeString(canonical)
		if unescaped != canonical {
			canonical = unescaped
			changed = true
		}
		if !changed {
			break
		}
	}
	if containsFoldedModelReference(canonical, upstreamModelName) ||
		hasPercentEscape(canonical) ||
		html.UnescapeString(canonical) != canonical {
		return true
	}
	// Escaped JSON/path fragments cannot be proven safe without knowing the
	// upstream's encoding convention, so routed diagnostics containing them
	// remain fail-closed.
	return strings.Contains(canonical, `\`)
}

func sanitizeRoutedErrorText(content string, upstreamModelNames []string, publicModelName string) (string, bool) {
	requestedModelPlaceholder := "\x00"
	for {
		placeholderConflict := strings.Contains(content, requestedModelPlaceholder)
		placeholderConflict = placeholderConflict || strings.Contains(publicModelName, requestedModelPlaceholder)
		for _, upstreamModelName := range upstreamModelNames {
			placeholderConflict = placeholderConflict || strings.Contains(upstreamModelName, requestedModelPlaceholder)
		}
		if !placeholderConflict {
			break
		}
		requestedModelPlaceholder += "\x00"
	}
	// Protect occurrences that are already the public requested identity before
	// replacing the shorter upstream name. This keeps sanitization idempotent
	// when, for example, public "openai/gpt-4" contains upstream "gpt-4".
	sanitized := replaceExactModelReferences(content, publicModelName, requestedModelPlaceholder)
	for _, upstreamModelName := range upstreamModelNames {
		sanitized = replaceExactModelReferences(sanitized, upstreamModelName, requestedModelPlaceholder)
	}
	for _, upstreamModelName := range upstreamModelNames {
		if containsObfuscatedModelReference(sanitized, upstreamModelName) {
			return "", false
		}
	}
	return strings.ReplaceAll(sanitized, requestedModelPlaceholder, publicModelName), true
}

func retainPublicLogDiagnostics(other map[string]interface{}) {
	diagnostics, ok := other["diagnostics"].(map[string]interface{})
	if !ok {
		delete(other, "diagnostics")
		return
	}

	publicDiagnostics := make(map[string]interface{}, 2)
	for _, field := range []string{"client", "request_protocol"} {
		value, ok := diagnostics[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		publicDiagnostics[field] = value
	}
	if len(publicDiagnostics) == 0 {
		delete(other, "diagnostics")
		return
	}
	other["diagnostics"] = publicDiagnostics
}

func formatUserLogs(logs []*Log, startIdx int, canViewModelRouting bool) {
	for i := range logs {
		if !canViewModelRouting {
			logs[i].ChannelId = 0
			logs[i].ChannelName = ""
			logs[i].UpstreamRequestId = ""
		}
		var otherMap map[string]interface{}
		otherMap, parseErr := common.StrToMap(logs[i].Other)
		if !canViewModelRouting && (parseErr != nil || len(otherMap) == 0) {
			// Empty or malformed historical metadata cannot prove that model_name
			// is the client-requested identity, so omit both uncertain fields.
			logs[i].ModelName = ""
			if logs[i].Type == LogTypeError {
				logs[i].Content = ""
			}
			logs[i].Other = common.MapToJsonStr(map[string]interface{}{})
			continue
		}
		if otherMap != nil {
			if canViewModelRouting {
				logs[i].ChannelName = adminLogSurfaceName(logs[i], otherMap)
				sanitizeAdminSelfLogInfo(otherMap)
				retainPublicLogDiagnostics(otherMap)
				delete(otherMap, "audit_info")
				delete(otherMap, "stream_status")
				delete(otherMap, "request_conversion")
				delete(otherMap, "request_path")
			}
			modelNameIsRequested := hasRequestedModelNameScope(otherMap)
			modelRoutingChecked := false
			upstreamModelNames := make([]string, 0, 2)
			if upstreamModelName, ok := otherMap["upstream_model_name"].(string); ok && upstreamModelName != "" {
				upstreamModelNames = append(upstreamModelNames, upstreamModelName)
			}
			if !canViewModelRouting {
				if adminInfo, ok := otherMap["admin_info"].(map[string]interface{}); ok {
					modelRoutingChecked, _ = adminInfo["model_routing_checked"].(bool)
					if upstreamModelName, ok := adminInfo["upstream_model_name"].(string); ok && upstreamModelName != "" {
						if len(upstreamModelNames) == 0 || upstreamModelNames[0] != upstreamModelName {
							upstreamModelNames = append(upstreamModelNames, upstreamModelName)
						}
					}
				}
			}
			if !canViewModelRouting {
				// Remove administrator-only diagnostics and historical top-level
				// channel fields. Frontend hiding is only a second line of defense.
				delete(otherMap, "admin_info")
				delete(otherMap, "audit_info")
				delete(otherMap, "stream_status")
				delete(otherMap, "channel_id")
				delete(otherMap, "channel_name")
				delete(otherMap, "request_conversion")
				delete(otherMap, "request_path")
				retainPublicLogDiagnostics(otherMap)
				if !modelNameIsRequested {
					// Historical rows may contain the routed upstream model in
					// model_name. Without server-written provenance it is unsafe to
					// expose the value, even when it matches a current public model.
					logs[i].ModelName = ""
				}
				// Historical logs stored model-routing details at the top level.
				delete(otherMap, "is_model_mapped")
				delete(otherMap, "upstream_model_name")
				// Historical parameter-override audits may contain the routed model.
				delete(otherMap, "po")
				if logs[i].Type == LogTypeError {
					if !modelNameIsRequested || !modelRoutingChecked {
						// Historical rows and writers that did not explicitly complete
						// routing analysis cannot prove their diagnostics are public-safe.
						logs[i].Content = ""
						delete(otherMap, "error_code")
						delete(otherMap, "error_type")
					} else if len(upstreamModelNames) > 0 {
						if sanitized, safe := sanitizeRoutedErrorText(logs[i].Content, upstreamModelNames, logs[i].ModelName); safe {
							logs[i].Content = sanitized
						} else {
							logs[i].Content = ""
						}
						for _, field := range []string{"error_code", "error_type"} {
							value, ok := otherMap[field].(string)
							if !ok {
								continue
							}
							if sanitized, safe := sanitizeRoutedErrorText(value, upstreamModelNames, logs[i].ModelName); safe {
								otherMap[field] = sanitized
							} else {
								delete(otherMap, field)
							}
						}
					}
				} else {
					for _, upstreamModelName := range upstreamModelNames {
						publicModelName := logs[i].ModelName
						modelNameMatchesUpstream := publicModelName == upstreamModelName ||
							strings.TrimSuffix(publicModelName, ratio_setting.CompactModelSuffix) == upstreamModelName &&
								strings.HasSuffix(publicModelName, ratio_setting.CompactModelSuffix)
						if modelNameMatchesUpstream {
							publicModelName = ""
						}
						logs[i].Content = replaceExactModelReferences(logs[i].Content, upstreamModelName, publicModelName)
					}
				}
				modelNameMatchesUpstream := false
				for _, upstreamModelName := range upstreamModelNames {
					if logs[i].ModelName == upstreamModelName ||
						strings.HasSuffix(logs[i].ModelName, ratio_setting.CompactModelSuffix) &&
							strings.TrimSuffix(logs[i].ModelName, ratio_setting.CompactModelSuffix) == upstreamModelName {
						modelNameMatchesUpstream = true
						break
					}
				}
				if modelNameMatchesUpstream {
					// Legacy realtime logs persisted only the routed model in model_name.
					// The requested model cannot be reconstructed safely, so omit it.
					logs[i].ModelName = ""
				}
			}
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
	}
	assignDisplayLogIds(logs, startIdx)
}

func formatAdminLogs(logs []*Log) {
	for _, log := range logs {
		if log == nil {
			continue
		}
		other, err := common.StrToMap(log.Other)
		if err != nil || other == nil {
			continue
		}
		if surfaceName := adminLogSurfaceName(log, other); surfaceName != "" {
			log.ChannelName = surfaceName
		}
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	order := "id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order(order).Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0, false)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// buildOpField 构建语言无关的操作描述（写入 Other.op）。
// 前端依据 action(稳定操作标识) + params(结构化参数) 在渲染期用 i18n 本地化展示，
// 因此不在数据库中存储自然语言句子。
func buildOpField(action string, params map[string]interface{}) map[string]interface{} {
	op := map[string]interface{}{
		"action": action,
	}
	if len(params) > 0 {
		op["params"] = params
	}
	return op
}

// RecordLoginLog 记录用户登录成功的审计日志（type=LogTypeLogin）。
// username 由调用方传入（登录流程已持有用户对象），避免额外的数据库查询。
// content 为英文兜底文本（用于导出）；action+params 供前端本地化渲染。
// extra 可携带 login_method、user_agent 等附加信息（普通用户可见）。
func RecordLoginLog(userId int, username string, content string, ip string, action string, params map[string]interface{}, extra map[string]interface{}) {
	other := map[string]interface{}{}
	for k, v := range extra {
		other[k] = v
	}
	other["op"] = buildOpField(action, params)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeLogin,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record login log: " + err.Error())
	}
}

// RecordOperationAuditLog 记录管理/高危操作审计日志（type=LogTypeManage）。
// logUserId 为日志归属者，管理审计日志应归属实际操作者；目标资源/用户放入
// action params。username 内部按 logUserId 查询。content 为英文兜底文本（供导出使用）。
// action+params 写入 Other.op，供前端本地化渲染（普通用户可见，不含敏感信息）。
// adminInfo 存放操作者身份（写入 Other.admin_info，普通用户查询时剥离）；
// auditInfo 存放路由/方法/结果等中间件兜底信息（写入 Other.audit_info，普通用户查询时剥离）。
func RecordOperationAuditLog(logUserId int, content string, ip string, action string, params map[string]interface{}, adminInfo map[string]interface{}, auditInfo map[string]interface{}) {
	username, _ := GetUsernameById(logUserId, false)
	other := map[string]interface{}{
		"op": buildOpField(action, params),
	}
	if len(adminInfo) > 0 {
		other["admin_info"] = adminInfo
	}
	if len(auditInfo) > 0 {
		other["audit_info"] = auditInfo
	}
	log := &Log{
		UserId:    logUserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeManage,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record operation audit log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     common.MapToJsonStr(other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	other = appendLogDiagnostics(c, channelId, other)
	otherStr := common.MapToJsonStr(withRequestedModelNameScope(other, modelName))
	log := &Log{
		UserId:            userId,
		Username:          username,
		CreatedAt:         common.GetTimestamp(),
		Type:              LogTypeError,
		Content:           content,
		PromptTokens:      0,
		CompletionTokens:  0,
		TokenName:         tokenName,
		ModelName:         modelName,
		Quota:             0,
		ChannelId:         channelId,
		TokenId:           tokenId,
		UseTime:           useTimeSeconds,
		IsStream:          isStream,
		Group:             group,
		Ip:                logIPForStorage(c),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		return
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	createdAt := common.GetTimestamp()
	params.Other = appendLogDiagnostics(c, params.ChannelId, params.Other)
	otherStr := common.MapToJsonStr(withRequestedModelNameScope(params.Other, params.ModelName))
	log := &Log{
		UserId:            userId,
		Username:          username,
		CreatedAt:         createdAt,
		Type:              LogTypeConsume,
		Content:           params.Content,
		PromptTokens:      params.PromptTokens,
		CompletionTokens:  params.CompletionTokens,
		TokenName:         params.TokenName,
		ModelName:         params.ModelName,
		Quota:             params.Quota,
		ChannelId:         params.ChannelId,
		TokenId:           params.TokenId,
		UseTime:           params.UseTimeSeconds,
		IsStream:          params.IsStream,
		Group:             params.Group,
		Ip:                logIPForStorage(c),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		LogQuotaData(QuotaDataLogParams{
			UserID:     userId,
			Username:   username,
			ModelName:  params.ModelName,
			ModelScope: QuotaModelScopeRequested,
			Quota:      params.Quota,
			CreatedAt:  createdAt,
			TokenUsed:  params.PromptTokens + params.CompletionTokens,
			UseGroup:   params.Group,
			TokenID:    params.TokenId,
			ChannelID:  params.ChannelId,
			NodeName:   common.NodeName,
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     map[string]interface{}
	NodeName  string // 任务发起节点；为空时回退当前节点
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: createdAt,
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(withRequestedModelNameScope(params.Other, params.ModelName)),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
	if params.LogType == LogTypeConsume && common.DataExportEnabled {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		LogQuotaData(QuotaDataLogParams{
			UserID:     params.UserId,
			Username:   username,
			ModelName:  params.ModelName,
			ModelScope: QuotaModelScopeRequested,
			Quota:      params.Quota,
			CreatedAt:  createdAt,
			UseGroup:   params.Group,
			TokenID:    params.TokenId,
			ChannelID:  params.ChannelId,
			NodeName:   nodeName,
		})
	}
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string, sortOptions ...LogSortOptions) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = resolveLogSortOptions(sortOptions).Apply(tx).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, startIdx)
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}
	formatAdminLogs(logs)

	return logs, total, err
}

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, canViewModelRouting bool, sortOptions ...LogSortOptions) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if canViewModelRouting {
		if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
			return nil, 0, err
		}
	} else if tx, err = applyUserVisibleModelFilter(tx, "logs.", modelName); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	err = resolveLogSortOptions(sortOptions).Apply(tx).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx, canViewModelRouting)
	return logs, total, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(quota), 0) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm")

	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

// SumUserUsedQuota returns self-service usage statistics. When a model filter
// is present it only includes rows whose model_name was explicitly marked as
// the client-requested identity at the server-side log boundary.
func SumUserUsedQuota(userId int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, channel int, group string) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(quota), 0) quota").Where("user_id = ?", userId)
	rpmTpmQuery := LOG_DB.Table("logs").
		Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm").
		Where("user_id = ?", userId)

	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyUserVisibleModelFilter(tx, "", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyUserVisibleModelFilter(rpmTpmQuery, "", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query user log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query user rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func CountOldLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	var total int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteOldLogBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if nil != ctx.Err() {
		return 0, ctx.Err()
	}

	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// ClickHouse DELETE is a heavy mutation that rewrites data parts, so
		// per-batch mutations would be pathologically slow. Remove all matching
		// rows in a single synchronous mutation regardless of limit; the reported
		// count lets the caller's progress loop complete in one pass.
		total, err := CountOldLog(ctx, targetTimestamp)
		if err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, nil
		}
		if err := LOG_DB.WithContext(ctx).Exec(
			"ALTER TABLE logs DELETE WHERE created_at < ? SETTINGS mutations_sync = 1",
			targetTimestamp,
		).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	result := LOG_DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
	if nil != result.Error {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
