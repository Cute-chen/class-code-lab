package app

import "github.com/gin-gonic/gin"

func (a *App) AuthMiddleware() gin.HandlerFunc { return a.authMiddleware() }
func RequireRole(role string) gin.HandlerFunc  { return requireRole(role) }
func RequirePasswordChanged() gin.HandlerFunc  { return requirePasswordChanged() }

func (a *App) HandlePublicClasses() gin.HandlerFunc        { return a.handlePublicClasses }
func (a *App) HandlePublicRoster() gin.HandlerFunc         { return a.handlePublicRoster }
func (a *App) HandleLogin() gin.HandlerFunc                { return a.handleLogin }
func (a *App) HandleMe() gin.HandlerFunc                   { return a.handleMe }
func (a *App) HandleLogout() gin.HandlerFunc               { return a.handleLogout }
func (a *App) HandleChangePassword() gin.HandlerFunc       { return a.handleChangePassword }
func (a *App) HandleStudentWork() gin.HandlerFunc          { return a.handleStudentWork }
func (a *App) HandleSaveStudentWork() gin.HandlerFunc      { return a.handleSaveStudentWork }
func (a *App) HandleRestoreRevision() gin.HandlerFunc      { return a.handleRestoreRevision }
func (a *App) HandlePreviewToken() gin.HandlerFunc         { return a.handlePreviewToken }
func (a *App) HandlePublishWork() gin.HandlerFunc          { return a.handlePublishWork }
func (a *App) HandleSaveWorkThumbnail() gin.HandlerFunc    { return a.handleSaveWorkThumbnail }
func (a *App) HandleUnpublishOwnWork() gin.HandlerFunc     { return a.handleUnpublishOwnWork }
func (a *App) HandleAIHistory() gin.HandlerFunc            { return a.handleAIHistory }
func (a *App) HandleAIMessage() gin.HandlerFunc            { return a.handleAIMessage }
func (a *App) HandleApplyProposal() gin.HandlerFunc        { return a.handleApplyProposal }
func (a *App) HandleGallery() gin.HandlerFunc              { return a.handleGallery }
func (a *App) HandleFeatured() gin.HandlerFunc             { return a.handleFeatured }
func (a *App) HandleGalleryWork() gin.HandlerFunc          { return a.handleGalleryWork }
func (a *App) HandleGalleryThumbnail() gin.HandlerFunc     { return a.handleGalleryThumbnail }
func (a *App) HandleGalleryRunToken() gin.HandlerFunc      { return a.handleGalleryRunToken }
func (a *App) HandleScoreWork() gin.HandlerFunc            { return a.handleScoreWork }
func (a *App) HandleTeacherOverview() gin.HandlerFunc      { return a.handleTeacherOverview }
func (a *App) HandleTeacherClasses() gin.HandlerFunc       { return a.handleTeacherClasses }
func (a *App) HandleCreateClass() gin.HandlerFunc          { return a.handleCreateClass }
func (a *App) HandleUpdateClass() gin.HandlerFunc          { return a.handleUpdateClass }
func (a *App) HandleTeacherStudents() gin.HandlerFunc      { return a.handleTeacherStudents }
func (a *App) HandleCreateStudent() gin.HandlerFunc        { return a.handleCreateStudent }
func (a *App) HandleImportStudents() gin.HandlerFunc       { return a.handleImportStudents }
func (a *App) HandleStudentTemplate() gin.HandlerFunc      { return a.handleStudentTemplate }
func (a *App) HandleResetStudentPassword() gin.HandlerFunc { return a.handleResetStudentPassword }
func (a *App) HandleToggleStudent() gin.HandlerFunc        { return a.handleToggleStudent }
func (a *App) HandleTeacherStudentAI() gin.HandlerFunc     { return a.handleTeacherStudentAI }
func (a *App) HandleTeacherWorks() gin.HandlerFunc         { return a.handleTeacherWorks }
func (a *App) HandleTeacherWorkThumbnail() gin.HandlerFunc { return a.handleTeacherWorkThumbnail }
func (a *App) HandleTeacherWorkAction() gin.HandlerFunc    { return a.handleTeacherWorkAction }
func (a *App) HandleTeacherAIUsage() gin.HandlerFunc       { return a.handleTeacherAIUsage }
func (a *App) HandleTeacherConversations() gin.HandlerFunc { return a.handleTeacherConversations }
func (a *App) HandleTeacherHealth() gin.HandlerFunc        { return a.handleTeacherHealth }
func (a *App) HandleTeacherTestAI() gin.HandlerFunc        { return a.handleTeacherTestAI }
func (a *App) HandleTeacherAudit() gin.HandlerFunc         { return a.handleTeacherAudit }
