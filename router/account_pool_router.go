package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func registerAccountPoolRoutes(apiRouter *gin.RouterGroup) {
	accountPoolRoute := apiRouter.Group("/account-pool")
	accountPoolRoute.Use(middleware.UserAuth(), middleware.DisableCache())
	{
		accountPoolRoute.GET("", controller.GetAccountPool)
		accountPoolRoute.POST("/refresh", controller.RefreshAccountPool)
	}

	accountPoolSettingsRoute := apiRouter.Group("/account-pool/settings")
	accountPoolSettingsRoute.Use(middleware.RootAuth(), middleware.DisableCache())
	{
		accountPoolSettingsRoute.GET("", controller.GetAccountPoolSettings)
		accountPoolSettingsRoute.PUT("", controller.UpdateAccountPoolSettings)
	}
}
