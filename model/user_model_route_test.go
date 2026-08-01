package model

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserModelRouteTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "routes.db")), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(
		&User{},
		&UserGroupMembership{},
		&Channel{},
		&ChannelAggregate{},
		&Ability{},
		&UserModelRoute{},
		&UserModelRouteGroup{},
		&UserModelRouteExecutionGroup{},
		&UserModelRouteChannel{},
	))
	require.NoError(t, db.Create(&User{Id: 1, Username: "route-user", Group: "default"}).Error)
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestGetUserModelRouteCandidatesOnlyReturnsEnabledCapabilities(t *testing.T) {
	setupUserModelRouteTestDB(t)
	priority := int64(20)
	require.NoError(t, DB.Create(&[]Channel{
		{Id: 10, Name: "primary", Key: "key-1", Type: 1, Status: common.ChannelStatusEnabled},
		{Id: 11, Name: "disabled", Key: "key-2", Type: 1, Status: common.ChannelStatusManuallyDisabled},
		{Id: 12, Name: "secondary", Key: "key-3", Type: 14, Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "internal", Model: "target-b", ChannelId: 10, Enabled: true, Priority: &priority, Weight: 30},
		{Group: "internal", Model: "target-b", ChannelId: 11, Enabled: true},
		{Group: "internal", Model: "target-a", ChannelId: 12, Enabled: true},
		{Group: "internal", Model: "gpt-4-gizmo-*", ChannelId: 12, Enabled: true},
		{Group: "internal", Model: "target-disabled", ChannelId: 12, Enabled: false},
	}).Error)

	models, err := GetUserModelRouteTargetModels()
	require.NoError(t, err)
	assert.Equal(t, []string{"target-a", "target-b"}, models)

	channels, err := GetUserModelRouteCandidateChannels("internal", "target-b")
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, 10, channels[0].Id)
	assert.Equal(t, "primary", channels[0].Name)
	assert.Equal(t, 1, channels[0].Type)
	assert.Equal(t, priority, *channels[0].Priority)
	assert.Equal(t, uint(30), channels[0].Weight)

	channels, err = GetUserModelRouteCandidateChannels("internal", "gpt-4-gizmo-*")
	require.NoError(t, err)
	assert.Empty(t, channels)
}

func TestGetUserModelRouteExecutionGroupCountsFallBackPerGroup(t *testing.T) {
	setupUserModelRouteTestDB(t)
	require.NoError(t, DB.Create(&[]Channel{
		{Id: 10, Name: "exact", Key: "key-1", Type: 1, Status: common.ChannelStatusEnabled},
		{Id: 12, Name: "wildcard", Key: "key-2", Type: 1, Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "exact-group", Model: "gpt-4-gizmo-demo", ChannelId: 10, Enabled: true},
		{Group: "fallback-group", Model: "gpt-4-gizmo-*", ChannelId: 12, Enabled: true},
	}).Error)

	counts, err := GetUserModelRouteExecutionGroupChannelCounts("gpt-4-gizmo-demo")
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{
		"exact-group":    1,
		"fallback-group": 1,
	}, counts)
}

func TestGetUserModelRouteCandidateChannelsForGroupsReturnsOrderedUnion(t *testing.T) {
	setupUserModelRouteTestDB(t)
	require.NoError(t, DB.Create(&[]Channel{
		{Id: 10, Name: "default-only", Key: "key-1", Type: 1, Status: common.ChannelStatusEnabled},
		{Id: 11, Name: "vip-only", Key: "key-2", Type: 1, Status: common.ChannelStatusEnabled},
		{Id: 12, Name: "shared", Key: "key-3", Type: 1, Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "target-model", ChannelId: 10, Enabled: true},
		{Group: "default", Model: "target-model", ChannelId: 12, Enabled: true},
		{Group: "vip", Model: "target-model", ChannelId: 11, Enabled: true},
		{Group: "vip", Model: "target-model", ChannelId: 12, Enabled: true},
	}).Error)

	channels, err := GetUserModelRouteCandidateChannelsForGroups(
		[]string{"vip", "default", "vip"},
		"target-model",
	)
	require.NoError(t, err)
	require.Len(t, channels, 3)
	assert.Equal(t, []int{11, 12, 10}, []int{channels[0].Id, channels[1].Id, channels[2].Id})
	assert.Equal(t, []string{"vip"}, channels[0].ExecutionGroups)
	assert.Equal(t, []string{"vip", "default"}, channels[1].ExecutionGroups)
	assert.Equal(t, []string{"default"}, channels[2].ExecutionGroups)
}

func newTestUserModelRoute(allGroups bool, groups ...string) *UserModelRoute {
	return &UserModelRoute{
		UserId:         1,
		SourceModel:    "gpt-5.4",
		TargetModel:    "gpt-5.5",
		AllGroups:      allGroups,
		Groups:         groups,
		ExecutionGroup: "default",
		Enabled:        true,
		ChannelIds:     []int{10},
	}
}

func insertUserModelRouteChannel(t *testing.T, id int, status int, modelName string) *Channel {
	t.Helper()
	channel := &Channel{
		Id:     id,
		Name:   "route-channel",
		Key:    "route-key",
		Type:   1,
		Status: status,
		Models: modelName,
		Group:  "default",
	}
	require.NoError(t, channel.Insert())
	return channel
}

func TestChannelDeleteRemovesAbilitiesAndDisablesEmptyUserModelRoute(t *testing.T) {
	setupUserModelRouteTestDB(t)
	channel := insertUserModelRouteChannel(t, 10, common.ChannelStatusEnabled, "gpt-5.5")
	route := newTestUserModelRoute(true)
	require.NoError(t, SaveUserModelRoute(route))

	require.NoError(t, channel.Delete())

	var channelCount, abilityCount, routeChannelCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Count(&channelCount).Error)
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
	require.NoError(t, DB.Model(&UserModelRouteChannel{}).Where("route_id = ?", route.Id).Count(&routeChannelCount).Error)
	assert.Zero(t, channelCount)
	assert.Zero(t, abilityCount)
	assert.Zero(t, routeChannelCount)

	var persistedRoute UserModelRoute
	require.NoError(t, DB.First(&persistedRoute, route.Id).Error)
	assert.False(t, persistedRoute.Enabled)
	sources, err := GetEnabledUserModelRouteSources(route.UserId, []string{"default"})
	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestBatchDeletePreservesRouteWhenAnotherSelectedChannelRemains(t *testing.T) {
	setupUserModelRouteTestDB(t)
	insertUserModelRouteChannel(t, 10, common.ChannelStatusEnabled, "gpt-5.5")
	insertUserModelRouteChannel(t, 11, common.ChannelStatusEnabled, "gpt-5.5")
	route := newTestUserModelRoute(true)
	route.ChannelIds = []int{10, 11}
	require.NoError(t, SaveUserModelRoute(route))

	deleted, err := BatchDeleteChannels([]int{10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	routes, err := GetUserModelRoutes(route.UserId)
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.True(t, routes[0].Enabled)
	assert.Equal(t, []int{11}, routes[0].ChannelIds)
}

func TestChannelDeleteRollsBackWhenAbilityCleanupFails(t *testing.T) {
	setupUserModelRouteTestDB(t)
	channel := insertUserModelRouteChannel(t, 10, common.ChannelStatusEnabled, "gpt-5.5")
	route := newTestUserModelRoute(true)
	require.NoError(t, SaveUserModelRoute(route))

	callbackName := "test:fail_channel_ability_delete"
	require.NoError(t, DB.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "abilities" {
			tx.AddError(errors.New("forced ability delete failure"))
		}
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Delete().Remove(callbackName)
	})

	err := channel.Delete()
	require.ErrorContains(t, err, "forced ability delete failure")

	var channelCount, abilityCount, routeChannelCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Count(&channelCount).Error)
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
	require.NoError(t, DB.Model(&UserModelRouteChannel{}).Where("route_id = ?", route.Id).Count(&routeChannelCount).Error)
	assert.Equal(t, int64(1), channelCount)
	assert.Equal(t, int64(1), abilityCount)
	assert.Equal(t, int64(1), routeChannelCount)
}

func TestChannelDeleteSupportsSchemasWithoutUserModelRouteTables(t *testing.T) {
	setupUserModelRouteTestDB(t)
	require.NoError(t, DB.Migrator().DropTable(&UserModelRouteChannel{}, &UserModelRoute{}))
	channel := insertUserModelRouteChannel(t, 10, common.ChannelStatusEnabled, "gpt-5.5")

	require.NoError(t, channel.Delete())

	var channelCount, abilityCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Count(&channelCount).Error)
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
	assert.Zero(t, channelCount)
	assert.Zero(t, abilityCount)
}

func TestReplaceUserModelRoutesSwapsWholeSetAtomically(t *testing.T) {
	setupUserModelRouteTestDB(t)

	existing := newTestUserModelRoute(true)
	require.NoError(t, SaveUserModelRoute(existing))

	first := newTestUserModelRoute(false, "premium")
	first.SourceModel = "gpt-5.5"
	first.TargetModel = "gpt-5.4"
	second := newTestUserModelRoute(true)
	second.SourceModel = "claude-4.5"
	second.ChannelIds = []int{10, 11}
	require.NoError(t, ReplaceUserModelRoutes(1, []*UserModelRoute{first, second}))

	routes, err := GetUserModelRoutes(1)
	require.NoError(t, err)
	require.Len(t, routes, 2)
	assert.Equal(t, "claude-4.5", routes[0].SourceModel)
	assert.Equal(t, []int{10, 11}, routes[0].ChannelIds)
	assert.Equal(t, "gpt-5.5", routes[1].SourceModel)
	assert.Equal(t, []string{"premium"}, routes[1].Groups)
}

func TestReplaceUserModelRoutesRollsBackOnPayloadConflict(t *testing.T) {
	setupUserModelRouteTestDB(t)

	existing := newTestUserModelRoute(true)
	require.NoError(t, SaveUserModelRoute(existing))

	first := newTestUserModelRoute(true)
	duplicate := newTestUserModelRoute(false, "premium")
	err := ReplaceUserModelRoutes(1, []*UserModelRoute{first, duplicate})
	assert.ErrorIs(t, err, ErrUserModelRouteConflict)

	routes, err := GetUserModelRoutes(1)
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.Equal(t, existing.Id, routes[0].Id)
}

func TestSaveUserModelRouteRejectsAllGroupsAndScopedOverlap(t *testing.T) {
	setupUserModelRouteTestDB(t)

	allGroups := newTestUserModelRoute(true)
	require.NoError(t, SaveUserModelRoute(allGroups))

	scoped := newTestUserModelRoute(false, "premium")
	err := SaveUserModelRoute(scoped)
	assert.ErrorIs(t, err, ErrUserModelRouteConflict)

	otherSource := newTestUserModelRoute(false, "premium")
	otherSource.SourceModel = "claude-4"
	require.NoError(t, SaveUserModelRoute(otherSource))
}

func TestSaveUserModelRouteDefaultsPoolName(t *testing.T) {
	setupUserModelRouteTestDB(t)

	route := newTestUserModelRoute(true)
	require.NoError(t, SaveUserModelRoute(route))
	assert.Equal(t, "gpt-5.4 → gpt-5.5", route.PoolName)

	routes, err := GetUserModelRoutes(1)
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.Equal(t, "gpt-5.4 → gpt-5.5", routes[0].PoolName)
}

func TestSaveUserModelRoutePersistsOrderedExecutionGroups(t *testing.T) {
	setupUserModelRouteTestDB(t)

	route := newTestUserModelRoute(true)
	route.ExecutionGroups = []string{"vip", "default", "vip", " internal "}
	require.NoError(t, SaveUserModelRoute(route))
	assert.Equal(t, "vip", route.ExecutionGroup)

	routes, err := GetUserModelRoutes(1)
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.Equal(t, "vip", routes[0].ExecutionGroup)
	assert.Equal(t, []string{"vip", "default", "internal"}, routes[0].ExecutionGroups)

	routes[0].ExecutionGroups = []string{"internal", "vip"}
	require.NoError(t, SaveUserModelRoute(routes[0]))

	updated, err := GetUserModelRoutes(1)
	require.NoError(t, err)
	require.Len(t, updated, 1)
	assert.Equal(t, "internal", updated[0].ExecutionGroup)
	assert.Equal(t, []string{"internal", "vip"}, updated[0].ExecutionGroups)
}

func TestSetUserModelRouteEnabledPreservesStaleRouteConfiguration(t *testing.T) {
	setupUserModelRouteTestDB(t)

	route := newTestUserModelRoute(true)
	route.ExecutionGroups = []string{"default", "vip"}
	route.ChannelIds = []int{10, 11}
	require.NoError(t, SaveUserModelRoute(route))

	updated, err := SetUserModelRouteEnabled(1, route.Id, false)
	require.NoError(t, err)
	assert.False(t, updated.Enabled)
	assert.Equal(t, []string{"default", "vip"}, updated.ExecutionGroups)
	assert.Equal(t, []int{10, 11}, updated.ChannelIds)

	loaded, err := GetUserModelRoute(1, route.Id)
	require.NoError(t, err)
	assert.False(t, loaded.Enabled)
	assert.Equal(t, route.SourceModel, loaded.SourceModel)
}

func TestSaveUserModelRouteCountsInjectPromptCharactersInsteadOfBytes(t *testing.T) {
	setupUserModelRouteTestDB(t)

	boundary := newTestUserModelRoute(true)
	boundary.InjectPrompt = strings.Repeat("路", UserModelRouteMaxInjectPrompt)
	require.NoError(t, SaveUserModelRoute(boundary))

	overLimit := newTestUserModelRoute(true)
	overLimit.SourceModel = "claude-4"
	overLimit.InjectPrompt = strings.Repeat("路", UserModelRouteMaxInjectPrompt+1)
	assert.EqualError(t, SaveUserModelRoute(overLimit), "user model route inject prompt is too long")
}

func TestSaveUserModelRouteRejectsWildcardTarget(t *testing.T) {
	setupUserModelRouteTestDB(t)

	route := newTestUserModelRoute(true)
	route.TargetModel = "gpt-4-gizmo-*"

	assert.ErrorIs(t, SaveUserModelRoute(route), ErrUserModelRouteWildcardTarget)
}

func TestSaveUserModelRouteRejectsScopedAndAllGroupsOverlapInEitherOrder(t *testing.T) {
	setupUserModelRouteTestDB(t)

	scoped := newTestUserModelRoute(false, "premium")
	require.NoError(t, SaveUserModelRoute(scoped))

	allGroups := newTestUserModelRoute(true)
	err := SaveUserModelRoute(allGroups)
	assert.ErrorIs(t, err, ErrUserModelRouteConflict)
}

func TestSaveUserModelRouteRejectsOnlyOverlappingScopedGroups(t *testing.T) {
	setupUserModelRouteTestDB(t)

	first := newTestUserModelRoute(false, "premium", "team")
	require.NoError(t, SaveUserModelRoute(first))

	nonOverlapping := newTestUserModelRoute(false, "enterprise")
	require.NoError(t, SaveUserModelRoute(nonOverlapping))

	overlapping := newTestUserModelRoute(false, "team", "enterprise")
	err := SaveUserModelRoute(overlapping)
	assert.ErrorIs(t, err, ErrUserModelRouteConflict)
}

func TestGetApplicableUserModelRouteUsesOrderedMemberships(t *testing.T) {
	setupUserModelRouteTestDB(t)

	first := newTestUserModelRoute(false, "premium")
	first.TargetModel = "target-premium"
	require.NoError(t, SaveUserModelRoute(first))

	second := newTestUserModelRoute(false, "enterprise")
	second.TargetModel = "target-enterprise"
	require.NoError(t, SaveUserModelRoute(second))

	resolved, err := GetApplicableUserModelRouteForGroups(1, "gpt-5.4", []string{"enterprise", "premium"})
	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Equal(t, "target-enterprise", resolved.TargetModel)

	resolved, err = GetApplicableUserModelRouteForGroups(1, "gpt-5.4", []string{"premium", "enterprise"})
	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Equal(t, "target-premium", resolved.TargetModel)
}
