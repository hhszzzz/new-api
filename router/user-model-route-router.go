package router

import (
	"net/http"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerUserModelRouteRoutes(adminRoute *gin.RouterGroup) {
	for _, route := range userModelRoutePermissionRoutes {
		adminRoute.Handle(
			route.method,
			route.path,
			middleware.RequirePermission(route.permission),
			route.handler,
		)
	}
}

var userModelRoutePermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "/:id/model-route-candidates", permission: authz.ChannelRead, handler: controller.GetUserModelRouteCandidates},
	{method: http.MethodGet, path: "/:id/model-routes", permission: authz.ChannelRead, handler: controller.GetUserModelRoutes},
	{method: http.MethodPost, path: "/:id/model-routes", permission: authz.ChannelWrite, handler: controller.CreateUserModelRoute},
	{method: http.MethodPut, path: "/:id/model-routes", permission: authz.ChannelWrite, handler: controller.ReplaceUserModelRoutes},
	{method: http.MethodPost, path: "/batch/model-routes", permission: authz.ChannelWrite, handler: controller.BatchAddUserModelRoutes},
	{method: http.MethodPut, path: "/:id/model-routes/:route_id", permission: authz.ChannelWrite, handler: controller.UpdateUserModelRoute},
	{method: http.MethodDelete, path: "/:id/model-routes/:route_id", permission: authz.ChannelWrite, handler: controller.DeleteUserModelRoute},
}
