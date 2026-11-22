package model

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupGroupRegistryTestDB(t *testing.T) *gorm.DB {
	return setupGroupRegistryDB(
		t,
		sqlite.Open(filepath.Join(t.TempDir(), "groups.db")),
		common.DatabaseTypeSQLite,
		true,
	)
}

func setupGroupRegistryDB(
	t *testing.T,
	dialector gorm.Dialector,
	databaseType common.DatabaseType,
	migrateRegistry bool,
) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled, previousMemoryCacheEnabled := common.RedisEnabled, common.MemoryCacheEnabled
	common.OptionMapRWMutex.Lock()
	previousOptions := maps.Clone(common.OptionMap)
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	previousRatios := ratio_setting.GroupRatio2JSONString()
	previousGroupRatios := ratio_setting.GroupGroupRatio2JSONString()
	previousDescriptions := setting.UserUsableGroups2JSONString()

	common.SetDatabaseTypes(databaseType, databaseType)
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	initCol()
	models := []any{
		&Option{},
		&Task{},
		&User{},
		&UserGroupMembership{},
		&Token{},
		&Channel{},
		&Ability{},
		&UserModelRoute{},
		&UserModelRouteGroup{},
		&UserModelRouteExecutionGroup{},
		&SubscriptionPlan{},
		&UserSubscription{},
	}
	if migrateRegistry {
		models = append(models, &Group{}, &GroupNameHistory{})
	}
	require.NoError(t, db.AutoMigrate(models...))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2,"team":3}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"vip":0.7}}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","team":"Team"}`))

	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedisEnabled, previousMemoryCacheEnabled
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousDescriptions))
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedGroupRegistryTestOptions(t *testing.T, db *gorm.DB) {
	t.Helper()
	options := []Option{
		{Key: "GroupRatio", Value: `{"default":1,"vip":2,"team":3}`},
		{Key: "UserUsableGroups", Value: `{"default":"Default","vip":"VIP","team":"Team"}`},
		{Key: "TopupGroupRatio", Value: `{"vip":1.5,"team":2}`},
		{Key: "GroupGroupRatio", Value: `{"vip":{"vip":0.7}}`},
	}
	require.NoError(t, db.Create(&options).Error)
}

func seedGroupRegistryTestGroups(t *testing.T) {
	t.Helper()
	require.NoError(t, SyncGroupRegistryFromSettings())
}

func TestGroupRegistrySyncIsIdempotent(t *testing.T) {
	db := setupGroupRegistryTestDB(t)
	seedGroupRegistryTestGroups(t)
	require.NoError(t, SyncGroupRegistryFromSettings())

	groups, err := ListGroups()
	require.NoError(t, err)
	require.Len(t, groups, 3)
	assert.Equal(t, []string{"default", "team", "vip"}, []string{groups[0].Name, groups[1].Name, groups[2].Name})
	assert.Equal(t, 2.0, groups[2].Ratio)
	require.NoError(t, db.Model(&Group{}).Where("name = ?", "vip").Updates(map[string]any{
		"description": "Curated description",
		"sort_order":  20,
	}).Error)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2.5,"team":3}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"Legacy description","team":"Team"}`))

	require.NoError(t, db.AutoMigrate(&Group{}, &GroupNameHistory{}))
	require.NoError(t, db.AutoMigrate(&Group{}, &GroupNameHistory{}))
	require.NoError(t, SyncGroupRegistryFromSettings())
	groups, err = ListGroups()
	require.NoError(t, err)
	assert.Len(t, groups, 3)
	assert.Equal(t, "vip", groups[2].Name)
	assert.Equal(t, "Curated description", groups[2].Description)
	assert.Equal(t, 20, groups[2].SortOrder)
	assert.Equal(t, 2.5, groups[2].Ratio)
}

func TestGroupRegistryImportsGroupsAfterPersistedOptionsLoad(t *testing.T) {
	db := setupGroupRegistryDB(t, sqlite.Open(filepath.Join(t.TempDir(), "group-options-upgrade.db")), common.DatabaseTypeSQLite, false)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1,"svip":1}`))
	require.NoError(t, db.Create(&[]Option{
		{Key: "GroupRatio", Value: `{"default":1,"team":3}`},
		{Key: "UserUsableGroups", Value: `{"default":"Default","team":"Team"}`},
	}).Error)

	// Database migration runs before InitOptionMap publishes the saved settings.
	require.NoError(t, db.AutoMigrate(&Group{}, &GroupNameHistory{}))
	loadOptionsFromDatabase()

	groups, err := ListGroups()
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "team"}, []string{groups[0].Name, groups[1].Name})
}

func TestGroupRegistryMigratesLegacySchemaAndMutatesGroupsOnSQLDialects(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		database   common.DatabaseType
		dialector  func(string) gorm.Dialector
		versionSQL string
	}{
		{
			name:       "sqlite",
			database:   common.DatabaseTypeSQLite,
			dialector:  func(_ string) gorm.Dialector { return sqlite.Open(filepath.Join(t.TempDir(), "upgrade.db")) },
			versionSQL: "SELECT sqlite_version()",
		},
		{
			name:       "mysql",
			env:        "TEST_MYSQL_DSN",
			database:   common.DatabaseTypeMySQL,
			dialector:  mysql.Open,
			versionSQL: "SELECT VERSION()",
		},
		{
			name:       "postgres",
			env:        "TEST_POSTGRES_DSN",
			database:   common.DatabaseTypePostgreSQL,
			dialector:  postgres.Open,
			versionSQL: "SELECT version()",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := ""
			if test.env != "" {
				dsn = os.Getenv(test.env)
				if dsn == "" {
					t.Skipf("set %s to run the real %s database test", test.env, test.name)
				}
			}
			db := setupGroupRegistryDB(t, test.dialector(dsn), test.database, false)
			assert.False(t, db.Migrator().HasTable(&Group{}))
			seedGroupRegistryTestOptions(t, db)

			var version string
			require.NoError(t, db.Raw(test.versionSQL).Scan(&version).Error)
			t.Logf("database version: %s", version)

			require.NoError(t, db.AutoMigrate(&Group{}, &GroupNameHistory{}))
			require.NoError(t, db.AutoMigrate(&Group{}, &GroupNameHistory{}))
			loadOptionsFromDatabase()
			user := User{
				Username:   "group-dialect-user",
				Password:   "unused-password-hash",
				Role:       common.RoleCommonUser,
				Status:     common.UserStatusEnabled,
				Group:      "team",
				TopupGroup: "team",
				AffCode:    "dialect1",
			}
			require.NoError(t, db.Create(&user).Error)
			manual := true
			require.NoError(t, db.Create(&UserGroupMembership{
				UserId:    user.Id,
				GroupName: "team",
				SortOrder: 0,
				Manual:    &manual,
			}).Error)
			token := Token{UserId: user.Id, Key: "group-dialect-token", Group: "team"}
			require.NoError(t, db.Create(&token).Error)
			autoToken := Token{UserId: user.Id, Key: "group-dialect-auto-token", Group: "auto"}
			require.NoError(t, autoToken.SetAutoGroups([]string{"team"}))
			require.NoError(t, db.Create(&autoToken).Error)
			renameOnlyUser := User{
				Username: "group-dialect-rename-only-user",
				Password: "unused-password-hash",
				Role:     common.RoleCommonUser,
				Status:   common.UserStatusEnabled,
				Group:    "default",
				AffCode:  "dialect2",
			}
			require.NoError(t, db.Create(&renameOnlyUser).Error)
			renameOnlyToken := Token{UserId: renameOnlyUser.Id, Key: "group-dialect-rename-only-token", Group: "auto"}
			require.NoError(t, renameOnlyToken.SetAutoGroups([]string{"vip"}))
			require.NoError(t, db.Create(&renameOnlyToken).Error)
			renameOnlyPolicyVersion := renameOnlyUser.PolicyVersion
			deleteOnlyUser := User{
				Username: "group-dialect-delete-only-user",
				Password: "unused-password-hash",
				Role:     common.RoleCommonUser,
				Status:   common.UserStatusEnabled,
				Group:    "default",
				AffCode:  "dialect3",
			}
			require.NoError(t, db.Create(&deleteOnlyUser).Error)
			deleteOnlyToken := Token{UserId: deleteOnlyUser.Id, Key: "group-dialect-delete-only-token", Group: "auto"}
			require.NoError(t, deleteOnlyToken.SetAutoGroups([]string{"team"}))
			require.NoError(t, db.Create(&deleteOnlyToken).Error)
			deleteOnlyPolicyVersion := deleteOnlyUser.PolicyVersion

			require.NoError(t, RenameGroup("vip", Group{
				Name:        "premium",
				Description: "Premium users",
				Ratio:       2.5,
				SortOrder:   1,
			}))
			require.NoError(t, DeleteGroup("team"))
			groups, err := ListGroups()
			require.NoError(t, err)
			assert.Equal(t, []string{"default", "premium"}, []string{groups[0].Name, groups[1].Name})
			history, err := GroupHistoryNames()
			require.NoError(t, err)
			assert.Len(t, history, 2)
			var updatedUser User
			require.NoError(t, db.First(&updatedUser, user.Id).Error)
			assert.Equal(t, "default", updatedUser.Group)
			assert.Equal(t, "default", updatedUser.TopupGroup)
			var updatedToken Token
			require.NoError(t, db.First(&updatedToken, token.Id).Error)
			assert.Equal(t, "default", updatedToken.Group)
			var updatedAutoToken Token
			require.NoError(t, db.First(&updatedAutoToken, autoToken.Id).Error)
			autoGroups, err := updatedAutoToken.GetAutoGroups()
			require.NoError(t, err)
			assert.Equal(t, []string{"default"}, autoGroups)
			var updatedRenameOnlyUser User
			require.NoError(t, db.First(&updatedRenameOnlyUser, renameOnlyUser.Id).Error)
			assert.Greater(t, updatedRenameOnlyUser.PolicyVersion, renameOnlyPolicyVersion)
			var updatedRenameOnlyToken Token
			require.NoError(t, db.First(&updatedRenameOnlyToken, renameOnlyToken.Id).Error)
			autoGroups, err = updatedRenameOnlyToken.GetAutoGroups()
			require.NoError(t, err)
			assert.Equal(t, []string{"premium"}, autoGroups)
			var updatedDeleteOnlyUser User
			require.NoError(t, db.First(&updatedDeleteOnlyUser, deleteOnlyUser.Id).Error)
			assert.Greater(t, updatedDeleteOnlyUser.PolicyVersion, deleteOnlyPolicyVersion)
			var updatedDeleteOnlyToken Token
			require.NoError(t, db.First(&updatedDeleteOnlyToken, deleteOnlyToken.Id).Error)
			autoGroups, err = updatedDeleteOnlyToken.GetAutoGroups()
			require.NoError(t, err)
			assert.Equal(t, []string{"default"}, autoGroups)
		})
	}
}

func TestGroupRegistryUpgradeFromLatestReleaseDatabase(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		database  common.DatabaseType
		dialector func(string) gorm.Dialector
	}{
		{name: "sqlite", env: "GROUP_RELEASE_SCHEMA_PATH", database: common.DatabaseTypeSQLite, dialector: sqlite.Open},
		{name: "mysql", env: "GROUP_RELEASE_MYSQL_DSN", database: common.DatabaseTypeMySQL, dialector: mysql.Open},
		{name: "postgres", env: "GROUP_RELEASE_POSTGRES_DSN", database: common.DatabaseTypePostgreSQL, dialector: postgres.Open},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if dsn == "" {
				t.Skipf("set %s to run the latest-release upgrade test", test.env)
			}
			previousDB, previousLogDB := DB, LOG_DB
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			previousRatios := ratio_setting.GroupRatio2JSONString()
			previousGroupRatios := ratio_setting.GroupGroupRatio2JSONString()
			previousDescriptions := setting.UserUsableGroups2JSONString()
			common.OptionMapRWMutex.Lock()
			previousOptions := maps.Clone(common.OptionMap)
			common.OptionMapRWMutex.Unlock()
			db, err := gorm.Open(test.dialector(dsn), &gorm.Config{})
			require.NoError(t, err)
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMainType, previousLogType)
				initCol()
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
				require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousGroupRatios))
				require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousDescriptions))
				common.OptionMapRWMutex.Lock()
				common.OptionMap = previousOptions
				common.OptionMapRWMutex.Unlock()
				sqlDB, sqlErr := db.DB()
				if sqlErr == nil {
					_ = sqlDB.Close()
				}
			})
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(test.database, test.database)
			initCol()
			require.NoError(t, migrateDB())
			InitOptionMap()

			groups, err := ListGroups()
			require.NoError(t, err)
			require.Equal(t, []string{"default", "release-team"}, []string{groups[0].Name, groups[1].Name})
			assert.Equal(t, 2.25, groups[1].Ratio)
			assert.Equal(t, "Release Team", groups[1].Description)
			var user User
			require.NoError(t, db.First(&user, 777).Error)
			assert.Equal(t, "release-team", user.Group)
			require.NoError(t, migrateDB())
			groups, err = ListGroups()
			require.NoError(t, err)
			assert.Equal(t, []string{"default", "release-team"}, []string{groups[0].Name, groups[1].Name})
		})
	}
}

func TestUpdateOptionsBulkRequiresRegistryForGroupRemoval(t *testing.T) {
	setupGroupRegistryTestDB(t)
	seedGroupRegistryTestGroups(t)

	err := UpdateOptionsBulk(map[string]string{
		"GroupRatio": `{"default":1,"team":3}`,
	})
	require.ErrorIs(t, err, ErrGroupRemovalRequiresRegistry)
	assert.Equal(t, 2.0, ratio_setting.GetGroupRatio("vip"))
	groups, err := ListGroups()
	require.NoError(t, err)
	assert.Len(t, groups, 3)
}

func TestRenameGroupUpdatesLiveReferencesAndSnapshotsTaskRatio(t *testing.T) {
	db := setupGroupRegistryTestDB(t)
	seedGroupRegistryTestOptions(t, db)
	seedGroupRegistryTestGroups(t)

	user := User{
		Username:   "group-rename-user",
		Password:   "unused-password-hash",
		Role:       common.RoleCommonUser,
		Status:     common.UserStatusEnabled,
		Group:      "vip",
		TopupGroup: "vip",
	}
	require.NoError(t, db.Create(&user).Error)
	manual := true
	other := false
	require.NoError(t, db.Create(&[]UserGroupMembership{
		{UserId: user.Id, GroupName: "vip", SortOrder: 0, Manual: &manual},
		{UserId: user.Id, GroupName: "default", SortOrder: 1, Manual: &other},
	}).Error)

	token := Token{UserId: user.Id, Key: "group-rename-token", Group: "vip", Name: "rename token"}
	require.NoError(t, token.SetAutoGroups([]string{"vip", "default"}))
	require.NoError(t, db.Create(&token).Error)
	channel := Channel{Key: "group-rename-channel", Name: "rename channel", Group: "vip,default", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{ChannelId: channel.Id, Model: "model", Group: "vip", Enabled: true}).Error)
	route := UserModelRoute{
		UserId:         user.Id,
		SourceModel:    "source",
		TargetModel:    "target",
		PoolName:       "pool",
		ExecutionGroup: "vip",
		Enabled:        true,
	}
	require.NoError(t, db.Create(&route).Error)
	require.NoError(t, db.Create(&[]UserModelRouteGroup{{RouteId: route.Id, GroupName: "vip"}, {RouteId: route.Id, GroupName: "default"}}).Error)
	require.NoError(t, db.Create(&UserModelRouteExecutionGroup{RouteId: route.Id, GroupName: "vip", Position: 0}).Error)
	task := Task{TaskID: "group-rename-task", UserId: user.Id, Group: "vip", Status: TaskStatusQueued}
	require.NoError(t, db.Create(&task).Error)

	require.NoError(t, RenameGroup("vip", Group{Name: "pro", Description: "Professional", Ratio: 2, SortOrder: 2}))

	var updatedUser User
	require.NoError(t, db.First(&updatedUser, user.Id).Error)
	assert.Equal(t, "pro", updatedUser.Group)
	assert.Equal(t, "pro", updatedUser.TopupGroup)
	var memberships []UserGroupMembership
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("sort_order asc").Find(&memberships).Error)
	require.Len(t, memberships, 2)
	assert.Equal(t, []string{"pro", "default"}, []string{memberships[0].GroupName, memberships[1].GroupName})
	require.NotNil(t, memberships[0].Manual)
	assert.True(t, *memberships[0].Manual)

	var updatedToken Token
	require.NoError(t, db.First(&updatedToken, token.Id).Error)
	assert.Equal(t, "pro", updatedToken.Group)
	autoGroups, err := updatedToken.GetAutoGroups()
	require.NoError(t, err)
	assert.Equal(t, []string{"pro", "default"}, autoGroups)
	var updatedChannel Channel
	require.NoError(t, db.First(&updatedChannel, channel.Id).Error)
	assert.Equal(t, []string{"pro", "default"}, updatedChannel.GetGroups())
	var ability Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channel.Id, "model").First(&ability).Error)
	assert.Equal(t, "pro", ability.Group)
	var updatedRoute UserModelRoute
	require.NoError(t, db.First(&updatedRoute, route.Id).Error)
	assert.Equal(t, "pro", updatedRoute.ExecutionGroup)
	var routeGroups []UserModelRouteGroup
	require.NoError(t, db.Where("route_id = ?", route.Id).Order("group_name").Find(&routeGroups).Error)
	assert.Equal(t, []string{"default", "pro"}, []string{routeGroups[0].GroupName, routeGroups[1].GroupName})
	var executionGroups []UserModelRouteExecutionGroup
	require.NoError(t, db.Where("route_id = ?", route.Id).Find(&executionGroups).Error)
	require.Len(t, executionGroups, 1)
	assert.Equal(t, "pro", executionGroups[0].GroupName)

	var updatedTask Task
	require.NoError(t, db.First(&updatedTask, task.ID).Error)
	assert.Equal(t, "pro", updatedTask.Group)
	require.NotNil(t, updatedTask.PrivateData.BillingContext)
	assert.True(t, updatedTask.PrivateData.BillingContext.GroupRatioSnapshot)
	assert.Equal(t, 0.7, updatedTask.PrivateData.BillingContext.GroupRatio)

	var ratioOption, topupOption, overrideOption Option
	require.NoError(t, db.First(&ratioOption, "key = ?", "GroupRatio").Error)
	require.NoError(t, db.First(&topupOption, "key = ?", "TopupGroupRatio").Error)
	require.NoError(t, db.First(&overrideOption, "key = ?", "GroupGroupRatio").Error)
	var ratios map[string]float64
	var topupRatios map[string]float64
	var overrides map[string]map[string]float64
	require.NoError(t, common.UnmarshalJsonStr(ratioOption.Value, &ratios))
	require.NoError(t, common.UnmarshalJsonStr(topupOption.Value, &topupRatios))
	require.NoError(t, common.UnmarshalJsonStr(overrideOption.Value, &overrides))
	assert.Equal(t, 2.0, ratios["pro"])
	assert.Equal(t, 1.5, topupRatios["pro"])
	assert.Equal(t, 0.7, overrides["pro"]["pro"])
	assert.NotContains(t, ratios, "vip")
	assert.NotContains(t, overrides, "vip")
	var history GroupNameHistory
	require.NoError(t, db.First(&history, "old_name = ?", "vip").Error)
	assert.Equal(t, "pro", history.NewName)
}

func TestDeleteGroupBlocksSubscriptionsAndCleansAssignments(t *testing.T) {
	db := setupGroupRegistryTestDB(t)
	seedGroupRegistryTestOptions(t, db)
	seedGroupRegistryTestGroups(t)
	user := User{Username: "group-delete-user", Password: "unused-password-hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "team", TopupGroup: "team", AffCode: "team1"}
	require.NoError(t, db.Create(&user).Error)
	manual := true
	require.NoError(t, db.Create(&[]UserGroupMembership{
		{UserId: user.Id, GroupName: "team", SortOrder: 0, Manual: &manual},
		{UserId: user.Id, GroupName: "default", SortOrder: 1, Manual: &manual},
	}).Error)
	token := Token{UserId: user.Id, Key: "group-delete-token", Group: "team"}
	require.NoError(t, token.SetAutoGroups([]string{"team"}))
	require.NoError(t, db.Create(&token).Error)
	legacyUser := User{Username: "group-delete-legacy-user", Password: "unused-password-hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "team", AffCode: "team2"}
	require.NoError(t, db.Create(&legacyUser).Error)
	legacyToken := Token{UserId: legacyUser.Id, Key: "group-delete-legacy-token", Group: "team"}
	require.NoError(t, db.Create(&legacyToken).Error)
	autoToken := Token{UserId: user.Id, Key: "group-delete-auto-token", Group: "auto"}
	require.NoError(t, autoToken.SetAutoGroups([]string{"team"}))
	require.NoError(t, db.Create(&autoToken).Error)
	autoOnlyUser := User{Username: "group-delete-auto-only-user", Password: "unused-password-hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "team3"}
	require.NoError(t, db.Create(&autoOnlyUser).Error)
	autoOnlyToken := Token{UserId: autoOnlyUser.Id, Key: "group-delete-auto-only-token", Group: "auto"}
	require.NoError(t, autoOnlyToken.SetAutoGroups([]string{"team"}))
	require.NoError(t, db.Create(&autoOnlyToken).Error)
	initialPolicyVersion := autoOnlyUser.PolicyVersion
	channel := Channel{Key: "group-delete-channel", Name: "delete channel", Group: "team", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	route := UserModelRoute{UserId: user.Id, SourceModel: "source", TargetModel: "target", PoolName: "pool", ExecutionGroup: "team", Enabled: true}
	require.NoError(t, db.Create(&route).Error)
	require.NoError(t, db.Create(&UserModelRouteGroup{RouteId: route.Id, GroupName: "team"}).Error)
	plan := SubscriptionPlan{Title: "Team plan", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100, UpgradeGroup: "team"}
	require.NoError(t, db.Create(&plan).Error)

	require.ErrorIs(t, DeleteGroup("team"), ErrGroupHasSubscriptions)
	plan.UpgradeGroup = ""
	require.NoError(t, db.Save(&plan).Error)
	subscription := UserSubscription{
		UserId:       user.Id,
		PlanId:       plan.Id,
		Status:       SubscriptionStatusActive,
		EndTime:      GetDBTimestamp() + int64(time.Hour.Seconds()),
		UpgradeGroup: "team",
	}
	require.NoError(t, db.Create(&subscription).Error)
	require.ErrorIs(t, DeleteGroup("team"), ErrGroupHasSubscriptions)
	require.NoError(t, db.Delete(&subscription).Error)

	require.NoError(t, DeleteGroup("team"))
	var updatedUser User
	require.NoError(t, db.First(&updatedUser, user.Id).Error)
	assert.Equal(t, "default", updatedUser.Group)
	assert.Equal(t, "default", updatedUser.TopupGroup)
	var unchangedToken Token
	require.NoError(t, db.First(&unchangedToken, token.Id).Error)
	assert.Equal(t, "default", unchangedToken.Group)
	autoGroups, err := unchangedToken.GetAutoGroups()
	require.NoError(t, err)
	assert.Empty(t, autoGroups)
	var updatedLegacyToken Token
	require.NoError(t, db.First(&updatedLegacyToken, legacyToken.Id).Error)
	assert.Equal(t, "default", updatedLegacyToken.Group)
	var updatedAutoToken Token
	require.NoError(t, db.First(&updatedAutoToken, autoToken.Id).Error)
	assert.Equal(t, "auto", updatedAutoToken.Group)
	autoGroups, err = updatedAutoToken.GetAutoGroups()
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, autoGroups)
	var updatedAutoOnlyUser User
	require.NoError(t, db.First(&updatedAutoOnlyUser, autoOnlyUser.Id).Error)
	assert.Greater(t, updatedAutoOnlyUser.PolicyVersion, initialPolicyVersion)
	var updatedAutoOnlyToken Token
	require.NoError(t, db.First(&updatedAutoOnlyToken, autoOnlyToken.Id).Error)
	autoGroups, err = updatedAutoOnlyToken.GetAutoGroups()
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, autoGroups)
	var updatedChannel Channel
	require.NoError(t, db.First(&updatedChannel, channel.Id).Error)
	assert.Empty(t, updatedChannel.GetGroups())
	assert.Equal(t, common.ChannelStatusManuallyDisabled, updatedChannel.Status)
	var updatedRoute UserModelRoute
	require.NoError(t, db.First(&updatedRoute, route.Id).Error)
	assert.False(t, updatedRoute.Enabled)
	assert.Empty(t, updatedRoute.ExecutionGroup)
	var history GroupNameHistory
	require.NoError(t, db.First(&history, "old_name = ?", "team").Error)
	assert.Empty(t, history.NewName)
}

func TestDefaultGroupCannotBeRenamedOrDeleted(t *testing.T) {
	setupGroupRegistryTestDB(t)
	seedGroupRegistryTestGroups(t)

	err := RenameGroup("default", Group{Name: "base", Description: "Base", Ratio: 1})
	require.ErrorIs(t, err, ErrDefaultGroupProtected)
	require.ErrorIs(t, DeleteGroup("default"), ErrDefaultGroupProtected)
	groups, err := ListGroups()
	require.NoError(t, err)
	assert.Contains(t, []string{groups[0].Name, groups[1].Name, groups[2].Name}, "default")
}

func TestSnapshotUnfinishedTaskGroupRatioPreservesZero(t *testing.T) {
	db := setupGroupRegistryTestDB(t)
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	user := User{Username: "zero-ratio-user", Password: "unused-password-hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "vip"}
	require.NoError(t, db.Create(&user).Error)
	task := Task{TaskID: "zero-ratio-task", UserId: user.Id, Group: "vip", Status: TaskStatusQueued}
	require.NoError(t, db.Create(&task).Error)

	require.NoError(t, SnapshotUnfinishedTaskGroupRatiosWithTx(db, "vip", 0))
	var updated Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.NotNil(t, updated.PrivateData.BillingContext)
	assert.True(t, updated.PrivateData.BillingContext.GroupRatioSnapshot)
	assert.Zero(t, updated.PrivateData.BillingContext.GroupRatio)
}
