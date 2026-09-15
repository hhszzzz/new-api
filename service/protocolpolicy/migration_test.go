package protocolpolicy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/service/channelcompat"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var migrationFixtureSequence atomic.Int32

func migrationDatabase(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	switch engine {
	case "sqlite":
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "migration.db"))
	case "mysql":
		dsn := os.Getenv("NEWAPI_PROTOCOL_TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("set NEWAPI_PROTOCOL_TEST_MYSQL_DSN to an isolated MySQL database")
		}
		dialector = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("NEWAPI_PROTOCOL_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set NEWAPI_PROTOCOL_TEST_POSTGRES_DSN to an isolated PostgreSQL database")
		}
		dialector = postgres.Open(dsn)
	}
	prefix := fmt.Sprintf("pm%d_%d_", os.Getpid(), migrationFixtureSequence.Add(1))
	db, err := gorm.Open(dialector, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Option{}))
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Option{}))
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(&model.Channel{}, &model.Option{}))
		sqlDB, err := db.DB()
		require.NoError(t, err)
		assert.NoError(t, sqlDB.Close())
	})
	return db
}

func TestProtocolPolicyMigrationDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := migrationDatabase(t, engine)
			global := model_setting.GetGlobalSettings()
			previous := *global
			t.Cleanup(func() { *global = previous })
			global.ProtocolPolicy = nil
			global.ProtocolBridgePolicy = model_setting.ProtocolBridgePolicy{Enabled: true, DefaultAllowConversion: true, StateTTLSeconds: 3600, MaxStateTurns: 128, MaxStateBytes: 4 * 1024 * 1024}
			global.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{AllChannels: true}
			global.PassThroughRequestEnabled = false
			bridge, err := common.Marshal(global.ProtocolBridgePolicy)
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Option{Key: "global.protocol_bridge_policy", Value: string(bridge)}).Error)
			require.NoError(t, db.Create(&model.Option{Key: "untouched", Value: "keep"}).Error)
			channels := []model.Channel{
				{Id: 1, Type: constant.ChannelTypeOpenAI, Key: "channel-key", Models: "public", ModelMapping: common.GetPointer(`{"public":"provider-model"}`), OtherSettings: `{"allow_service_tier":true,"protocol_capabilities":{"upstream_protocols":["chat"],"allow_conversion":true,"model_overrides":[{"model_pattern":"^provider-model$","upstream_protocols":["messages"]}]}}`},
				{Id: 2, Type: constant.ChannelTypeAdvancedCustom, Key: "custom-key", Models: "public", OtherSettings: `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/private/messages","converter":"openai_chat_completions_to_anthropic_messages","models":["public"],"auth":{"type":"header","name":"x-upstream-secret","value":"private-route-key"}},{"incoming_path":"/v1/messages","upstream_path":"/private/native-messages","converter":"none"},{"incoming_path":"/v1/responses","upstream_path":"/private/native-responses","target_protocol":"native"},{"incoming_path":"/v1/responses/compact","upstream_path":"/private/compact","converter":"none"},{"incoming_path":"/v1/images/generations","upstream_path":"/private/images","converter":"none"}]},"custom_extension":{"keep":true}}`},
				{Id: 3, Type: constant.ChannelTypeOpenAI, Key: "strict-key", Models: "public", OtherSettings: `{"protocol_capabilities":{"upstream_protocols":["chat"],"allow_conversion":false},"tool_loss_policy":"strict"}`},
				{Id: 4, Type: constant.ChannelTypeGemini, Key: "gemini-key", Models: "gemini-model"},
			}
			require.NoError(t, db.Create(&channels).Error)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 4).UpdateColumn("settings", nil).Error)
			beforePlans := make(map[string]channelcompat.ProtocolPlan)
			for _, channel := range channels {
				for _, protocol := range relayconvert.Protocols() {
					key := fmt.Sprintf("%d/%s", channel.Id, protocol)
					beforePlans[key] = channelcompat.PlanForRequest(&channel, protocol, "public", relayconvert.CanonicalPath(protocol, "public"), channelcompat.RequestFeatureSet{})
				}
			}
			manifest, err := Preflight(db)
			require.NoError(t, err)
			require.Empty(t, manifest.Blockers)
			require.Len(t, manifest.Channels, len(channels))
			review, err := common.Marshal(manifest.Review())
			require.NoError(t, err)
			assert.NotContains(t, string(review), "private-route-key")
			var unchanged model.Channel
			require.NoError(t, db.First(&unchanged, 1).Error)
			assert.Equal(t, channels[0].OtherSettings, unchanged.OtherSettings)
			backup := filepath.Join(t.TempDir(), "protocol-policy.json")
			require.NoError(t, SaveBackup(backup, manifest))
			stat, err := os.Stat(backup)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0600), stat.Mode().Perm())
			require.NoError(t, Apply(db, manifest))
			require.NoError(t, Apply(db, manifest))
			var policyOption model.Option
			require.NoError(t, db.Where(map[string]any{"key": optionKey}).First(&policyOption).Error)
			var policy hostdto.ProtocolPolicy
			require.NoError(t, common.UnmarshalJsonStr(policyOption.Value, &policy))
			global.ProtocolPolicy = &policy
			var migrated []model.Channel
			require.NoError(t, db.Order("id").Find(&migrated).Error)
			for _, channel := range migrated {
				assert.NotContains(t, channel.OtherSettings, "protocol_capabilities")
				assert.NotContains(t, channel.OtherSettings, `"converter"`)
				if channel.Id == 2 {
					var settings hostdto.ChannelOtherSettings
					require.NoError(t, common.UnmarshalJsonStr(channel.OtherSettings, &settings))
					require.NotNil(t, settings.AdvancedCustom)
					require.Len(t, settings.AdvancedCustom.Routes, 5)
					assert.Equal(t, "messages", settings.AdvancedCustom.Routes[0].TargetProtocol)
					for _, route := range settings.AdvancedCustom.Routes[1:] {
						assert.Equal(t, "native", route.TargetProtocol, route.IncomingPath)
					}
				}
				for _, protocol := range relayconvert.Protocols() {
					key := fmt.Sprintf("%d/%s", channel.Id, protocol)
					old := beforePlans[key]
					next := channelcompat.PlanForRequest(&channel, protocol, "public", relayconvert.CanonicalPath(protocol, "public"), channelcompat.RequestFeatureSet{})
					assert.Equal(t, old.Status, next.Status, key)
					assert.Equal(t, old.UpstreamProtocol, next.UpstreamProtocol, key)
				}
			}
			again, err := Preflight(db)
			require.NoError(t, err)
			assert.Empty(t, again.Blockers)
			assert.Empty(t, again.Channels)
			assert.Empty(t, again.Options)
			var edited map[string]json.RawMessage
			require.NoError(t, common.UnmarshalJsonStr(migrated[0].OtherSettings, &edited))
			edited["allow_speed"] = json.RawMessage("true")
			encoded, err := common.Marshal(edited)
			require.NoError(t, err)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Updates(map[string]any{"settings": string(encoded), "used_quota": 4321, "balance": 12.5}).Error)
			loaded, err := LoadBackup(backup)
			require.NoError(t, err)
			require.NoError(t, Rollback(db, loaded))
			require.NoError(t, Rollback(db, loaded))
			var restored model.Channel
			require.NoError(t, db.First(&restored, 1).Error)
			assert.EqualValues(t, 4321, restored.UsedQuota)
			assert.Equal(t, 12.5, restored.Balance)
			require.NoError(t, common.UnmarshalJsonStr(restored.OtherSettings, &edited))
			assert.JSONEq(t, "true", string(edited["allow_speed"]))
			assert.Contains(t, restored.OtherSettings, "protocol_capabilities")
			var nullCount int64
			require.NoError(t, db.Model(&model.Channel{}).Where(map[string]any{"id": 4, "settings": nil}).Count(&nullCount).Error)
			assert.EqualValues(t, 1, nullCount)
			var untouched model.Option
			require.NoError(t, db.Where(map[string]any{"key": "untouched"}).First(&untouched).Error)
			assert.Equal(t, "keep", untouched.Value)
		})
	}
}

func TestProtocolMigrationBlocksInvalidAndConcurrentConfiguration(t *testing.T) {
	db := migrationDatabase(t, "sqlite")
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Key: "secret", Models: "model", OtherSettings: `{"protocol_capabilities":{"upstream_protocols":["unknown"]}}`}).Error)
	manifest, err := Preflight(db)
	require.NoError(t, err)
	require.NotEmpty(t, manifest.Blockers)
	require.Error(t, SaveBackup(filepath.Join(t.TempDir(), "blocked.json"), manifest))
	require.Error(t, Apply(db, manifest))
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).UpdateColumn("settings", "{}").Error)
	manifest, err = Preflight(db)
	require.NoError(t, err)
	require.Empty(t, manifest.Blockers)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).UpdateColumn("settings", `{"allow_speed":true}`).Error)
	require.ErrorContains(t, Apply(db, manifest), "changed since preflight")
	var count int64
	require.NoError(t, db.Model(&model.Option{}).Where(map[string]any{"key": optionKey}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestProtocolMigrationFreshDatabaseDefaults(t *testing.T) {
	db := migrationDatabase(t, "sqlite")
	backupDirectory := t.TempDir()
	require.NoError(t, MigrateOnStartup(db, backupDirectory))
	require.NoError(t, MigrateOnStartup(db, backupDirectory))
	var option model.Option
	require.NoError(t, db.Where(map[string]any{"key": optionKey}).First(&option).Error)
	var policy hostdto.ProtocolPolicy
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &policy))
	assert.Equal(t, hostdto.ProtocolConversionSafe, policy.Conversion)
	assert.Equal(t, hostdto.ProtocolStateBridge, policy.StateScope)
	files, err := os.ReadDir(backupDirectory)
	require.NoError(t, err)
	assert.Len(t, files, 1)
}

func TestProtocolMigrationImportsPreservePassthroughAndRejectUnknownFields(t *testing.T) {
	global := model_setting.GlobalSettings{ProtocolPolicy: common.GetPointer(hostdto.DefaultProtocolPolicy())}
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Models: "public", Setting: common.GetPointer(`{"pass_through_body_enabled":true}`)}
	encoded, _, err := NormalizeChannel(channel, global)
	require.NoError(t, err)
	var settings hostdto.ChannelOtherSettings
	require.NoError(t, common.UnmarshalJsonStr(encoded, &settings))
	require.NotNil(t, settings.ProtocolPolicy)
	assert.Equal(t, hostdto.ProtocolRequestPassthrough, settings.ProtocolPolicy.RequestMode)
	for _, invalid := range []string{
		`{"protocol_capabilities":{"new_unknown_policy":true}}`,
		`{"protocol_capabilities":{"model_overrides":[{"model_pattern":".*","unknown":true}]}}`,
		`{"protocol_policy":{"version":1,"rules":[{"unknown":true}]}}`,
	} {
		channel.OtherSettings = invalid
		_, _, err := NormalizeChannel(channel, global)
		require.ErrorContains(t, err, "unrecognized fields")
	}
}
