package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeChannelsIntoAggregateCreatesAndLinksInOneRequest(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelAggregate{}, &model.Log{}))
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 71, Name: "first merge target", Key: "first-key"},
		{Id: 72, Name: "second merge target", Key: "second-key"},
	}).Error)

	body, err := common.Marshal(mergeChannelsIntoAggregateRequest{
		IDs: []int{72, 71},
		NewAggregate: &model.ChannelAggregate{
			Name:    "controller aggregate",
			BaseURL: "https://shared.example.com/v1/",
		},
		InheritAggregateBaseURL: true,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/channel/aggregates/merge",
		bytes.NewReader(body),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	MergeChannelsIntoAggregate(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	response := struct {
		Success bool `json:"success"`
		Data    struct {
			Aggregate model.ChannelAggregate `json:"aggregate"`
			Updated   int                    `json:"updated"`
		} `json:"data"`
	}{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, 2, response.Data.Updated)
	assert.Equal(t, "controller aggregate", response.Data.Aggregate.Name)
	assert.Equal(t, "https://shared.example.com/v1", response.Data.Aggregate.BaseURL)
	assert.Equal(t, int64(2), response.Data.Aggregate.ChildCount)

	var channels []model.Channel
	require.NoError(t, db.Order("id ASC").Find(&channels).Error)
	require.Len(t, channels, 2)
	for _, channel := range channels {
		require.NotNil(t, channel.AggregateId)
		assert.Equal(t, response.Data.Aggregate.Id, *channel.AggregateId)
		assert.True(t, channel.InheritAggregateBaseURL)
	}
}

func TestDetachChannelsFromAggregatesRemovesOnlyExistingLinks(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelAggregate{}, &model.Log{}))
	aggregate := &model.ChannelAggregate{Name: "controller detach aggregate"}
	require.NoError(t, model.SaveChannelAggregate(aggregate))
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 81, Name: "linked", Key: "linked-key", AggregateId: &aggregate.Id, InheritAggregateBaseURL: true},
		{Id: 82, Name: "standalone", Key: "standalone-key"},
	}).Error)

	body, err := common.Marshal(detachChannelsFromAggregatesRequest{IDs: []int{82, 81}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/channel/aggregates/detach",
		bytes.NewReader(body),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	DetachChannelsFromAggregates(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	response := struct {
		Success bool `json:"success"`
		Data    struct {
			Updated int `json:"updated"`
		} `json:"data"`
	}{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, 1, response.Data.Updated)

	var linked model.Channel
	require.NoError(t, db.First(&linked, 81).Error)
	assert.Nil(t, linked.AggregateId)
	assert.False(t, linked.InheritAggregateBaseURL)
}
