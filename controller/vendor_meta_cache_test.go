package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVendorCRUDInvalidatesPricingVendorCache(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	router := gin.New()
	router.POST("/vendors", CreateVendorMeta)
	router.PUT("/vendors", UpdateVendorMeta)
	router.DELETE("/vendors/:id", DeleteVendorMeta)

	require.Empty(t, model.GetVendors())

	create := httptest.NewRequest(http.MethodPost, "/vendors", strings.NewReader(`{"name":"cache-vendor","description":"created"}`))
	create.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, create)
	require.Equal(t, http.StatusOK, createRecorder.Code)

	var vendor model.Vendor
	require.NoError(t, db.Where("name = ?", "cache-vendor").First(&vendor).Error)
	require.Len(t, model.GetVendors(), 1)
	assert.Equal(t, "created", model.GetVendors()[0].Description)

	updateBody := `{"id":` + strconv.Itoa(vendor.Id) + `,"name":"cache-vendor","description":"updated"}`
	update := httptest.NewRequest(http.MethodPut, "/vendors", strings.NewReader(updateBody))
	update.Header.Set("Content-Type", "application/json")
	updateRecorder := httptest.NewRecorder()
	router.ServeHTTP(updateRecorder, update)
	require.Equal(t, http.StatusOK, updateRecorder.Code)
	require.Len(t, model.GetVendors(), 1)
	assert.Equal(t, "updated", model.GetVendors()[0].Description)

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/vendors/"+strconv.Itoa(vendor.Id), nil)
	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, deleteRequest)
	require.Equal(t, http.StatusOK, deleteRecorder.Code)
	assert.Empty(t, model.GetVendors())
}
