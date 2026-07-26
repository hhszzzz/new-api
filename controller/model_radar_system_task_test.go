package controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestModelRadarSyncHandlerSettings(t *testing.T) {
	handler := modelRadarSyncHandler{}
	assert.Equal(t, model.SystemTaskTypeModelRadarSync, handler.Type())

	t.Setenv("MODEL_RADAR_SYNC_ENABLED", "false")
	assert.False(t, handler.Enabled())
	t.Setenv("MODEL_RADAR_SYNC_ENABLED", "true")
	t.Setenv("MODEL_RADAR_SYNC_INTERVAL_MINUTES", "1")
	assert.True(t, handler.Enabled())
	assert.Equal(t, 10*time.Minute, handler.Interval())
	t.Setenv("MODEL_RADAR_SYNC_INTERVAL_MINUTES", "25")
	assert.Equal(t, 25*time.Minute, handler.Interval())
}

func TestModelRadarSyncHandlerRecordsFailedResult(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-radar-task.db")), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	task, err := model.CreateSystemTask(model.SystemTaskTypeModelRadarSync, nil, nil)
	require.NoError(t, err)
	const runnerID = "model-radar-test-runner"
	claimedTask, claimed, err := model.ClaimSystemTask(
		task.ID,
		task.Type,
		runnerID,
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	modelRadarSyncHandler{}.Run(ctx, claimedTask, runnerID)

	stored, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusFailed, stored.Status)
	assert.NotEmpty(t, stored.Error)
}
