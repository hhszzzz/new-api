package model

import (
	"path/filepath"
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
