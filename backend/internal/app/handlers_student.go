package app

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxThumbnailBytes = 600 * 1024

func (a *App) getOrCreateWork(user User) (Work, error) {
	var work Work
	err := a.DB.Omit("thumbnail").Where("user_id = ?", user.ID).First(&work).Error
	if err == nil {
		return work, nil
	}
	if !isNotFound(err) {
		return Work{}, err
	}
	if user.ClassID == nil {
		return Work{}, gorm.ErrInvalidData
	}
	work = Work{UserID: user.ID, ClassID: *user.ClassID}
	if err := a.DB.Create(&work).Error; err != nil {
		return Work{}, err
	}
	return work, nil
}

func (a *App) handleStudentWork(c *gin.Context) {
	auth := authFrom(c)
	work, err := a.getOrCreateWork(auth.User)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "作品加载失败")
		return
	}
	var revisions []WorkRevision
	a.DB.Select("id", "work_id", "source", "summary", "created_at").Where("work_id = ?", work.ID).Order("id DESC").Limit(20).Find(&revisions)
	remaining := a.studentAIRemaining(auth.User, *auth.User.ClassID)
	c.JSON(http.StatusOK, gin.H{
		"work": work, "revisions": revisions, "safety": CheckHTMLSafety(work.DraftCode),
		"ai_remaining": remaining,
	})
}

func (a *App) handleSaveStudentWork(c *gin.Context) {
	var request struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Code        string `json:"code"`
		Source      string `json:"source"`
	}
	if !bindJSON(c, &request) {
		return
	}
	auth := authFrom(c)
	work, err := a.getOrCreateWork(auth.User)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "作品保存失败")
		return
	}
	if work.IsLocked {
		jsonError(c, http.StatusForbidden, "作品已被教师锁定，请联系教师")
		return
	}
	if auth.User.Class != nil && work.IsPublished && !auth.User.Class.AllowEditPublished {
		jsonError(c, http.StatusForbidden, "教师已暂停修改已发布作品")
		return
	}
	request.Title = strings.TrimSpace(request.Title)
	request.Description = strings.TrimSpace(request.Description)
	if len([]rune(request.Title)) > 40 || len([]rune(request.Description)) > 200 {
		jsonError(c, http.StatusBadRequest, "作品名称最多 40 字，简介最多 200 字")
		return
	}
	if len([]byte(request.Code)) > MaxCodeBytes {
		jsonError(c, http.StatusBadRequest, "代码不能超过 512KB")
		return
	}
	if request.Source == "" {
		request.Source = "manual"
	}
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		if work.DraftCode != "" && work.DraftCode != request.Code {
			revision := WorkRevision{WorkID: work.ID, Source: request.Source, Summary: "保存前版本", Code: work.DraftCode}
			if err := tx.Create(&revision).Error; err != nil {
				return err
			}
		}
		return tx.Model(&work).Updates(map[string]any{
			"title": request.Title, "description": request.Description, "draft_code": request.Code,
			"draft_cleared": strings.TrimSpace(request.Code) == "",
		}).Error
	})
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "作品保存失败")
		return
	}
	a.pruneRevisions(work.ID)
	work.Title, work.Description, work.DraftCode = request.Title, request.Description, request.Code
	work.DraftCleared = strings.TrimSpace(request.Code) == ""
	c.JSON(http.StatusOK, gin.H{"work": work, "safety": CheckHTMLSafety(request.Code)})
}

func (a *App) handleRestoreRevision(c *gin.Context) {
	revisionID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	work, err := a.getOrCreateWork(auth.User)
	if err != nil || work.IsLocked {
		jsonError(c, http.StatusForbidden, "当前无法恢复版本")
		return
	}
	var revision WorkRevision
	if err := a.DB.Where("id = ? AND work_id = ?", revisionID, work.ID).First(&revision).Error; err != nil {
		jsonError(c, http.StatusNotFound, "历史版本不存在")
		return
	}
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		if work.DraftCode != "" {
			if err := tx.Create(&WorkRevision{WorkID: work.ID, Source: "restore", Summary: "恢复前版本", Code: work.DraftCode}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&work).Updates(map[string]any{"draft_code": revision.Code, "draft_cleared": false}).Error
	})
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "版本恢复失败")
		return
	}
	a.pruneRevisions(work.ID)
	c.JSON(http.StatusOK, gin.H{"code": revision.Code, "safety": CheckHTMLSafety(revision.Code)})
}

func (a *App) pruneRevisions(workID uint) {
	var ids []uint
	a.DB.Model(&WorkRevision{}).Where("work_id = ?", workID).Order("id DESC").Pluck("id", &ids)
	if len(ids) > 30 {
		a.DB.Delete(&WorkRevision{}, ids[30:])
	}
}

func (a *App) handlePreviewToken(c *gin.Context) {
	auth := authFrom(c)
	work, err := a.getOrCreateWork(auth.User)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "预览准备失败")
		return
	}
	if work.IsLocked {
		jsonError(c, http.StatusForbidden, "作品已被教师锁定")
		return
	}
	a.createRunTokenResponse(c, &work.ID, work.ClassID, work.DraftCode)
}

func (a *App) createRunTokenResponse(c *gin.Context, workID *uint, classID uint, code string) {
	safety := CheckHTMLSafety(code)
	if !safety.OK {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "代码安全检查未通过", "safety": safety})
		return
	}
	token, expires, err := a.createRunToken(workID, classID, code)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "运行令牌创建失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": expires, "safety": safety})
}

func (a *App) createRunToken(workID *uint, classID uint, code string) (string, time.Time, error) {
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(a.Config.RunTokenTTL)
	entry := RunToken{TokenHash: hashToken(token), WorkID: workID, ClassID: classID, Code: code, ExpiresAt: expires}
	if err := a.DB.Create(&entry).Error; err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func (a *App) handlePublishWork(c *gin.Context) {
	auth := authFrom(c)
	work, err := a.getOrCreateWork(auth.User)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "发布失败")
		return
	}
	if work.IsLocked {
		jsonError(c, http.StatusForbidden, "作品已被教师锁定")
		return
	}
	if auth.User.Class == nil || !auth.User.Class.PublishEnabled {
		jsonError(c, http.StatusForbidden, "教师暂未开放作品发布")
		return
	}
	if strings.TrimSpace(work.Title) == "" {
		jsonError(c, http.StatusBadRequest, "请先填写作品名称")
		return
	}
	safety := CheckHTMLSafety(work.DraftCode)
	if !safety.OK {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "代码安全检查未通过", "safety": safety})
		return
	}
	now := time.Now()
	publishedVersion := work.PublishedVersion + 1
	if err := a.DB.Model(&work).Updates(map[string]any{
		"published_code": work.DraftCode, "published_version": publishedVersion,
		"is_published": true, "published_at": &now, "unpublished_reason": "",
		"thumbnail": nil, "thumbnail_media_type": "", "thumbnail_version": 0,
	}).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "发布失败")
		return
	}
	a.audit(&auth.User, auth.User.ClassID, "work", "publish", "success", work.Title)
	response := gin.H{"ok": true, "published_at": now, "version": publishedVersion}
	if token, _, tokenErr := a.createRunToken(&work.ID, work.ClassID, work.DraftCode); tokenErr == nil {
		response["capture_token"] = token
	}
	c.JSON(http.StatusOK, response)
}

func (a *App) handleSaveWorkThumbnail(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxThumbnailBytes*2)
	var request struct {
		Image   string `json:"image"`
		Version int    `json:"version"`
	}
	if !bindJSON(c, &request) {
		return
	}
	auth := authFrom(c)
	work, err := a.getOrCreateWork(auth.User)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "封面保存失败")
		return
	}
	if !work.IsPublished || request.Version != work.PublishedVersion {
		jsonError(c, http.StatusConflict, "作品已更新，请为最新发布版本重新生成封面")
		return
	}
	mediaType, encoded, ok := strings.Cut(request.Image, ",")
	if !ok || (mediaType != "data:image/jpeg;base64" && mediaType != "data:image/png;base64") {
		jsonError(c, http.StatusBadRequest, "封面图片格式不正确")
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > maxThumbnailBytes {
		jsonError(c, http.StatusBadRequest, "封面图片无效或过大")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 1920 || config.Height > 1080 {
		jsonError(c, http.StatusBadRequest, "封面图片尺寸不正确")
		return
	}
	if (format == "jpeg" && mediaType != "data:image/jpeg;base64") || (format == "png" && mediaType != "data:image/png;base64") || (format != "jpeg" && format != "png") {
		jsonError(c, http.StatusBadRequest, "封面图片内容与格式不匹配")
		return
	}
	storedMediaType := strings.TrimPrefix(strings.TrimSuffix(mediaType, ";base64"), "data:")
	if err := a.DB.Model(&work).Updates(map[string]any{
		"thumbnail": decoded, "thumbnail_media_type": storedMediaType, "thumbnail_version": request.Version,
	}).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "封面保存失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) handleUnpublishOwnWork(c *gin.Context) {
	auth := authFrom(c)
	work, err := a.getOrCreateWork(auth.User)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "撤下失败")
		return
	}
	if err := a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&work).Updates(map[string]any{"is_published": false, "is_featured": false}).Error; err != nil {
			return err
		}
		return tx.Where("work_id = ?", work.ID).Delete(&WorkScore{}).Error
	}); err != nil {
		jsonError(c, http.StatusInternalServerError, "撤下失败")
		return
	}
	a.audit(&auth.User, auth.User.ClassID, "work", "unpublish", "success", work.Title)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) studentAIRemaining(user User, classID uint) int {
	var class Class
	if err := a.DB.First(&class, classID).Error; err != nil {
		return 0
	}
	limit := class.AIRequestLimit
	var used int64
	a.DB.Model(&AIUsageLog{}).Where("user_id = ? AND status = ?", user.ID, "success").Count(&used)
	remaining := limit + user.AIExtraRequests - int(used)
	if remaining < 0 {
		return 0
	}
	return remaining
}
