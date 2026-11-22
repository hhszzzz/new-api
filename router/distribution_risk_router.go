package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerDistributionRiskRoutes(apiRouter *gin.RouterGroup) {
	route := apiRouter.Group("/distribution-risk")
	route.Use(middleware.AdminAuth())
	handlePermissionRoute(route, "GET", "/config", authz.DistributionRiskManage, controller.GetDistributionRiskConfig)
	handlePermissionRoute(route, "PUT", "/config", authz.DistributionRiskManage, controller.UpdateDistributionRiskConfig)
	handlePermissionRoute(route, "GET", "/events", authz.DistributionRiskRead, controller.ListDistributionRiskEvents)
	handlePermissionRoute(route, "GET", "/events/:id", authz.DistributionRiskRead, controller.GetDistributionRiskEvent)
	handlePermissionRoute(route, "PATCH", "/events/:id/review", authz.DistributionRiskManage, controller.ReviewDistributionRiskEvent)
}
