package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// SetTaskRouter registers the generic task-plugin API surface.
//
// Gin requires every route sharing a path position to use the same wildcard
// name, so the first segment is uniformly ":key"; it carries the plugin key
// on submit routes and the task id on read routes.
func SetTaskRouter(router *gin.Engine) {
	taskSubmitRouter := router.Group("/v1/tasks")
	taskSubmitRouter.Use(middleware.RouteTag("relay"), middleware.TokenAuth())
	taskSubmitRouter.Use(middleware.SiteClosureAPI())
	{
		taskSubmitRouter.POST("/:key", middleware.PrepareTaskPluginSubmit(), middleware.Distribute(), controller.RelayTask)
	}

	taskReadRouter := router.Group("/v1/tasks")
	taskReadRouter.Use(middleware.RouteTag("relay"), middleware.TokenAuth())
	taskReadRouter.Use(middleware.SiteClosureAPI())
	{
		taskReadRouter.GET("/:key", controller.GetTask)
		taskReadRouter.GET("/:key/artifacts", controller.GetTaskArtifacts)
	}

	// Content capability URLs are gated too: pausing the API service must also
	// pause downloads. Capability requests have no user role, so they are
	// blocked for everyone; normal token requests still allow admin bypass.
	taskContentRouter := router.Group("/v1/tasks")
	taskContentRouter.Use(
		middleware.RouteTag("relay"),
		middleware.TokenOrTaskArtifactAccessAuth("key", "artifact_key"),
		middleware.SiteClosureAPI(),
	)
	{
		taskContentRouter.GET("/:key/artifacts/:artifact_key/content", controller.TaskArtifactContent)
		taskContentRouter.HEAD("/:key/artifacts/:artifact_key/content", controller.TaskArtifactContent)
	}
}
