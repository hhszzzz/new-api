package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const modelRadarSnapshotID = 1

// ModelRadarSnapshot stores the latest validated Codex Radar payload. The
// upstream payload already contains its 48-hour history, so only one row is
// retained.
type ModelRadarSnapshot struct {
	ID              int    `json:"id" gorm:"primaryKey;autoIncrement:false"`
	SchemaVersion   int    `json:"schema_version" gorm:"not null"`
	Payload         []byte `json:"-" gorm:"not null"`
	SourceUpdatedAt int64  `json:"source_updated_at" gorm:"bigint;not null"`
	AlertsUpdatedAt int64  `json:"alerts_updated_at" gorm:"bigint;not null"`
	FetchedAt       int64  `json:"fetched_at" gorm:"bigint;not null"`
}

func (ModelRadarSnapshot) TableName() string {
	return "model_radar_snapshots"
}

func SaveModelRadarSnapshot(ctx context.Context, snapshot *ModelRadarSnapshot) error {
	if snapshot == nil || len(snapshot.Payload) == 0 {
		return errors.New("model radar snapshot payload is required")
	}
	snapshot.ID = modelRadarSnapshotID
	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"schema_version",
			"payload",
			"source_updated_at",
			"alerts_updated_at",
			"fetched_at",
		}),
	}).Create(snapshot).Error
}

func GetModelRadarSnapshot(ctx context.Context) (*ModelRadarSnapshot, error) {
	var snapshot ModelRadarSnapshot
	err := DB.WithContext(ctx).First(&snapshot, "id = ?", modelRadarSnapshotID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}
