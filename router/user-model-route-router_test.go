package router

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/stretchr/testify/assert"
)

func TestUserModelRouteEndpointsRequireChannelPermissions(t *testing.T) {
	tests := []struct {
		method     string
		path       string
		permission authz.Permission
		handler    any
	}{
		{method: http.MethodGet, path: "/:id/model-route-candidates", permission: authz.ChannelRead, handler: controller.GetUserModelRouteCandidates},
		{method: http.MethodGet, path: "/:id/model-routes", permission: authz.ChannelRead, handler: controller.GetUserModelRoutes},
		{method: http.MethodPost, path: "/:id/model-routes", permission: authz.ChannelWrite, handler: controller.CreateUserModelRoute},
		{method: http.MethodPut, path: "/:id/model-routes", permission: authz.ChannelWrite, handler: controller.ReplaceUserModelRoutes},
		{method: http.MethodPost, path: "/batch/model-routes", permission: authz.ChannelWrite, handler: controller.BatchAddUserModelRoutes},
		{method: http.MethodPut, path: "/:id/model-routes/:route_id", permission: authz.ChannelWrite, handler: controller.UpdateUserModelRoute},
		{method: http.MethodPatch, path: "/:id/model-routes/:route_id/enabled", permission: authz.ChannelWrite, handler: controller.SetUserModelRouteEnabled},
		{method: http.MethodDelete, path: "/:id/model-routes/:route_id", permission: authz.ChannelWrite, handler: controller.DeleteUserModelRoute},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			for _, route := range userModelRoutePermissionRoutes {
				if route.method != test.method || route.path != test.path {
					continue
				}
				assert.Equal(t, test.permission, route.permission)
				assert.Equal(t, reflect.ValueOf(test.handler).Pointer(), reflect.ValueOf(route.handler).Pointer())
				return
			}
			t.Fatalf("route %s %s not found", test.method, test.path)
		})
	}
}
