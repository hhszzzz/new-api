package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
)

func registerPromptAuditRoutes(apiRouter *gin.RouterGroup) {
	route := apiRouter.Group("/prompt-audit")
	route.Use(middleware.AdminAuth())

	// The catalog is static, non-sensitive configuration metadata and is needed by
	// both the read and manage screens. Keep it available to any authenticated
	// administrator so independently granting manage does not implicitly require
	// read access to audit events.
	route.GET("/categories", controller.GetPromptAuditCategories)
	route.GET("/events", middleware.RequirePermission(authz.PromptAuditRead), controller.ListPromptAudits)
	route.GET("/events/:id", middleware.RequirePermission(authz.PromptAuditRead), controller.GetPromptAudit)
	route.GET("/stats", middleware.RequirePermission(authz.PromptAuditRead), controller.GetPromptAuditStats)

	route.GET("/config", middleware.RequirePermission(authz.PromptAuditManage), controller.GetPromptAuditConfig)
	route.PUT("/config", middleware.RequirePermission(authz.PromptAuditManage), controller.UpdatePromptAuditConfig)
	route.POST("/nodes/:id/test", middleware.RequirePermission(authz.PromptAuditManage), controller.TestPromptAuditNode)
	route.POST("/events/:id/retry", middleware.RequirePermission(authz.PromptAuditManage), controller.RetryPromptAudit)

	route.POST("/events/delete-preview", middleware.RequirePermission(authz.PromptAuditDelete), controller.PreviewDeletePromptAudits)
	route.DELETE("/events", middleware.RequirePermission(authz.PromptAuditDelete), controller.DeletePromptAudits)
}
