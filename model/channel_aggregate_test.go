package model

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelAggregateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-aggregates.db")), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelAggregate{}, &Channel{}))
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestChannelAggregateBaseURLInheritanceAndFallback(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	aggregate := &ChannelAggregate{Name: "dual protocol", BaseURL: "https://shared.example.com/v1/"}
	require.NoError(t, SaveChannelAggregate(aggregate))
	assert.Equal(t, "https://shared.example.com/v1", aggregate.BaseURL)

	childURL := "https://child.example.com/v1"
	children := []Channel{
		{Id: 1, Name: "inherited", Key: "key-1", AggregateId: &aggregate.Id, InheritAggregateBaseURL: true, BaseURL: &childURL},
		{Id: 2, Name: "override", Key: "key-2", AggregateId: &aggregate.Id, InheritAggregateBaseURL: false, BaseURL: &childURL},
	}
	require.NoError(t, db.Create(&children).Error)

	inherited, err := GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, "dual protocol", inherited.AggregateName)
	assert.Equal(t, "https://shared.example.com/v1", inherited.GetBaseURL())

	override, err := GetChannelById(2, true)
	require.NoError(t, err)
	assert.Equal(t, childURL, override.GetBaseURL())

	aggregate.BaseURL = ""
	require.NoError(t, SaveChannelAggregate(aggregate))
	inherited, err = GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, childURL, inherited.GetBaseURL())
}

func TestDeleteChannelAggregateDetachesChildrenAndPreservesEffectiveAddress(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	aggregate := &ChannelAggregate{Name: "shared provider", BaseURL: "https://shared.example.com"}
	require.NoError(t, SaveChannelAggregate(aggregate))

	inheritedOriginal := "https://old-child.example.com"
	overrideURL := "https://override.example.com"
	require.NoError(t, db.Create(&[]Channel{
		{Id: 10, Name: "inherited", Key: "key-1", AggregateId: &aggregate.Id, InheritAggregateBaseURL: true, BaseURL: &inheritedOriginal},
		{Id: 11, Name: "override", Key: "key-2", AggregateId: &aggregate.Id, InheritAggregateBaseURL: false, BaseURL: &overrideURL},
	}).Error)

	require.NoError(t, DeleteChannelAggregate(aggregate.Id))

	var detached []Channel
	require.NoError(t, db.Order("id asc").Find(&detached).Error)
	require.Len(t, detached, 2)
	assert.Nil(t, detached[0].AggregateId)
	assert.False(t, detached[0].InheritAggregateBaseURL)
	require.NotNil(t, detached[0].BaseURL)
	assert.Equal(t, aggregate.BaseURL, *detached[0].BaseURL)
	assert.Nil(t, detached[1].AggregateId)
	assert.False(t, detached[1].InheritAggregateBaseURL)
	require.NotNil(t, detached[1].BaseURL)
	assert.Equal(t, overrideURL, *detached[1].BaseURL)

	var aggregateCount int64
	require.NoError(t, db.Model(&ChannelAggregate{}).Count(&aggregateCount).Error)
	assert.Zero(t, aggregateCount)
}

func TestDeleteEmptyAddressAggregateKeepsChildAddress(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	aggregate := &ChannelAggregate{Name: "metadata only"}
	require.NoError(t, SaveChannelAggregate(aggregate))
	childURL := "https://child.example.com"
	require.NoError(t, db.Create(&Channel{
		Id: 20, Name: "child", Key: "key", AggregateId: &aggregate.Id,
		InheritAggregateBaseURL: true, BaseURL: &childURL,
	}).Error)

	require.NoError(t, DeleteChannelAggregate(aggregate.Id))
	child, err := GetChannelById(20, true)
	require.NoError(t, err)
	require.NotNil(t, child.BaseURL)
	assert.Equal(t, childURL, *child.BaseURL)
}
