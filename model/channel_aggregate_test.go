package model

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPaginatedTopLevelChannelsKeepsAggregateChildrenOnOnePage(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	firstAggregate := &ChannelAggregate{Name: "First aggregate"}
	secondAggregate := &ChannelAggregate{Name: "Second aggregate"}
	require.NoError(t, SaveChannelAggregate(firstAggregate))
	require.NoError(t, SaveChannelAggregate(secondAggregate))
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1, Name: "first-high", Key: "secret-1", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(100)), AggregateId: &firstAggregate.Id},
		{Id: 2, Name: "first-low", Key: "secret-2", Status: common.ChannelStatusManuallyDisabled, Priority: common.GetPointer(int64(10)), AggregateId: &firstAggregate.Id},
		{Id: 3, Name: "second-high", Key: "secret-3", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(90)), AggregateId: &secondAggregate.Id},
		{Id: 4, Name: "second-low", Key: "secret-4", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(80)), AggregateId: &secondAggregate.Id},
		{Id: 5, Name: "standalone", Key: "secret-5", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(95))},
	}).Error)
	sortOptions := NewChannelSortOptions("priority", "desc", false)

	firstPage, total, err := GetPaginatedTopLevelChannels(db.Model(&Channel{}), 0, 2, sortOptions)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, firstPage, 3)
	assert.Equal(t, []int{1, 2, 5}, []int{firstPage[0].Id, firstPage[1].Id, firstPage[2].Id})
	assert.Equal(t, "First aggregate", firstPage[0].AggregateName)
	assert.Equal(t, "First aggregate", firstPage[1].AggregateName)
	assert.Empty(t, firstPage[0].Key)
	assert.Empty(t, firstPage[1].Key)
	assert.Empty(t, firstPage[2].Key)

	secondPage, total, err := GetPaginatedTopLevelChannels(db.Model(&Channel{}), 2, 2, sortOptions)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, secondPage, 2)
	assert.Equal(t, []int{3, 4}, []int{secondPage[0].Id, secondPage[1].Id})
	assert.Equal(t, "Second aggregate", secondPage[0].AggregateName)
	assert.Equal(t, "Second aggregate", secondPage[1].AggregateName)

	enabledOnly, total, err := GetPaginatedTopLevelChannels(
		db.Model(&Channel{}).Where("status = ?", common.ChannelStatusEnabled),
		0,
		3,
		sortOptions,
	)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Equal(t, []int{1, 5, 3, 4}, []int{enabledOnly[0].Id, enabledOnly[1].Id, enabledOnly[2].Id, enabledOnly[3].Id})
	for _, channel := range enabledOnly {
		assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	}
}

func setupChannelAggregateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-aggregates.db")), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelAggregate{}, &Channel{}, &Ability{}))
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

func TestChannelAggregateRejectsBaseURLWithQueryOrFragment(t *testing.T) {
	setupChannelAggregateTestDB(t)

	for _, baseURL := range []string{
		"https://shared.example.com/v1?api-version=1",
		"https://shared.example.com/v1?",
		"https://shared.example.com/v1#fragment",
	} {
		t.Run(baseURL, func(t *testing.T) {
			err := SaveChannelAggregate(&ChannelAggregate{Name: "invalid address", BaseURL: baseURL})
			require.ErrorContains(t, err, "valid HTTP or HTTPS URL")
		})
	}
}

func TestBatchInsertChannelsRejectsMissingAggregate(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	missingAggregateId := 999

	err := BatchInsertChannels([]Channel{
		{
			Id:                      30,
			Name:                    "dangling child",
			Key:                     "key",
			AggregateId:             &missingAggregateId,
			InheritAggregateBaseURL: true,
		},
	})

	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var count int64
	require.NoError(t, db.Model(&Channel{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestChannelInsertRejectsMissingAggregate(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	missingAggregateId := 999
	channel := &Channel{
		Id:                      31,
		Name:                    "dangling clone",
		Key:                     "key",
		AggregateId:             &missingAggregateId,
		InheritAggregateBaseURL: true,
	}

	err := channel.Insert()

	require.ErrorContains(t, err, "channel aggregate 999 does not exist")
	var count int64
	require.NoError(t, db.Model(&Channel{}).Count(&count).Error)
	assert.Zero(t, count)
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

func TestStaleChannelUpdateCannotRestoreDeletedAggregateLink(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	aggregate := &ChannelAggregate{Name: "soon deleted", BaseURL: "https://shared.example.com"}
	require.NoError(t, SaveChannelAggregate(aggregate))
	childURL := "https://child.example.com"
	stale := &Channel{
		Id:                      40,
		Name:                    "stale child",
		Key:                     "key",
		AggregateId:             &aggregate.Id,
		InheritAggregateBaseURL: true,
		BaseURL:                 &childURL,
	}
	require.NoError(t, db.Create(stale).Error)
	require.NoError(t, DeleteChannelAggregate(aggregate.Id))

	stale.Name = "updated after detach"
	require.NoError(t, stale.Update())

	var persisted Channel
	require.NoError(t, db.First(&persisted, stale.Id).Error)
	assert.Nil(t, persisted.AggregateId)
	assert.False(t, persisted.InheritAggregateBaseURL)
}

func TestChannelUpdateWithAggregateLinkRollsBackFieldsWhenAbilitiesFail(t *testing.T) {
	db := setupChannelAggregateTestDB(t)
	originalAggregate := &ChannelAggregate{Name: "original"}
	updatedAggregate := &ChannelAggregate{Name: "updated"}
	require.NoError(t, SaveChannelAggregate(originalAggregate))
	require.NoError(t, SaveChannelAggregate(updatedAggregate))

	channel := &Channel{
		Id:          41,
		Name:        "before",
		Key:         "key",
		Models:      "model-before",
		Group:       "default",
		Status:      common.ChannelStatusEnabled,
		AggregateId: &originalAggregate.Id,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(db))

	callbackName := "test:fail_channel_ability_create"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "abilities" {
			tx.AddError(errors.New("forced ability failure"))
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(callbackName)
	})

	channel.Name = "after"
	channel.Models = "model-after"
	channel.AggregateId = &updatedAggregate.Id
	err := channel.UpdateWithAggregateLink()
	require.ErrorContains(t, err, "forced ability failure")

	var persisted Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	assert.Equal(t, "before", persisted.Name)
	assert.Equal(t, "model-before", persisted.Models)
	require.NotNil(t, persisted.AggregateId)
	assert.Equal(t, originalAggregate.Id, *persisted.AggregateId)

	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.Equal(t, "model-before", abilities[0].Model)
}
