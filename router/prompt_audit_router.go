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
	handlePermissionRoute(route, "GET", "/events", authz.PromptAuditRead, controller.ListPromptAudits)
	handlePermissionRoute(route, "GET", "/events/:id", authz.PromptAuditRead, controller.GetPromptAudit)
	handlePermissionRoute(route, "GET", "/events/:id/group-content", authz.PromptAuditRead, controller.GetPromptAuditGroupContent)
	handlePermissionRoute(route, "GET", "/events/:id/group-records", authz.PromptAuditRead, controller.GetPromptAuditGroupRecords)
	handlePermissionRoute(route, "GET", "/events/:id/session-questions", authz.PromptAuditRead, controller.GetPromptAuditSessionQuestions)
	handlePermissionRoute(route, "GET", "/stats", authz.PromptAuditRead, controller.GetPromptAuditStats)
	handlePermissionRoute(route, "GET", "/storage", authz.PromptAuditRead, controller.GetPromptAuditStorage)
	handlePermissionRoute(route, "GET", "/archives", authz.PromptAuditRead, controller.ListPromptAuditArchives)
	handlePermissionRoute(route, "GET", "/imports", authz.PromptAuditRead, controller.ListPromptAuditImports)
	handlePermissionRoute(route, "GET", "/imports/:source/events", authz.PromptAuditRead, controller.ListPromptAuditImportedEvents)
	handlePermissionRoute(route, "GET", "/imports/:source/events/:id", authz.PromptAuditRead, controller.GetPromptAuditImportedEvent)
	handlePermissionRoute(route, "GET", "/import-jobs/:id", authz.PromptAuditManage, controller.GetPromptAuditImportJob)
	handlePermissionRoute(route, "POST", "/imports", authz.PromptAuditManage, middleware.RequirePermission(authz.PromptAuditViewFullPrompt), controller.ImportPromptAuditArchive)

	handlePermissionRoute(route, "GET", "/config", authz.PromptAuditManage, controller.GetPromptAuditConfig)
	handlePermissionRoute(route, "PUT", "/config", authz.PromptAuditManage, controller.UpdatePromptAuditConfig)
	handlePermissionRoute(route, "GET", "/wordlists", authz.PromptAuditManage, controller.ListPromptWordlists)
	handlePermissionRoute(route, "POST", "/wordlists", authz.PromptAuditManage, controller.CreatePromptWordlist)
	handlePermissionRoute(route, "PUT", "/wordlists/:id", authz.PromptAuditManage, controller.UpdatePromptWordlist)
	handlePermissionRoute(route, "DELETE", "/wordlists/:id", authz.PromptAuditManage, controller.DeletePromptWordlist)
	handlePermissionRoute(route, "POST", "/wordlists/:id/sync", authz.PromptAuditManage, controller.SyncPromptWordlist)
	handlePermissionRoute(route, "GET", "/wordlists/manual/content", authz.PromptAuditManage, controller.GetManualPromptWordlist)
	handlePermissionRoute(route, "PUT", "/wordlists/manual/content", authz.PromptAuditManage, controller.UpdateManualPromptWordlist)
	handlePermissionRoute(route, "POST", "/wordlists/test", authz.PromptAuditManage, controller.TestPromptWordlists)
	handlePermissionRoute(route, "POST", "/nodes/:id/test", authz.PromptAuditManage, controller.TestPromptAuditNode)
	handlePermissionRoute(route, "POST", "/test", authz.PromptAuditManage, controller.TestPromptAuditPolicy)
	handlePermissionRoute(route, "POST", "/events/:id/retry", authz.PromptAuditManage, controller.RetryPromptAudit)
	handlePermissionRoute(route, "PATCH", "/events/:id/review", authz.PromptAuditManage, controller.ReviewPromptAudit)

	handlePermissionRoute(route, "POST", "/events/delete-preview", authz.PromptAuditDelete, controller.PreviewDeletePromptAudits)
	handlePermissionRoute(route, "DELETE", "/events", authz.PromptAuditDelete, controller.DeletePromptAudits)
}
