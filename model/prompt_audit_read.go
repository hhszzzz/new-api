package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// PromptAuditReadOptions bounds metadata reads. MaxID is a table-wide insertion
// watermark, not a snapshot of mutable async status or review fields.
type PromptAuditReadOptions struct {
	Page     int
	PageSize int
	MaxID    int64
}

type PromptAuditContentVersion struct {
	ID              int64  `json:"id"`
	Kind            string `json:"kind"`
	Direction       string `json:"direction"`
	Count           int64  `json:"count"`
	FirstAt         int64  `json:"first_at"`
	LastAt          int64  `json:"last_at"`
	RedactedPreview string `json:"redacted_preview"`
}

type PromptAuditContentPage struct {
	Items    []PromptAuditContentVersion `json:"items"`
	Total    int64                       `json:"total"`
	Page     int                         `json:"page"`
	PageSize int                         `json:"page_size"`
	MaxID    int64                       `json:"max_id"`
	Summary  PromptAuditRepeat           `json:"summary"`
}

type PromptAuditRecordsPage struct {
	Items    []PromptAuditResponse `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	MaxID    int64                 `json:"max_id"`
}

type PromptAuditSessionPage struct {
	Items      []PromptAuditResponse `json:"items"`
	Total      int64                 `json:"total"`
	Page       int                   `json:"page"`
	PageSize   int                   `json:"page_size"`
	MaxID      int64                 `json:"max_id"`
	HasSession bool                  `json:"has_session"`
}

const promptAuditOutcome = "CASE WHEN status IN ('queued', 'processing', 'retry') THEN status " +
	"WHEN status = 'stored' THEN 'stored' " +
	"WHEN decision IN ('pass', 'block', 'flag', 'unavailable') THEN decision " +
	"WHEN status = 'failed' AND COALESCE(decision, '') = '' THEN 'failed' ELSE 'unknown' END"

const promptAuditContentKind = "CASE WHEN request_kind IN ('prompt', 'step') THEN 'main' " +
	"WHEN request_kind IS NULL OR request_kind = '' THEN 'unknown' ELSE request_kind END"

// CASE/SUM, including the NULL fallbacks for historical rows, works on all
// supported database versions without window functions or aggregate FILTER.
type promptAuditRepeatAggregate struct {
	ID                 int64
	RepeatCount        int64
	FirstAt            int64
	LastAt             int64
	WorstDecisionRank  int
	Blocks             int64
	Unavailable        int64
	OutcomePass        int64
	OutcomeBlock       int64
	OutcomeFlag        int64
	OutcomeUnavailable int64
	OutcomeQueued      int64
	OutcomeProcessing  int64
	OutcomeRetry       int64
	OutcomeFailed      int64
	OutcomeStored      int64
	OutcomeUnknown     int64
}

func promptAuditRepeatSelect() string {
	var sql strings.Builder
	sql.WriteString("COUNT(*) AS repeat_count, MIN(created_at) AS first_at, MAX(created_at) AS last_at, " +
		promptAuditWorstDecisionRank + " AS worst_decision_rank, " +
		"SUM(CASE WHEN COALESCE(status, '') <> 'stored' AND action = 'block' THEN 1 ELSE 0 END) AS blocks, " +
		"SUM(CASE WHEN COALESCE(status, '') <> 'stored' AND action = 'unavailable' THEN 1 ELSE 0 END) AS unavailable")
	for _, outcome := range []string{"pass", "block", "flag", "unavailable", "queued", "processing", "retry", "failed", "stored", "unknown"} {
		sql.WriteString(", SUM(CASE WHEN (" + promptAuditOutcome + ") = '" + outcome + "' THEN 1 ELSE 0 END) AS outcome_" + outcome)
	}
	return sql.String()
}

func (row promptAuditRepeatAggregate) summary() PromptAuditRepeat {
	outcomes := map[string]int64{
		"pass": row.OutcomePass, "block": row.OutcomeBlock, "flag": row.OutcomeFlag,
		"unavailable": row.OutcomeUnavailable, "queued": row.OutcomeQueued, "processing": row.OutcomeProcessing,
		"retry": row.OutcomeRetry, "failed": row.OutcomeFailed, "stored": row.OutcomeStored, "unknown": row.OutcomeUnknown,
	}
	for outcome, count := range outcomes {
		if count == 0 {
			delete(outcomes, outcome)
		}
	}
	worstDecision := ""
	if row.RepeatCount > 0 && row.WorstDecisionRank >= 0 && row.WorstDecisionRank < len(promptAuditDecisionsByRank) {
		worstDecision = promptAuditDecisionsByRank[row.WorstDecisionRank]
	}
	return PromptAuditRepeat{
		Count: row.RepeatCount, FirstAt: row.FirstAt, LastAt: row.LastAt,
		WorstDecision: worstDecision, Blocks: row.Blocks, Unavailable: row.Unavailable, OutcomeCounts: outcomes,
	}
}

func resolvePromptAuditReadOptions(options *PromptAuditReadOptions) error {
	options.Page = max(options.Page, 1)
	if options.PageSize < 1 {
		options.PageSize = 20
	}
	options.PageSize = min(options.PageSize, 200)
	if options.MaxID <= 0 {
		return DB.Model(&PromptAudit{}).Select("COALESCE(MAX(id), 0)").Scan(&options.MaxID).Error
	}
	return nil
}

// A representative identifies a question even if that particular request does
// not match the current decision/model filter. All rows still obey that filter,
// including any explicitly supplied IDs and group IDs.
func promptAuditQuestionQuery(filter PromptAuditFilter, representativeID int64, options *PromptAuditReadOptions) (*gorm.DB, error) {
	if err := resolvePromptAuditReadOptions(options); err != nil {
		return nil, err
	}
	anchor := PromptAuditFilter{GroupIDs: []int64{representativeID}}
	if err := resolvePromptAuditGroups(&anchor); err != nil {
		return nil, err
	}
	if len(anchor.groups) == 0 || anchor.groups[0].ID > options.MaxID {
		return nil, ErrPromptAuditNotFound
	}
	if err := resolvePromptAuditGroups(&filter); err != nil {
		return nil, err
	}
	filter.MaxID = options.MaxID
	condition, args := promptAuditGroupCondition(anchor.groups)
	return applyPromptAuditFilter(DB.Model(&PromptAudit{}), filter).Where(condition, args...), nil
}

// ListPromptAuditGroupContent groups identical audit snapshots, never messages
// within one snapshot. Hashless historical requests remain individual versions.
func ListPromptAuditGroupContent(filter PromptAuditFilter, representativeID int64, options PromptAuditReadOptions) (PromptAuditContentPage, error) {
	result := PromptAuditContentPage{Items: []PromptAuditContentVersion{}, Summary: PromptAuditRepeat{OutcomeCounts: map[string]int64{}}}
	query, err := promptAuditQuestionQuery(filter, representativeID, &options)
	if err != nil {
		return result, err
	}
	result.Page, result.PageSize, result.MaxID = options.Page, options.PageSize, options.MaxID
	var aggregate promptAuditRepeatAggregate
	if err := query.Session(&gorm.Session{}).Select(promptAuditRepeatSelect()).Scan(&aggregate).Error; err != nil {
		return result, err
	}
	result.Summary = aggregate.summary()
	groupColumns := promptAuditContentKind + ", COALESCE(direction, ''), COALESCE(prompt_hash, ''), " +
		"CASE WHEN COALESCE(prompt_hash, '') = '' THEN id ELSE 0 END"
	grouped := query.Session(&gorm.Session{}).
		Select("MIN(id) AS id, " + promptAuditContentKind + " AS kind, COALESCE(direction, '') AS direction, " +
			"COUNT(*) AS count, MIN(created_at) AS first_at, MAX(created_at) AS last_at").
		Group(groupColumns)
	if err := DB.Table("(?) AS prompt_audit_content_versions", grouped).Count(&result.Total).Error; err != nil {
		return result, err
	}
	if result.Total == 0 || int64(options.Page-1) > result.Total/int64(options.PageSize) {
		return result, nil
	}
	if err := grouped.Order("MIN(created_at) ASC, MIN(id) ASC").Limit(options.PageSize).
		Offset((options.Page - 1) * options.PageSize).Scan(&result.Items).Error; err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, nil
	}
	ids := make([]int64, 0, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.ID)
	}
	var previews []PromptAudit
	if err := DB.Select("id, direction, content_manifest, redacted_preview").Where("id IN ?", ids).Find(&previews).Error; err != nil {
		return result, err
	}
	byID := make(map[int64]string, len(previews))
	for _, preview := range previews {
		byID[preview.ID] = preview.ToResponse(false).RedactedPreview
	}
	for i := range result.Items {
		result.Items[i].RedactedPreview = byID[result.Items[i].ID]
	}
	return result, nil
}

// ListPromptAuditGroupRecords optionally narrows to a content version through a
// visible member's ID. Hash, normalized kind, direction and question/user scope
// are resolved on the server; no arbitrary public hash filter is needed.
func ListPromptAuditGroupRecords(filter PromptAuditFilter, representativeID, contentID int64, options PromptAuditReadOptions) (PromptAuditRecordsPage, error) {
	result := PromptAuditRecordsPage{Items: []PromptAuditResponse{}}
	query, err := promptAuditQuestionQuery(filter, representativeID, &options)
	if err != nil {
		return result, err
	}
	result.Page, result.PageSize, result.MaxID = options.Page, options.PageSize, options.MaxID
	if contentID != 0 {
		var content struct {
			ID         int64
			Kind       string
			Direction  string
			PromptHash string
		}
		if err := query.Session(&gorm.Session{}).
			Select("id, "+promptAuditContentKind+" AS kind, COALESCE(direction, '') AS direction, prompt_hash").
			Where("id = ?", contentID).Scan(&content).Error; err != nil {
			return result, err
		}
		if content.ID == 0 {
			return result, ErrPromptAuditNotFound
		}
		if content.PromptHash == "" {
			query = query.Where("id = ?", content.ID)
		} else {
			query = query.Where("prompt_hash = ? AND ("+promptAuditContentKind+") = ? AND COALESCE(direction, '') = ?",
				content.PromptHash, content.Kind, content.Direction)
		}
	}
	if err := query.Session(&gorm.Session{}).Count(&result.Total).Error; err != nil {
		return result, err
	}
	if result.Total == 0 || int64(options.Page-1) > result.Total/int64(options.PageSize) {
		return result, nil
	}
	var audits []PromptAudit
	if err := query.Session(&gorm.Session{}).Omit(promptAuditListingOmittedColumns...).Order("id ASC").
		Limit(options.PageSize).Offset((options.Page - 1) * options.PageSize).Find(&audits).Error; err != nil {
		return result, err
	}
	for _, audit := range audits {
		result.Items = append(result.Items, audit.ToResponse(false))
	}
	return result, nil
}

// ListPromptAuditSessionQuestions deliberately ignores list filters other than
// the time interval. Session identity is always resolved from a stored event and
// includes its user; absent sessions never broaden to an unscoped query.
func ListPromptAuditSessionQuestions(representativeID, startTime, endTime int64, options PromptAuditReadOptions) (PromptAuditSessionPage, error) {
	result := PromptAuditSessionPage{Items: []PromptAuditResponse{}}
	if err := resolvePromptAuditReadOptions(&options); err != nil {
		return result, err
	}
	result.Page, result.PageSize, result.MaxID = options.Page, options.PageSize, options.MaxID
	var anchor PromptAudit
	if err := DB.Select("id, user_id, session_key").Where("id = ? AND id <= ?", representativeID, options.MaxID).Take(&anchor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return result, ErrPromptAuditNotFound
		}
		return result, err
	}
	if anchor.SessionKey == "" {
		return result, nil
	}
	result.HasSession = true
	query := applyPromptAuditFilter(DB.Model(&PromptAudit{}), PromptAuditFilter{StartTime: startTime, EndTime: endTime, MaxID: options.MaxID}).
		Where("user_id = ? AND session_key = ?", anchor.UserID, anchor.SessionKey)
	rows, total, err := listPromptAuditRepeatRows(query, options.Page, options.PageSize, true)
	if err != nil {
		return result, err
	}
	result.Total = total
	for _, row := range rows {
		response := row.Audit.ToResponse(false)
		response.Repeat = &row.Repeat
		result.Items = append(result.Items, response)
	}
	return result, nil
}
