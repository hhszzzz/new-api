package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureModelRequestRateLimitTest(t *testing.T, totalMax, successMax int) {
	t.Helper()
	previousEnabled := setting.ModelRequestRateLimitEnabled
	previousDuration := setting.ModelRequestRateLimitDurationMinutes
	previousTotal := setting.ModelRequestRateLimitCount
	previousSuccess := setting.ModelRequestRateLimitSuccessCount
	previousRedis := common.RedisEnabled
	previousRDB := common.RDB
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = totalMax
	setting.ModelRequestRateLimitSuccessCount = successMax
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = previousEnabled
		setting.ModelRequestRateLimitDurationMinutes = previousDuration
		setting.ModelRequestRateLimitCount = previousTotal
		setting.ModelRequestRateLimitSuccessCount = previousSuccess
		common.RedisEnabled = previousRedis
		common.RDB = previousRDB
	})
}

func newModelRateLimitTestContext(userID int) *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", userID)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return ctx
}

func TestModelRedisRateLimitUsesUTCRegardlessOfLocalTimezone(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("test-utc-plus-eight", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })

	ctx := context.Background()
	recordKey := "rateLimit:model-utc-record"
	recordRedisRequest(ctx, redisClient, recordKey, 2)
	recorded, err := redisClient.LIndex(ctx, recordKey, 0).Result()
	require.NoError(t, err)
	recordedAt, err := time.Parse(modelRateLimitTimeFormat, recorded)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC(), recordedAt, 2*time.Second)

	checkKey := "rateLimit:model-utc-check"
	withinWindow := time.Now().UTC().Add(-30 * time.Second).Format(modelRateLimitTimeFormat)
	_, err = redisServer.Push(checkKey, withinWindow, withinWindow)
	require.NoError(t, err)
	allowed, err := checkRedisRateLimit(ctx, redisClient, checkKey, 2, 60)
	require.NoError(t, err)
	assert.False(t, allowed, "an existing UTC timestamp inside the window must remain limited on a non-UTC host")
}

func TestResponsesWebSocketHandshakeDetectionRequiresRealUpgradeHeaders(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		connection string
		upgrade    string
		want       bool
	}{
		{name: "responses websocket", method: http.MethodGet, path: "/v1/responses", connection: "keep-alive, Upgrade", upgrade: "websocket", want: true},
		{name: "missing connection upgrade", method: http.MethodGet, path: "/v1/responses", upgrade: "websocket"},
		{name: "responses post", method: http.MethodPost, path: "/v1/responses", connection: "Upgrade", upgrade: "websocket"},
		{name: "other websocket path", method: http.MethodGet, path: "/v1/realtime", connection: "Upgrade", upgrade: "websocket"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(test.method, test.path, nil)
			ctx.Request.Header.Set("Connection", test.connection)
			ctx.Request.Header.Set("Upgrade", test.upgrade)
			assert.Equal(t, test.want, isResponsesWebSocketHandshake(ctx))
		})
	}
}

func TestCheckModelRequestRateLimitCountsEveryCreateButOnlyCommitsSuccess(t *testing.T) {
	configureModelRequestRateLimitTest(t, 0, 1)

	failedCommit, apiErr := CheckModelRequestRateLimit(newModelRateLimitTestContext(101))
	require.Nil(t, apiErr)
	failedCommit(false)

	successCommit, apiErr := CheckModelRequestRateLimit(newModelRateLimitTestContext(101))
	require.Nil(t, apiErr)
	successCommit(true)

	commit, apiErr := CheckModelRequestRateLimit(newModelRateLimitTestContext(101))
	assert.Nil(t, commit)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
}

func TestCheckModelRequestRateLimitCountsFailedRequestsTowardTotal(t *testing.T) {
	configureModelRequestRateLimitTest(t, 1, 0)

	commit, apiErr := CheckModelRequestRateLimit(newModelRateLimitTestContext(102))
	require.Nil(t, apiErr)
	commit(false)

	commit, apiErr = CheckModelRequestRateLimit(newModelRateLimitTestContext(102))
	assert.Nil(t, commit)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
}

func TestCheckModelRequestRateLimitFallsBackWhenRedisClientIsNil(t *testing.T) {
	configureModelRequestRateLimitTest(t, 1, 0)
	common.RedisEnabled = true
	common.RDB = nil

	assert.NotPanics(t, func() {
		commit, apiErr := CheckModelRequestRateLimit(newModelRateLimitTestContext(103))
		require.Nil(t, apiErr)
		commit(false)
	})
	commit, apiErr := CheckModelRequestRateLimit(newModelRateLimitTestContext(103))
	assert.Nil(t, commit)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
}

func TestModelRequestRateLimitDoesNotChargeResponsesWebSocketHandshake(t *testing.T) {
	configureModelRequestRateLimitTest(t, 1, 0)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 104)
		c.Next()
	})
	router.Use(ModelRequestRateLimit())
	router.Any("/v1/responses", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	handshake := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	handshake.Header.Set("Connection", "Upgrade")
	handshake.Header.Set("Upgrade", "websocket")
	handshakeResponse := httptest.NewRecorder()
	router.ServeHTTP(handshakeResponse, handshake)
	assert.Equal(t, http.StatusNoContent, handshakeResponse.Code)

	firstCreate := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, firstCreate)
	assert.Equal(t, http.StatusNoContent, firstResponse.Code)

	secondCreate := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	secondResponse := httptest.NewRecorder()
	router.ServeHTTP(secondResponse, secondCreate)
	assert.Equal(t, http.StatusTooManyRequests, secondResponse.Code)
}

func TestModelRequestSucceededHandlesRequestsWithoutStreamStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   bool
	}{
		{name: "successful non-stream response", status: http.StatusNoContent, want: true},
		{name: "failed response", status: http.StatusBadRequest, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Status(test.status)

			assert.Equal(t, test.want, modelRequestSucceeded(ctx))
		})
	}
}

var modelRateLimitTestUsers atomic.Int64

func TestModelRateLimitStreamFailuresDoNotConsumeSuccessLimit(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		for _, totalLimit := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/total=%d", backend, totalLimit), func(t *testing.T) {
				userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
				handler := memoryRateLimitHandler(60, totalLimit, 1)
				if backend == "redis" {
					useRateLimitMiniRedis(t)
					handler = redisRateLimitHandler(60, totalLimit, 1)
				}
				router := gin.New()
				router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, handler, func(c *gin.Context) {
					status := relaycommon.NewStreamStatus()
					if c.Param("outcome") == "failed" {
						status.MarkFailed("server_error", "", 0)
					} else if c.Param("outcome") == "truncated" {
						status.MarkTerminalFailure(fmt.Errorf("stream ended without a terminal event"))
					} else {
						status.MarkCompleted()
					}
					common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
					c.Status(http.StatusOK)
				})
				assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/truncated", "127.0.0.1:1000").Code)
				if totalLimit == 0 {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
				}
				assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
			})
		}
	}
}

func TestModelMemoryRateLimitReservesConcurrentSuccessAdmission(t *testing.T) {
	userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := gin.New()
	router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, memoryRateLimitHandler(60, 0, 1), func(c *gin.Context) {
		if c.Param("outcome") == "failed" {
			close(entered)
			<-release
			status := relaycommon.NewStreamStatus()
			status.MarkFailed("server_error", "", 0)
			common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
		}
		c.Status(http.StatusOK)
	})
	go func() {
		defer close(finished)
		assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
	}()
	<-entered
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	close(release)
	<-finished
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
}
