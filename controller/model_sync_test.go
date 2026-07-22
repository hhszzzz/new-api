package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncUpstreamModelsInvalidatesPricingAfterCreatingMetadata(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id:     8801,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sync-cache-key",
		Name:   "sync-cache-channel",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "sync-cache-model",
		ChannelId: 8801,
		Enabled:   true,
	}).Error)

	model.InvalidatePricingCache()
	warmed := model.GetPricing()
	require.Len(t, warmed, 1)
	require.Equal(t, "sync-cache-model", warmed[0].ModelName)
	require.Empty(t, warmed[0].Description)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload any
		switch r.URL.Path {
		case "/api/newapi/models.json":
			payload = upstreamEnvelope[upstreamModel]{
				Success: true,
				Data: []upstreamModel{{
					ModelName:   "sync-cache-model",
					Description: "synced description",
					Status:      1,
					VendorName:  "Synced Vendor",
				}},
			}
		case "/api/newapi/vendors.json":
			payload = upstreamEnvelope[upstreamVendor]{
				Success: true,
				Data: []upstreamVendor{{
					Name:        "Synced Vendor",
					Description: "synced vendor description",
					Status:      1,
				}},
			}
		default:
			http.NotFound(w, r)
			return
		}
		body, err := common.Marshal(payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	t.Setenv("SYNC_UPSTREAM_BASE", server.URL)
	t.Setenv("SYNC_HTTP_RETRY", "1")

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/models/sync_upstream", bytes.NewBufferString(`{}`))
	context.Request.Header.Set("Content-Type", "application/json")
	SyncUpstreamModels(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			CreatedModels  int `json:"created_models"`
			CreatedVendors int `json:"created_vendors"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 1, response.Data.CreatedModels)
	assert.Equal(t, 1, response.Data.CreatedVendors)

	refreshed := model.GetPricing()
	require.Len(t, refreshed, 1)
	assert.Equal(t, "synced description", refreshed[0].Description)
	assert.NotZero(t, refreshed[0].VendorID)

	vendors := model.GetVendors()
	require.Len(t, vendors, 1)
	assert.Equal(t, "Synced Vendor", vendors[0].Name)
}
