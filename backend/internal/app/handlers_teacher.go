package app

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *App) teacherClass(c *gin.Context) (Class, bool) {
	classID, ok := parseUintParam(c, "id")
	if !ok {
		return Class{}, false
	}
	auth := authFrom(c)
	if !a.canManageClass(auth.User.ID, classID) {
		jsonError(c, http.StatusForbidden, "你没有管理这个班级的权限")
		return Class{}, false
	}
	var class Class
	if err := a.DB.First(&class, classID).Error; err != nil {
		jsonError(c, http.StatusNotFound, "班级不存在")
		return Class{}, false
	}
	return class, true
}

func (a *App) handleTeacherOverview(c *gin.Context) {
	auth := authFrom(c)
	var classes []Class
	query := a.DB.Joins("JOIN teacher_class_accesses ON teacher_class_accesses.class_id = classes.id").Where("teacher_class_accesses.teacher_id = ?", auth.User.ID).Order("classes.name ASC").Find(&classes)
	if query.Error != nil {
		jsonError(c, http.StatusInternalServerError, "课堂总览加载失败")
		return
	}
	type stat struct {
		ClassID      uint   `json:"class_id"`
		StudentCount int64  `json:"student_count"`
		LoginCount   int64  `json:"login_count"`
		Published    int64  `json:"published"`
		AIQueue      int64  `json:"ai_queue"`
		ClassName    string `json:"class_name"`
	}
	stats := make([]stat, 0, len(classes))
	for _, class := range classes {
		var studentCount, loginCount, published, aiQueue int64
		a.DB.Model(&User{}).Where("class_id = ? AND role = ?", class.ID, RoleStudent).Count(&studentCount)
		a.DB.Model(&User{}).Where("class_id = ? AND role = ? AND last_login_at IS NOT NULL", class.ID, RoleStudent).Count(&loginCount)
		a.DB.Model(&Work{}).Where("class_id = ? AND is_published = ?", class.ID, true).Count(&published)
		a.DB.Model(&AIUsageLog{}).Where("class_id = ? AND status = ? AND created_at >= ?", class.ID, "pending", time.Now().Add(-10*time.Minute)).Count(&aiQueue)
		stats = append(stats, stat{ClassID: class.ID, ClassName: class.Name, StudentCount: studentCount, LoginCount: loginCount, Published: published, AIQueue: aiQueue})
	}
	c.JSON(http.StatusOK, gin.H{"classes": classes, "stats": stats, "ai_configured": a.AIClient.Configured(), "model": a.Config.AIModel})
}

func (a *App) handleTeacherClasses(c *gin.Context) {
	auth := authFrom(c)
	var classes []Class
	err := a.DB.Joins("JOIN teacher_class_accesses ON teacher_class_accesses.class_id = classes.id").Where("teacher_class_accesses.teacher_id = ?", auth.User.ID).Order("classes.name ASC").Find(&classes).Error
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "班级加载失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"classes": classes})
}

func (a *App) handleCreateClass(c *gin.Context) {
	var request struct {
		Name string `json:"name"`
	}
	if !bindJSON(c, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len([]rune(request.Name)) > 80 {
		jsonError(c, http.StatusBadRequest, "班级名称不能为空且最多 80 字")
		return
	}
	auth := authFrom(c)
	class := Class{Name: request.Name, AIRequestLimit: a.Config.AIRequestsPerStudent, AIConcurrency: a.Config.AIMaxConcurrency}
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&class).Error; err != nil {
			return err
		}
		return tx.Create(&TeacherClassAccess{TeacherID: auth.User.ID, ClassID: class.ID}).Error
	})
	if err != nil {
		jsonError(c, http.StatusConflict, "班级名称已存在或创建失败")
		return
	}
	a.audit(&auth.User, &class.ID, "class", "create", "success", class.Name)
	c.JSON(http.StatusCreated, gin.H{"class": class})
}

func (a *App) handleUpdateClass(c *gin.Context) {
	class, ok := a.teacherClass(c)
	if !ok {
		return
	}
	var request struct {
		Name               *string `json:"name"`
		Active             *bool   `json:"active"`
		LoginOpen          *bool   `json:"login_open"`
		AIEnabled          *bool   `json:"ai_enabled"`
		PublishEnabled     *bool   `json:"publish_enabled"`
		AllowEditPublished *bool   `json:"allow_edit_published"`
		AIRequestLimit     *int    `json:"ai_request_limit"`
		AIConcurrency      *int    `json:"ai_concurrency"`
		CooldownSeconds    *int    `json:"ai_request_cooldown_seconds"`
		ScoreBudget        *int    `json:"score_budget"`
	}
	if !bindJSON(c, &request) {
		return
	}
	updates := map[string]any{}
	if request.Name != nil {
		updates["name"] = strings.TrimSpace(*request.Name)
	}
	if request.Active != nil {
		updates["active"] = *request.Active
	}
	if request.LoginOpen != nil {
		updates["login_open"] = *request.LoginOpen
	}
	if request.AIEnabled != nil {
		updates["ai_enabled"] = *request.AIEnabled
	}
	if request.PublishEnabled != nil {
		updates["publish_enabled"] = *request.PublishEnabled
	}
	if request.AllowEditPublished != nil {
		updates["allow_edit_published"] = *request.AllowEditPublished
	}
	if request.AIRequestLimit != nil && *request.AIRequestLimit >= 0 {
		updates["ai_request_limit"] = *request.AIRequestLimit
	}
	if request.AIConcurrency != nil && *request.AIConcurrency > 0 {
		updates["ai_concurrency"] = *request.AIConcurrency
	}
	if request.CooldownSeconds != nil && *request.CooldownSeconds >= 0 {
		updates["ai_request_cooldown_seconds"] = *request.CooldownSeconds
	}
	if request.ScoreBudget != nil {
		if *request.ScoreBudget < 0 {
			jsonError(c, http.StatusBadRequest, "作品评分积分不能小于 0")
			return
		}
		updates["score_budget"] = *request.ScoreBudget
	}
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"class": class})
		return
	}
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		if request.ScoreBudget != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&class, class.ID).Error; err != nil {
				return err
			}
			if *request.ScoreBudget > 0 {
				maximum, err := maxClassScoreSpent(tx, class.ID)
				if err != nil {
					return err
				}
				if maximum > *request.ScoreBudget {
					return &scoreInputError{status: http.StatusBadRequest, message: fmt.Sprintf("已有学生使用了 %d 分，新的额度不能低于该数值", maximum)}
				}
			}
		}
		return tx.Model(&class).Updates(updates).Error
	})
	if err != nil {
		var inputErr *scoreInputError
		if errors.As(err, &inputErr) {
			jsonError(c, inputErr.status, inputErr.message)
			return
		}
		jsonError(c, http.StatusConflict, "班级设置保存失败")
		return
	}
	auth := authFrom(c)
	a.audit(&auth.User, &class.ID, "class", "update_settings", "success", "")
	a.DB.First(&class, class.ID)
	c.JSON(http.StatusOK, gin.H{"class": class})
}

func (a *App) handleTeacherStudents(c *gin.Context) {
	class, ok := a.teacherClass(c)
	if !ok {
		return
	}
	var students []User
	if err := a.DB.Where("class_id = ? AND role = ?", class.ID, RoleStudent).Order("name ASC").Find(&students).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "学生列表加载失败")
		return
	}
	var works []Work
	a.DB.Omit("thumbnail").Where("class_id = ?", class.ID).Find(&works)
	workByUser := make(map[uint]Work, len(works))
	for _, work := range works {
		workByUser[work.UserID] = work
	}
	studentIDs := make([]uint, 0, len(students))
	for _, student := range students {
		studentIDs = append(studentIDs, student.ID)
	}
	type usageCount struct {
		UserID uint
		Used   int64
	}
	var usageCounts []usageCount
	if len(studentIDs) > 0 {
		a.DB.Model(&AIUsageLog{}).
			Select("user_id, COUNT(*) AS used").
			Where("user_id IN ? AND status = ?", studentIDs, "success").
			Group("user_id").
			Scan(&usageCounts)
	}
	usedByUser := make(map[uint]int64, len(usageCounts))
	for _, count := range usageCounts {
		usedByUser[count.UserID] = count.Used
	}
	aiRequestLimit := class.AIRequestLimit
	if aiRequestLimit <= 0 {
		aiRequestLimit = a.Config.AIRequestsPerStudent
	}
	items := make([]gin.H, 0, len(students))
	for _, student := range students {
		aiRemaining := aiRequestLimit + student.AIExtraRequests - int(usedByUser[student.ID])
		if aiRemaining < 0 {
			aiRemaining = 0
		}
		item := gin.H{"id": student.ID, "name": student.Name, "login_name": student.LoginName, "must_change_password": student.MustChangePassword, "locked": student.Locked, "ai_blocked": student.AIBlocked, "ai_extra_requests": student.AIExtraRequests, "ai_remaining": aiRemaining, "last_login_at": student.LastLoginAt}
		if work, exists := workByUser[student.ID]; exists {
			item["work"] = gin.H{"id": work.ID, "title": work.Title, "is_published": work.IsPublished, "is_locked": work.IsLocked, "updated_at": work.UpdatedAt}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"class": class, "students": items})
}

func (a *App) handleCreateStudent(c *gin.Context) {
	class, ok := a.teacherClass(c)
	if !ok {
		return
	}
	var request struct {
		Name      string `json:"name"`
		LoginName string `json:"login_name"`
	}
	if !bindJSON(c, &request) {
		return
	}
	request.Name, request.LoginName = strings.TrimSpace(request.Name), strings.TrimSpace(request.LoginName)
	if request.LoginName == "" {
		request.LoginName = request.Name
	}
	if request.Name == "" || request.LoginName == "" {
		jsonError(c, http.StatusBadRequest, "姓名和登录名不能为空")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	student := User{ClassID: &class.ID, Role: RoleStudent, Name: request.Name, LoginName: request.LoginName, PasswordHash: string(hash), MustChangePassword: true}
	if err := a.DB.Create(&student).Error; err != nil {
		jsonError(c, http.StatusConflict, "登录名已存在")
		return
	}
	auth := authFrom(c)
	a.audit(&auth.User, &class.ID, "student", "create", "success", student.LoginName)
	c.JSON(http.StatusCreated, gin.H{"student": gin.H{"id": student.ID, "name": student.Name, "login_name": student.LoginName}, "initial_password": "123456"})
}

func (a *App) handleResetStudentPassword(c *gin.Context) {
	studentID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	var student User
	if err := a.DB.First(&student, studentID).Error; err != nil || student.Role != RoleStudent || student.ClassID == nil || !a.canManageClass(auth.User.ID, *student.ClassID) {
		jsonError(c, http.StatusNotFound, "学生不存在")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	if err := a.DB.Model(&student).Updates(map[string]any{"password_hash": string(hash), "must_change_password": true, "locked": false}).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "密码重置失败")
		return
	}
	a.audit(&auth.User, student.ClassID, "student", "reset_password", "success", student.LoginName)
	c.JSON(http.StatusOK, gin.H{"ok": true, "initial_password": "123456", "message": "已重置为 123456，学生下次登录必须改密"})
}

func (a *App) handleToggleStudent(c *gin.Context) {
	studentID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	var student User
	if err := a.DB.First(&student, studentID).Error; err != nil || student.Role != RoleStudent || student.ClassID == nil || !a.canManageClass(auth.User.ID, *student.ClassID) {
		jsonError(c, http.StatusNotFound, "学生不存在")
		return
	}
	var request struct {
		Locked        *bool `json:"locked"`
		AIBlocked     *bool `json:"ai_blocked"`
		AddAIRequests *int  `json:"add_ai_requests"`
	}
	if !bindJSON(c, &request) {
		return
	}
	updates := map[string]any{}
	if request.Locked != nil {
		updates["locked"] = *request.Locked
	}
	if request.AIBlocked != nil {
		updates["ai_blocked"] = *request.AIBlocked
	}
	if request.AddAIRequests != nil {
		updates["ai_extra_requests"] = gorm.Expr("ai_extra_requests + ?", *request.AddAIRequests)
	}
	if err := a.DB.Model(&student).Updates(updates).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "学生状态保存失败")
		return
	}
	a.audit(&auth.User, student.ClassID, "student", "update_status", "success", student.LoginName)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) handleImportStudents(c *gin.Context) {
	class, ok := a.teacherClass(c)
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		jsonError(c, http.StatusBadRequest, "请选择 Excel 文件")
		return
	}
	upload, err := file.Open()
	if err != nil {
		jsonError(c, http.StatusBadRequest, "文件读取失败")
		return
	}
	defer upload.Close()
	book, err := excelize.OpenReader(upload)
	if err != nil {
		jsonError(c, http.StatusBadRequest, "只能导入有效的 Excel 文件")
		return
	}
	defer book.Close()
	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		jsonError(c, http.StatusBadRequest, "Excel 没有工作表")
		return
	}
	rows, err := book.GetRows(sheets[0])
	if err != nil || len(rows) < 2 {
		jsonError(c, http.StatusBadRequest, "Excel 至少需要一行表头和一行学生")
		return
	}
	headers := make(map[string]int)
	for index, header := range rows[0] {
		headers[strings.ToLower(strings.TrimSpace(header))] = index
	}
	findColumn := func(names ...string) int {
		for _, name := range names {
			if index, exists := headers[strings.ToLower(name)]; exists {
				return index
			}
		}
		return -1
	}
	nameIndex := findColumn("姓名", "name", "学生姓名")
	loginIndex := findColumn("登录名", "login_name", "账号", "用户名")
	classIndex := findColumn("班级", "class", "班级名称")
	if nameIndex < 0 {
		jsonError(c, http.StatusBadRequest, "缺少姓名列")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	created, skipped := 0, make([]string, 0)
	for rowIndex, row := range rows[1:] {
		get := func(index int) string {
			if index >= 0 && index < len(row) {
				return strings.TrimSpace(row[index])
			}
			return ""
		}
		name, loginName, className := get(nameIndex), get(loginIndex), get(classIndex)
		if name == "" {
			continue
		}
		if className != "" && className != class.Name {
			skipped = append(skipped, fmt.Sprintf("第 %d 行班级不匹配", rowIndex+2))
			continue
		}
		if loginName == "" {
			loginName = name
		}
		student := User{ClassID: &class.ID, Role: RoleStudent, Name: name, LoginName: loginName, PasswordHash: string(hash), MustChangePassword: true}
		if err := a.DB.Create(&student).Error; err != nil {
			skipped = append(skipped, fmt.Sprintf("第 %d 行登录名重复", rowIndex+2))
			continue
		}
		created++
	}
	auth := authFrom(c)
	a.audit(&auth.User, &class.ID, "student", "import", "success", fmt.Sprintf("created=%d skipped=%d", created, len(skipped)))
	c.JSON(http.StatusOK, gin.H{"created": created, "skipped": skipped, "initial_password": "123456"})
}

func (a *App) handleStudentTemplate(c *gin.Context) {
	book := excelize.NewFile()
	sheet := book.GetSheetName(0)
	_ = book.SetSheetRow(sheet, "A1", &[]any{"班级", "姓名", "登录名"})
	_ = book.SetSheetRow(sheet, "A2", &[]any{"七年级1班", "示例同学", "example"})
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="class-code-lab-students.xlsx"`)
	_ = book.Write(c.Writer)
	book.Close()
}

func (a *App) handleTeacherWorks(c *gin.Context) {
	class, ok := a.teacherClass(c)
	if !ok {
		return
	}
	var works []Work
	if err := a.DB.Omit("thumbnail").Preload("User").Where("class_id = ?", class.ID).Order("updated_at DESC").Find(&works).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "作品列表加载失败")
		return
	}
	workIDs := make([]uint, 0, len(works))
	for _, work := range works {
		workIDs = append(workIDs, work.ID)
	}
	stats, err := a.loadWorkScoreStats(workIDs, 0)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "作品评分加载失败")
		return
	}
	rankable := make([]rankableWork, 0, len(works))
	for _, work := range works {
		if work.IsPublished && !work.IsLocked {
			stat := stats[work.ID]
			rankable = append(rankable, rankableWork{ID: work.ID, ScoreTotal: stat.ScoreTotal, SupporterCount: stat.SupporterCount, PublishedAt: work.PublishedAt})
		}
	}
	ranks := buildScoreRanks(rankable)
	type teacherWorkItem struct {
		ID                uint       `json:"id"`
		Title             string     `json:"title"`
		Description       string     `json:"description"`
		StudentID         uint       `json:"student_id"`
		Author            string     `json:"author"`
		IsPublished       bool       `json:"is_published"`
		IsFeatured        bool       `json:"is_featured"`
		IsLocked          bool       `json:"is_locked"`
		PublishedVersion  int        `json:"published_version"`
		PublishedAt       *time.Time `json:"published_at"`
		UpdatedAt         time.Time  `json:"updated_at"`
		UnpublishedReason string     `json:"unpublished_reason"`
		ThumbnailURL      string     `json:"thumbnail_url,omitempty"`
		ScoreTotal        int        `json:"score_total"`
		SupporterCount    int        `json:"supporter_count"`
		Rank              int        `json:"rank,omitempty"`
	}
	items := make([]teacherWorkItem, 0, len(works))
	for _, work := range works {
		stat := stats[work.ID]
		thumbnailURL := ""
		if work.ThumbnailVersion > 0 && work.ThumbnailVersion == work.PublishedVersion {
			thumbnailURL = fmt.Sprintf("/api/teacher/works/%d/thumbnail?v=%d", work.ID, work.ThumbnailVersion)
		}
		items = append(items, teacherWorkItem{
			ID: work.ID, Title: work.Title, Description: work.Description, StudentID: work.UserID,
			Author: work.User.Name, IsPublished: work.IsPublished, IsFeatured: work.IsFeatured,
			IsLocked: work.IsLocked, PublishedVersion: work.PublishedVersion, PublishedAt: work.PublishedAt,
			UpdatedAt: work.UpdatedAt, UnpublishedReason: work.UnpublishedReason,
			ThumbnailURL: thumbnailURL, ScoreTotal: stat.ScoreTotal, SupporterCount: stat.SupporterCount, Rank: ranks[work.ID],
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.Rank > 0 || right.Rank > 0 {
			if left.Rank == 0 {
				return false
			}
			if right.Rank == 0 {
				return true
			}
			return left.Rank < right.Rank
		}
		if left.IsPublished != right.IsPublished {
			return left.IsPublished
		}
		if left.IsPublished && left.PublishedAt != nil && right.PublishedAt != nil && !left.PublishedAt.Equal(*right.PublishedAt) {
			return left.PublishedAt.After(*right.PublishedAt)
		}
		return left.UpdatedAt.After(right.UpdatedAt)
	})
	c.JSON(http.StatusOK, gin.H{"class": class, "works": items})
}

func (a *App) handleTeacherWorkThumbnail(c *gin.Context) {
	workID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	var work Work
	if err := a.DB.Select("id", "class_id", "published_version", "thumbnail", "thumbnail_media_type", "thumbnail_version").First(&work, workID).Error; err != nil {
		jsonError(c, http.StatusNotFound, "作品封面不可用")
		return
	}
	if !a.canManageClass(auth.User.ID, work.ClassID) {
		jsonError(c, http.StatusForbidden, "你没有管理这个班级的权限")
		return
	}
	if len(work.Thumbnail) == 0 || work.ThumbnailVersion != work.PublishedVersion || (work.ThumbnailMediaType != "image/jpeg" && work.ThumbnailMediaType != "image/png") {
		jsonError(c, http.StatusNotFound, "作品封面尚未生成")
		return
	}
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, work.ThumbnailMediaType, work.Thumbnail)
}

func (a *App) handleTeacherWorkAction(c *gin.Context) {
	workID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	var work Work
	if err := a.DB.Omit("thumbnail").First(&work, workID).Error; err != nil || !a.canManageClass(auth.User.ID, work.ClassID) {
		jsonError(c, http.StatusNotFound, "作品不存在")
		return
	}
	var request struct {
		Note string `json:"note"`
	}
	if c.Request.ContentLength != 0 {
		_ = c.ShouldBindJSON(&request)
	}
	action := c.Param("action")
	if action == "run-token" {
		a.createRunTokenResponse(c, &work.ID, work.ClassID, work.DraftCode)
		return
	}
	updates := map[string]any{}
	switch action {
	case "unpublish":
		updates["is_published"], updates["is_featured"] = false, false
		updates["unpublished_reason"] = strings.TrimSpace(request.Note)
	case "restore":
		updates["is_published"], updates["unpublished_reason"] = true, ""
	case "lock":
		updates["is_locked"] = true
	case "unlock":
		updates["is_locked"] = false
	case "feature":
		if !work.IsPublished {
			jsonError(c, http.StatusBadRequest, "只有已发布作品可以加入精选")
			return
		}
		updates["is_featured"] = true
	case "unfeature":
		updates["is_featured"] = false
	default:
		jsonError(c, http.StatusBadRequest, "不支持的作品操作")
		return
	}
	if err := a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&work).Updates(updates).Error; err != nil {
			return err
		}
		if action == "unpublish" || action == "lock" {
			return tx.Where("work_id = ?", work.ID).Delete(&WorkScore{}).Error
		}
		return nil
	}); err != nil {
		jsonError(c, http.StatusInternalServerError, "作品操作失败")
		return
	}
	a.audit(&auth.User, &work.ClassID, "work", action, "success", request.Note)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) handleTeacherAIUsage(c *gin.Context) {
	auth := authFrom(c)
	var classIDs []uint
	a.DB.Model(&TeacherClassAccess{}).Where("teacher_id = ?", auth.User.ID).Pluck("class_id", &classIDs)
	var logs []AIUsageLog
	a.DB.Model(&AIUsageLog{}).
		Select("ai_usage_logs.*, classes.name AS class_name, users.name AS student_name").
		Joins("LEFT JOIN classes ON classes.id = ai_usage_logs.class_id").
		Joins("LEFT JOIN users ON users.id = ai_usage_logs.user_id").
		Where("ai_usage_logs.class_id IN ?", classIDs).
		Order("ai_usage_logs.id DESC").Limit(100).Find(&logs)
	var total, success, failed int64
	a.DB.Model(&AIUsageLog{}).Where("class_id IN ?", classIDs).Count(&total)
	a.DB.Model(&AIUsageLog{}).Where("class_id IN ? AND status = ?", classIDs, "success").Count(&success)
	a.DB.Model(&AIUsageLog{}).Where("class_id IN ? AND status = ?", classIDs, "failed").Count(&failed)
	var tokenTotals struct {
		PromptTokens          int64
		CompletionTokens      int64
		PromptCacheHitTokens  int64
		PromptCacheMissTokens int64
	}
	a.DB.Model(&AIUsageLog{}).Where("class_id IN ?", classIDs).Select(
		"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, " +
			"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, " +
			"COALESCE(SUM(prompt_cache_hit_tokens), 0) AS prompt_cache_hit_tokens, " +
			"COALESCE(SUM(prompt_cache_miss_tokens), 0) AS prompt_cache_miss_tokens",
	).Scan(&tokenTotals)
	var cacheHitRate any
	cacheTotal := tokenTotals.PromptCacheHitTokens + tokenTotals.PromptCacheMissTokens
	if cacheTotal > 0 {
		cacheHitRate = float64(tokenTotals.PromptCacheHitTokens) * 100 / float64(cacheTotal)
	}
	c.JSON(http.StatusOK, gin.H{"summary": gin.H{
		"total": total, "success": success, "failed": failed, "configured": a.AIClient.Configured(), "model": a.Config.AIModel,
		"prompt_tokens": tokenTotals.PromptTokens, "completion_tokens": tokenTotals.CompletionTokens,
		"prompt_cache_hit_tokens": tokenTotals.PromptCacheHitTokens, "prompt_cache_miss_tokens": tokenTotals.PromptCacheMissTokens,
		"cache_hit_rate": cacheHitRate,
	}, "logs": logs})
}

func (a *App) handleTeacherConversations(c *gin.Context) {
	auth := authFrom(c)
	var classIDs []uint
	a.DB.Model(&TeacherClassAccess{}).Where("teacher_id = ?", auth.User.ID).Pluck("class_id", &classIDs)
	var conversations []AIConversation
	if err := a.DB.Model(&AIConversation{}).
		Select("ai_conversations.*, classes.name AS class_name, users.name AS student_name").
		Joins("LEFT JOIN classes ON classes.id = ai_conversations.class_id").
		Joins("LEFT JOIN users ON users.id = ai_conversations.user_id").
		Preload("Messages").Where("ai_conversations.class_id IN ?", classIDs).
		Order("ai_conversations.updated_at DESC").Limit(50).Find(&conversations).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "对话加载失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversations": conversations})
}

func (a *App) handleTeacherHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"configured": a.AIClient.Configured(), "base_url": a.Config.AIBaseURL, "model": a.Config.AIModel,
		"api_key_present": a.Config.AIAPIKey != "", "reasoning_effort": a.Config.AIReasoningEffort,
		"max_output_tokens": a.Config.AIMaxOutputTokens, "modification_max_tokens": a.Config.AIModificationTokens,
		"history_messages": a.Config.AIHistoryMessages,
		"history_chars":    a.Config.AIHistoryChars,
	})
}

func (a *App) handleTeacherAudit(c *gin.Context) {
	auth := authFrom(c)
	var classIDs []uint
	a.DB.Model(&TeacherClassAccess{}).Where("teacher_id = ?", auth.User.ID).Pluck("class_id", &classIDs)
	var logs []AuditLog
	if err := a.DB.Where("class_id IN ? OR actor_id = ?", classIDs, auth.User.ID).Order("id DESC").Limit(200).Find(&logs).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "操作记录加载失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

func (a *App) handleTeacherTestAI(c *gin.Context) {
	if !a.AIClient.Configured() {
		jsonError(c, http.StatusServiceUnavailable, "模型服务未配置")
		return
	}
	started := time.Now()
	_, err := a.AIClient.StreamWithOptions(
		c.Request.Context(),
		[]ChatMessage{{Role: "system", Content: "请只回复：连接正常"}, {Role: "user", Content: "连接测试"}},
		AIRequestOptions{MaxOutputTokens: 16, ReasoningEffort: "none"},
		func(string) error { return nil },
	)
	if err != nil {
		jsonError(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "duration_ms": time.Since(started).Milliseconds()})
}

func (a *App) handleTeacherStudentAI(c *gin.Context) {
	studentID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	// This small endpoint is kept separate so the teacher UI can update one row without reloading the full table.
	auth := authFrom(c)
	var student User
	if err := a.DB.First(&student, studentID).Error; err != nil || student.ClassID == nil || !a.canManageClass(auth.User.ID, *student.ClassID) {
		jsonError(c, http.StatusNotFound, "学生不存在")
		return
	}
	value, err := strconv.Atoi(c.Query("extra"))
	if err != nil {
		value = 1
	}
	if err := a.DB.Model(&student).Update("ai_extra_requests", gorm.Expr("ai_extra_requests + ?", value)).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "额度更新失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
