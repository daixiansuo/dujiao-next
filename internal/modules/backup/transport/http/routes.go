package backuphttp

import "github.com/gin-gonic/gin"

func RegisterAdminRoutes(admin gin.IRoutes, handler *Handler) {
	admin.GET("/backups/config", handler.GetConfig)
	admin.PUT("/backups/config", handler.UpdateConfig)
	admin.POST("/backups/test", handler.Test)
	admin.GET("/backups/status", handler.Status)
	admin.GET("/backups/records", handler.List)
	admin.POST("/backups/records", handler.Start)
	admin.POST("/backups/records/:id/download", handler.Download)
	admin.DELETE("/backups/records/:id", handler.Delete)
}
