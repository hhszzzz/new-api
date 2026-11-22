package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/distribution_risk_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestAssessDistributionRiskCategory(t *testing.T) {
	tests := []struct {
		name  string
		value int
		p95   int
		p99   int
		floor int
		want  int
	}{
		{name: "below floor and baseline", value: 3, p95: 4, p99: 8, floor: 10, want: 0},
		{name: "p95 deviation", value: 5, p95: 4, p99: 8, floor: 10, want: 15},
		{name: "absolute floor", value: 10, p95: 4, p99: 11, floor: 10, want: 15},
		{name: "p99 deviation", value: 9, p95: 4, p99: 8, floor: 10, want: 30},
		{name: "twice the floor", value: 20, p95: 4, p99: 11, floor: 10, want: 30},
		{name: "more than twice p99", value: 17, p95: 4, p99: 8, floor: 10, want: 50},
		{name: "three times the floor", value: 30, p95: 4, p99: 8, floor: 10, want: 50},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, assessDistributionRiskCategory(test.value, test.p95, test.p99, test.floor))
		})
	}
}

func TestDistributionRiskPercentilesUseNearestRank(t *testing.T) {
	samples := []distributionRiskSample{
		{IPs: 1}, {IPs: 2}, {IPs: 3}, {IPs: 4}, {IPs: 5},
	}
	p95, p99 := distributionRiskPercentiles(samples, func(sample distributionRiskSample) int { return sample.IPs })
	assert.Equal(t, 5, p95)
	assert.Equal(t, 5, p99)
}

func TestHashDistributionRiskSessionDoesNotStoreClientValue(t *testing.T) {
	previousSecret := common.SessionSecret
	common.SessionSecret = "test-distribution-risk-secret"
	t.Cleanup(func() { common.SessionSecret = previousSecret })

	first := hashDistributionRiskSession("client-session-value")
	assert.NotEmpty(t, first)
	assert.NotContains(t, first, "client-session-value")
	assert.Equal(t, first, hashDistributionRiskSession(" client-session-value "))
	assert.NotEqual(t, first, hashDistributionRiskSession("another-client-session"))
	assert.Empty(t, hashDistributionRiskSession(strings.Repeat("x", 4097)))
}

func TestDistributionRiskPersistenceDatabaseMatrix(t *testing.T) {
	tests := []struct {
		name string
		env  string
		open func(string) gorm.Dialector
	}{
		{name: "sqlite", open: func(string) gorm.Dialector { return sqlite.Open(":memory:") }},
		{name: "mysql", env: "TEST_MYSQL_DSN", open: mysql.Open},
		{name: "postgres", env: "TEST_POSTGRES_DSN", open: func(dsn string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := ""
			if test.env != "" {
				dsn = strings.TrimSpace(os.Getenv(test.env))
				if dsn == "" {
					t.Skip(test.env + " is not configured")
				}
			}
			prefix := fmt.Sprintf("drt_%d_", time.Now().UnixNano())
			db, err := gorm.Open(test.open(dsn), &gorm.Config{
				NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
			})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB := model.DB
			previousDatabaseType := common.MainDatabaseType()
			previousRedisEnabled := common.RedisEnabled
			model.DB = db
			common.SetMainDatabaseType(common.DatabaseType(test.name))
			common.RedisEnabled = false
			t.Cleanup(func() {
				model.DB = previousDB
				common.SetMainDatabaseType(previousDatabaseType)
				common.RedisEnabled = previousRedisEnabled
				assert.NoError(t, db.Migrator().DropTable(&model.DistributionRiskHold{}, &model.DistributionRiskEvent{}, &model.Token{}))
				assert.NoError(t, sqlDB.Close())
			})

			// Token rows stand in for the schema and data already present before this feature.
			require.NoError(t, db.AutoMigrate(&model.Token{}))
			token := model.Token{Id: 41, UserId: 7, Key: "distribution-risk-matrix-token", Status: common.TokenStatusEnabled, ExpiredTime: -1}
			require.NoError(t, db.Create(&token).Error)
			require.NoError(t, db.AutoMigrate(&model.DistributionRiskEvent{}, &model.DistributionRiskHold{}))
			require.NoError(t, db.AutoMigrate(&model.DistributionRiskEvent{}, &model.DistributionRiskHold{}), "repeated migration must be idempotent")
			var preserved model.Token
			require.NoError(t, db.First(&preserved, token.Id).Error)
			assert.Equal(t, token.Key, preserved.Key)

			largeTokenIDs := make([]int, 7000)
			for index := range largeTokenIDs {
				largeTokenIDs[index] = 100_000_000 + index
			}
			largeEvidence := `{"padding":"` + strings.Repeat("x", 70*1024) + `"}`
			largeEvent := &model.DistributionRiskEvent{UserID: 7, Evidence: model.DistributionRiskJSON(largeEvidence)}
			require.NoError(t, model.CreateDistributionRiskEvent(largeEvent, false, largeTokenIDs))
			var roundTripped model.DistributionRiskEvent
			require.NoError(t, db.First(&roundTripped, largeEvent.ID).Error)
			assert.Greater(t, len(roundTripped.TokenIDs), 64*1024)
			assert.Equal(t, largeEvidence, string(roundTripped.Evidence))

			falsePositive := &model.DistributionRiskEvent{UserID: 7, Score: 65, Level: "medium", Evidence: `{"ips":["192.0.2.1"]}`}
			require.NoError(t, model.CreateDistributionRiskEvent(falsePositive, true, []int{41}))
			secondIncident := &model.DistributionRiskEvent{UserID: 7, Score: 90, Level: "high", Evidence: `{"ips":["192.0.2.2"]}`}
			require.NoError(t, model.CreateDistributionRiskEvent(secondIncident, true, []int{41}))
			held, err := model.IsDistributionRiskTokenHeld(41)
			require.NoError(t, err)
			assert.True(t, held)
			reviewedTokens, err := model.ReviewDistributionRiskEvent(falsePositive.ID, model.DistributionRiskFalsePositive, 9, "shared office network")
			require.NoError(t, err)
			assert.Equal(t, []int{41}, reviewedTokens)
			held, err = model.IsDistributionRiskTokenHeld(41)
			require.NoError(t, err)
			assert.True(t, held, "reviewing one incident must retain a separate incident hold")
			_, err = model.ReviewDistributionRiskEvent(secondIncident.ID, model.DistributionRiskFalsePositive, 9, "second review")
			require.NoError(t, err)
			held, err = model.IsDistributionRiskTokenHeld(41)
			require.NoError(t, err)
			assert.False(t, held)

			confirmed := &model.DistributionRiskEvent{UserID: 7, Score: 90, Level: "high", Evidence: `{"user_agents":["client"]}`}
			require.NoError(t, model.CreateDistributionRiskEvent(confirmed, true, []int{41}))
			_, err = model.ReviewDistributionRiskEvent(confirmed.ID, model.DistributionRiskConfirmed, 9, "confirmed downstream resale")
			require.NoError(t, err)
			require.NoError(t, db.First(&preserved, token.Id).Error)
			assert.Equal(t, common.TokenStatusDisabled, preserved.Status)
			held, err = model.IsDistributionRiskTokenHeld(41)
			require.NoError(t, err)
			assert.False(t, held)

			now := time.Now().Unix()
			resolved := model.DistributionRiskEvent{
				UserID: 7, Status: model.DistributionRiskFalsePositive,
				CreatedAt: now - int64(200*24*time.Hour/time.Second), Evidence: `{"ips":["198.51.100.8"]}`,
			}
			pending := model.DistributionRiskEvent{
				UserID: 8, Status: model.DistributionRiskPending,
				CreatedAt: now - int64(200*24*time.Hour/time.Second), Evidence: `{"ips":["198.51.100.9"]}`,
			}
			require.NoError(t, db.Create(&resolved).Error)
			require.NoError(t, db.Create(&pending).Error)
			require.NoError(t, model.PruneDistributionRiskData(time.Unix(now, 0)))
			var retained model.DistributionRiskEvent
			require.NoError(t, db.First(&retained, pending.ID).Error)
			assert.Empty(t, retained.Evidence)
			var resolvedCount int64
			require.NoError(t, db.Model(&model.DistributionRiskEvent{}).Where("id = ?", resolved.ID).Count(&resolvedCount).Error)
			assert.Zero(t, resolvedCount)
		})
	}
}

func TestCheckDistributionRiskEnforcesAndKeepsPersistedHoldWithoutRedis(t *testing.T) {
	previousDB := model.DB
	previousRedis := common.RDB
	previousRedisEnabled := common.RedisEnabled
	previousDatabaseType := common.MainDatabaseType()
	previousSetting := distribution_risk_setting.GetSnapshot()
	previousLastPrune := distributionRiskLastPrune.Load()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/distribution-risk.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.DistributionRiskEvent{}, &model.DistributionRiskHold{}))
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB = client
	common.RedisEnabled = true
	distributionRiskHoldCache.Delete(41)
	distributionRiskLastPrune.Store(0)

	_, err = config.GlobalConfig.Update("distribution_risk_setting", map[string]string{
		"enabled": "true", "started_at": strconv.FormatInt(time.Now().Unix(), 10),
		"ip_floor": "2", "user_agent_floor": "2", "session_floor": "2",
		"requests_per_minute_floor": "100000", "concurrency_floor": "10000",
		"min_requests": "100", "min_active_days": "3",
	})
	require.NoError(t, err)
	require.Equal(t, distribution_risk_setting.ModeShadow, distribution_risk_setting.CurrentMode())

	t.Cleanup(func() {
		_, restoreErr := config.GlobalConfig.Update("distribution_risk_setting", map[string]string{
			"enabled": strconv.FormatBool(previousSetting.Enabled), "started_at": strconv.FormatInt(previousSetting.StartedAt, 10),
			"ip_floor": strconv.Itoa(previousSetting.IPFloor), "user_agent_floor": strconv.Itoa(previousSetting.UserAgentFloor),
			"session_floor":             strconv.Itoa(previousSetting.SessionFloor),
			"requests_per_minute_floor": strconv.Itoa(previousSetting.RequestsPerMinute),
			"concurrency_floor":         strconv.Itoa(previousSetting.ConcurrencyFloor),
			"min_requests":              strconv.Itoa(previousSetting.MinRequests), "min_active_days": strconv.Itoa(previousSetting.MinActiveDays),
		})
		assert.NoError(t, restoreErr)
		assert.NoError(t, client.Close())
		server.Close()
		assert.NoError(t, sqlDB.Close())
		model.DB = previousDB
		common.RDB = previousRedis
		common.RedisEnabled = previousRedisEnabled
		common.SetMainDatabaseType(previousDatabaseType)
		distributionRiskHoldCache.Delete(41)
		distributionRiskLastPrune.Store(previousLastPrune)
	})

	now := time.Now().UTC()
	ctx := context.Background()
	for daysAgo := 1; daysAgo <= 3; daysAgo++ {
		dayKey := now.AddDate(0, 0, -daysAgo).Format("20060102")
		for _, scopeKey := range []string{"dr:v1:{user:7}", "dr:v1:{token:41}"} {
			key := scopeKey + ":day:" + dayKey
			require.NoError(t, client.HSet(ctx, key+":stats", "requests", 40, "peak_rate", 1, "peak_concurrent", 1).Err())
			require.NoError(t, client.SAdd(ctx, key+":ips", "baseline-ip").Err())
			require.NoError(t, client.SAdd(ctx, key+":agents", "baseline-agent").Err())
			require.NoError(t, client.SAdd(ctx, key+":sessions", "baseline-session").Err())
		}
	}

	for _, input := range []struct{ ip, agent, session string }{
		{"baseline-ip", "baseline-agent", "baseline-session"},
		{"192.0.2.1", "agent-1", "session-1"},
	} {
		decision, err := CheckDistributionRisk(newDistributionRiskTestContext(), 7, 41, input.ip, input.agent, input.session)
		require.NoError(t, err)
		assert.False(t, decision.Blocked)
		if decision.Release != nil {
			decision.Release()
		}
	}
	shadowDecision, err := CheckDistributionRisk(newDistributionRiskTestContext(), 7, 41, "192.0.2.2", "agent-2", "session-2")
	require.NoError(t, err)
	assert.False(t, shadowDecision.Blocked, "shadow mode must never enforce a new restriction")
	shadowDecision.Release()
	held, err := model.IsDistributionRiskTokenHeld(41)
	require.NoError(t, err)
	assert.False(t, held, "shadow mode must not create durable holds")

	_, err = config.GlobalConfig.Update("distribution_risk_setting", map[string]string{
		"enabled": "true", "started_at": strconv.FormatInt(time.Now().Add(-8*24*time.Hour).Unix(), 10),
		"ip_floor": "2", "user_agent_floor": "2", "session_floor": "2",
		"requests_per_minute_floor": "100000", "concurrency_floor": "10000",
		"min_requests": "100", "min_active_days": "3",
	})
	require.NoError(t, err)
	blockedDecision, err := CheckDistributionRisk(newDistributionRiskTestContext(), 7, 41, "192.0.2.3", "agent-3", "session-3")
	require.NoError(t, err)
	assert.True(t, blockedDecision.Blocked)
	require.NotNil(t, blockedDecision.Release)
	blockedDecision.Release()
	held, err = model.IsDistributionRiskTokenHeld(41)
	require.NoError(t, err)
	assert.True(t, held)

	distributionRiskHoldCache.Store(41, distributionRiskHoldCacheEntry{
		held: false, expiresAt: time.Now().Add(time.Hour),
	})
	server.Close()
	stillBlocked, err := CheckDistributionRisk(newDistributionRiskTestContext(), 7, 41, "192.0.2.4", "agent-4", "session-4")
	require.NoError(t, err)
	assert.True(t, stillBlocked.Blocked, "persisted high-risk holds must survive Redis failure")
}

func newDistributionRiskTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c
}
