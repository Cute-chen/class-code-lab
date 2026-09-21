package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"class-code-lab/backend/internal/app"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg := app.LoadConfig()
	db, err := app.OpenDatabase(cfg)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	service := app.New(cfg, db)
	service.StartCleanupLoop()

	go func() {
		log.Printf("runner listening on %s", cfg.RunnerAddress)
		if err := service.RunnerRouter().Run(cfg.RunnerAddress); err != nil {
			log.Fatalf("runner: %v", err)
		}
	}()

	router := buildRouter(service)
	log.Printf("class-code-lab listening on %s", cfg.AppAddress)
	if err := router.Run(cfg.AppAddress); err != nil {
		log.Fatal(err)
	}
}

func buildRouter(service *app.App) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/api/health", func(c *gin.Context) {
		if sqlDB, err := service.DB.DB(); err != nil || sqlDB.Ping() != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "database": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "database": true, "ai_configured": service.AIClient.Configured()})
	})
	public := router.Group("/api/public")
	public.GET("/classes", service.HandlePublicClasses())
	public.GET("/classes/:id/roster", service.HandlePublicRoster())

	auth := router.Group("/api/auth")
	auth.POST("/login", service.HandleLogin())
	auth.Use(service.AuthMiddleware())
	auth.GET("/me", service.HandleMe())
	auth.POST("/logout", service.HandleLogout())
	auth.POST("/change-password", service.HandleChangePassword())

	student := router.Group("/api")
	student.Use(service.AuthMiddleware(), app.RequireRole("student"), app.RequirePasswordChanged())
	student.GET("/student/work", service.HandleStudentWork())
	student.PUT("/student/work", service.HandleSaveStudentWork())
	student.POST("/student/work/restore/:id", service.HandleRestoreRevision())
	student.POST("/student/work/preview-token", service.HandlePreviewToken())
	student.POST("/student/work/publish", service.HandlePublishWork())
	student.POST("/student/work/thumbnail", service.HandleSaveWorkThumbnail())
	student.POST("/student/work/unpublish", service.HandleUnpublishOwnWork())
	student.GET("/student/ai/history", service.HandleAIHistory())
	student.POST("/student/ai/messages", service.HandleAIMessage())
	student.POST("/student/ai/proposals/:id/apply", service.HandleApplyProposal())
	student.GET("/gallery", service.HandleGallery())
	student.GET("/featured", service.HandleFeatured())
	student.GET("/gallery/:id", service.HandleGalleryWork())
	student.GET("/gallery/:id/thumbnail", service.HandleGalleryThumbnail())
	student.POST("/gallery/:id/run-token", service.HandleGalleryRunToken())
	student.PUT("/gallery/:id/score", service.HandleScoreWork())

	teacher := router.Group("/api/teacher")
	teacher.Use(service.AuthMiddleware(), app.RequireRole("teacher"), app.RequirePasswordChanged())
	teacher.GET("/overview", service.HandleTeacherOverview())
	teacher.GET("/classes", service.HandleTeacherClasses())
	teacher.POST("/classes", service.HandleCreateClass())
	teacher.PATCH("/classes/:id", service.HandleUpdateClass())
	teacher.GET("/classes/:id/students", service.HandleTeacherStudents())
	teacher.POST("/classes/:id/students", service.HandleCreateStudent())
	teacher.POST("/classes/:id/import", service.HandleImportStudents())
	teacher.GET("/students/template", service.HandleStudentTemplate())
	teacher.POST("/students/:id/reset-password", service.HandleResetStudentPassword())
	teacher.PATCH("/students/:id/status", service.HandleToggleStudent())
	teacher.POST("/students/:id/add-ai", service.HandleTeacherStudentAI())
	teacher.GET("/classes/:id/works", service.HandleTeacherWorks())
	teacher.GET("/works/:id/thumbnail", service.HandleTeacherWorkThumbnail())
	teacher.POST("/works/:id/:action", service.HandleTeacherWorkAction())
	teacher.GET("/ai/usage", service.HandleTeacherAIUsage())
	teacher.GET("/ai/conversations", service.HandleTeacherConversations())
	teacher.GET("/ai/health", service.HandleTeacherHealth())
	teacher.POST("/ai/test", service.HandleTeacherTestAI())
	teacher.GET("/audit-logs", service.HandleTeacherAudit())

	serveFrontend(router, service.Config.FrontendDist)
	return router
}

func serveFrontend(router *gin.Engine, dist string) {
	if strings.TrimSpace(dist) == "" {
		return
	}
	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		clean := filepath.Clean(strings.TrimPrefix(c.Request.URL.Path, "/"))
		if clean != "." && !strings.HasPrefix(clean, "..") {
			candidate := filepath.Join(dist, clean)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				c.File(candidate)
				return
			}
		}
		index := filepath.Join(dist, "index.html")
		if _, err := os.Stat(index); err == nil {
			c.File(index)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "前端尚未构建，请先执行 npm run build"})
	})
}
