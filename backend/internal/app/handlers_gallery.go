package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type galleryItem struct {
	ID               uint       `json:"id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Author           string     `json:"author"`
	ClassName        string     `json:"class_name"`
	ClassID          uint       `json:"class_id"`
	PublishedVersion int        `json:"published_version"`
	ThumbnailURL     string     `json:"thumbnail_url,omitempty"`
	PublishedAt      *time.Time `json:"published_at"`
	IsFeatured       bool       `json:"is_featured"`
	IsMine           bool       `json:"is_mine"`
	ScoreTotal       int        `json:"score_total"`
	SupporterCount   int        `json:"supporter_count"`
	MyScore          int        `json:"my_score"`
	Rank             int        `json:"rank,omitempty"`
}

type scoringSummary struct {
	Budget    int  `json:"budget"`
	Spent     int  `json:"spent"`
	Remaining int  `json:"remaining"`
	Enabled   bool `json:"enabled"`
	Completed bool `json:"completed"`
}

type workScoreStats struct {
	ScoreTotal     int
	SupporterCount int
	MyScore        int
}

type rankableWork struct {
	ID             uint
	ScoreTotal     int
	SupporterCount int
	PublishedAt    *time.Time
}

type scoreInputError struct {
	status  int
	message string
}

func (e *scoreInputError) Error() string { return e.message }

func (a *App) loadWorkScoreStats(workIDs []uint, voterID uint) (map[uint]workScoreStats, error) {
	stats := make(map[uint]workScoreStats, len(workIDs))
	if len(workIDs) == 0 {
		return stats, nil
	}
	type aggregateRow struct {
		WorkID         uint
		ScoreTotal     int
		SupporterCount int
	}
	var rows []aggregateRow
	if err := a.DB.Model(&WorkScore{}).
		Select("work_id, COALESCE(SUM(points), 0) AS score_total, COUNT(*) AS supporter_count").
		Where("work_id IN ? AND points > 0", workIDs).
		Group("work_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		stats[row.WorkID] = workScoreStats{ScoreTotal: row.ScoreTotal, SupporterCount: row.SupporterCount}
	}
	if voterID == 0 {
		return stats, nil
	}
	var ownScores []WorkScore
	if err := a.DB.Select("work_id", "points").Where("voter_id = ? AND work_id IN ?", voterID, workIDs).Find(&ownScores).Error; err != nil {
		return nil, err
	}
	for _, score := range ownScores {
		item := stats[score.WorkID]
		item.MyScore = score.Points
		stats[score.WorkID] = item
	}
	return stats, nil
}

func buildScoreRanks(works []rankableWork) map[uint]int {
	ranked := make([]rankableWork, 0, len(works))
	for _, work := range works {
		if work.ScoreTotal > 0 {
			ranked = append(ranked, work)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		left, right := ranked[i], ranked[j]
		if left.ScoreTotal != right.ScoreTotal {
			return left.ScoreTotal > right.ScoreTotal
		}
		if left.SupporterCount != right.SupporterCount {
			return left.SupporterCount > right.SupporterCount
		}
		if left.PublishedAt != nil && right.PublishedAt != nil && !left.PublishedAt.Equal(*right.PublishedAt) {
			return left.PublishedAt.Before(*right.PublishedAt)
		}
		if left.PublishedAt == nil && right.PublishedAt != nil {
			return false
		}
		if left.PublishedAt != nil && right.PublishedAt == nil {
			return true
		}
		return left.ID < right.ID
	})
	ranks := make(map[uint]int, len(ranked))
	for index, work := range ranked {
		ranks[work.ID] = index + 1
	}
	return ranks
}

func (a *App) scoringForStudent(classID, voterID uint) (scoringSummary, error) {
	var class Class
	if err := a.DB.First(&class, classID).Error; err != nil {
		return scoringSummary{}, err
	}
	var spent int
	if err := a.DB.Model(&WorkScore{}).Where("class_id = ? AND voter_id = ?", classID, voterID).
		Select("COALESCE(SUM(points), 0)").Scan(&spent).Error; err != nil {
		return scoringSummary{}, err
	}
	summary := scoringSummary{Budget: class.ScoreBudget, Spent: spent, Enabled: class.ScoreBudget > 0}
	if summary.Enabled {
		summary.Remaining = class.ScoreBudget - spent
		if summary.Remaining < 0 {
			summary.Remaining = 0
		}
		summary.Completed = summary.Remaining == 0
	}
	return summary, nil
}

func maxClassScoreSpent(db *gorm.DB, classID uint) (int, error) {
	var rows []struct {
		VoterID uint
		Spent   int
	}
	if err := db.Model(&WorkScore{}).Select("voter_id, SUM(points) AS spent").
		Where("class_id = ?", classID).Group("voter_id").Scan(&rows).Error; err != nil {
		return 0, err
	}
	maximum := 0
	for _, row := range rows {
		if row.Spent > maximum {
			maximum = row.Spent
		}
	}
	return maximum, nil
}

func (a *App) handleGallery(c *gin.Context) {
	auth := authFrom(c)
	if auth.User.ClassID == nil {
		jsonError(c, http.StatusBadRequest, "当前账号没有班级")
		return
	}
	items, err := a.listGallery(*auth.User.ClassID, false, auth.User.ID)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "作品广场加载失败")
		return
	}
	scoring, err := a.scoringForStudent(*auth.User.ClassID, auth.User.ID)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "评分信息加载失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"works": items, "scoring": scoring})
}

func (a *App) handleFeatured(c *gin.Context) {
	auth := authFrom(c)
	items, err := a.listGallery(0, true, auth.User.ID)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "精选作品加载失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"works": items})
}

func (a *App) listGallery(classID uint, featured bool, currentUserID uint) ([]galleryItem, error) {
	query := a.DB.Model(&Work{}).
		Select("works.id, works.title, works.description, works.class_id, works.published_version, works.thumbnail_version, works.published_at, works.is_featured, users.name AS author, classes.name AS class_name, works.user_id").
		Joins("JOIN users ON users.id = works.user_id").
		Joins("JOIN classes ON classes.id = works.class_id").
		Where("works.is_published = ? AND works.is_locked = ?", true, false)
	if featured {
		query = query.Where("works.is_featured = ?", true)
	} else {
		query = query.Where("works.class_id = ?", classID)
	}
	type row struct {
		ID               uint
		Title            string
		Description      string
		Author           string
		ClassName        string
		ClassID          uint
		PublishedVersion int
		ThumbnailVersion int
		PublishedAt      *time.Time
		IsFeatured       bool
		UserID           uint
	}
	var rows []row
	if err := query.Order("works.published_at DESC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	workIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		workIDs = append(workIDs, row.ID)
	}
	stats := make(map[uint]workScoreStats, len(workIDs))
	if !featured {
		var err error
		stats, err = a.loadWorkScoreStats(workIDs, currentUserID)
		if err != nil {
			return nil, err
		}
	}
	items := make([]galleryItem, 0, len(rows))
	rankable := make([]rankableWork, 0, len(rows))
	for _, row := range rows {
		stat := stats[row.ID]
		thumbnailURL := ""
		if row.ThumbnailVersion > 0 && row.ThumbnailVersion == row.PublishedVersion {
			thumbnailURL = fmt.Sprintf("/api/gallery/%d/thumbnail?v=%d", row.ID, row.ThumbnailVersion)
		}
		items = append(items, galleryItem{
			ID: row.ID, Title: row.Title, Description: row.Description, Author: row.Author,
			ClassName: row.ClassName, ClassID: row.ClassID, PublishedVersion: row.PublishedVersion, ThumbnailURL: thumbnailURL,
			PublishedAt: row.PublishedAt, IsFeatured: row.IsFeatured, IsMine: row.UserID == currentUserID,
			ScoreTotal: stat.ScoreTotal, SupporterCount: stat.SupporterCount, MyScore: stat.MyScore,
		})
		if !featured {
			rankable = append(rankable, rankableWork{ID: row.ID, ScoreTotal: stat.ScoreTotal, SupporterCount: stat.SupporterCount, PublishedAt: row.PublishedAt})
		}
	}
	if featured {
		return items, nil
	}
	ranks := buildScoreRanks(rankable)
	for index := range items {
		items[index].Rank = ranks[items[index].ID]
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
		if left.PublishedAt != nil && right.PublishedAt != nil && !left.PublishedAt.Equal(*right.PublishedAt) {
			return left.PublishedAt.After(*right.PublishedAt)
		}
		return left.ID > right.ID
	})
	return items, nil
}

func (a *App) handleGalleryThumbnail(c *gin.Context) {
	workID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	var work Work
	if err := a.DB.Select("id", "class_id", "is_published", "is_featured", "is_locked", "published_version", "thumbnail", "thumbnail_media_type", "thumbnail_version").First(&work, workID).Error; err != nil || !work.IsPublished || work.IsLocked {
		jsonError(c, http.StatusNotFound, "作品封面不可用")
		return
	}
	sameClass := auth.User.ClassID != nil && *auth.User.ClassID == work.ClassID
	if !sameClass && !work.IsFeatured {
		jsonError(c, http.StatusForbidden, "不能访问其他班级的普通作品")
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

func (a *App) handleGalleryWork(c *gin.Context) {
	workID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	var work Work
	if err := a.DB.Omit("thumbnail").Preload("User").Preload("Class").First(&work, workID).Error; err != nil || !work.IsPublished || work.IsLocked {
		jsonError(c, http.StatusNotFound, "作品当前不可用")
		return
	}
	sameClass := auth.User.ClassID != nil && *auth.User.ClassID == work.ClassID
	if !sameClass && !work.IsFeatured {
		jsonError(c, http.StatusForbidden, "不能访问其他班级的普通作品")
		return
	}
	if sameClass {
		items, listErr := a.listGallery(work.ClassID, false, auth.User.ID)
		if listErr != nil {
			jsonError(c, http.StatusInternalServerError, "作品信息加载失败")
			return
		}
		for _, item := range items {
			if item.ID == work.ID {
				scoring, scoreErr := a.scoringForStudent(work.ClassID, auth.User.ID)
				if scoreErr != nil {
					jsonError(c, http.StatusInternalServerError, "评分信息加载失败")
					return
				}
				c.JSON(http.StatusOK, gin.H{"work": item, "scoring": scoring})
				return
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"work": gin.H{
		"id": work.ID, "title": work.Title, "description": work.Description,
		"author": work.User.Name, "class_name": work.Class.Name, "class_id": work.ClassID,
		"published_at": work.PublishedAt, "published_version": work.PublishedVersion,
		"is_featured": work.IsFeatured,
	}})
}

func (a *App) handleScoreWork(c *gin.Context) {
	workID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var request struct {
		Points *int `json:"points"`
	}
	if !bindJSON(c, &request) {
		return
	}
	if request.Points == nil || *request.Points <= 0 {
		jsonError(c, http.StatusBadRequest, "提交后的评分不能撤回，分值必须是正整数")
		return
	}
	auth := authFrom(c)
	if auth.User.ClassID == nil {
		jsonError(c, http.StatusBadRequest, "当前账号没有班级")
		return
	}
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		var class Class
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&class, *auth.User.ClassID).Error; err != nil {
			return err
		}
		if class.ScoreBudget <= 0 {
			return &scoreInputError{status: http.StatusForbidden, message: "教师暂未开放作品评分"}
		}
		var voter User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&voter, auth.User.ID).Error; err != nil {
			return err
		}
		if voter.ClassID == nil || *voter.ClassID != class.ID {
			return &scoreInputError{status: http.StatusForbidden, message: "当前账号不能参与这个班级的评分"}
		}
		var work Work
		if err := tx.Omit("thumbnail").Clauses(clause.Locking{Strength: "UPDATE"}).First(&work, workID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &scoreInputError{status: http.StatusNotFound, message: "作品不存在"}
			}
			return err
		}
		if work.ClassID != class.ID {
			return &scoreInputError{status: http.StatusForbidden, message: "只能给本班作品评分"}
		}
		if work.UserID == voter.ID {
			return &scoreInputError{status: http.StatusBadRequest, message: "不能给自己的作品评分"}
		}
		if !work.IsPublished || work.IsLocked {
			return &scoreInputError{status: http.StatusBadRequest, message: "只能给正在展示的作品评分"}
		}
		var otherSpent int
		if err := tx.Model(&WorkScore{}).
			Where("class_id = ? AND voter_id = ? AND work_id <> ?", class.ID, voter.ID, work.ID).
			Select("COALESCE(SUM(points), 0)").Scan(&otherSpent).Error; err != nil {
			return err
		}
		if otherSpent+*request.Points > class.ScoreBudget {
			return &scoreInputError{status: http.StatusBadRequest, message: "评分积分不足，请先减少其他作品的积分"}
		}
		var score WorkScore
		findErr := tx.Where("voter_id = ? AND work_id = ?", voter.ID, work.ID).First(&score).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return tx.Create(&WorkScore{ClassID: class.ID, VoterID: voter.ID, WorkID: work.ID, Points: *request.Points}).Error
		}
		if findErr != nil {
			return findErr
		}
		return tx.Model(&score).Update("points", *request.Points).Error
	})
	if err != nil {
		var inputErr *scoreInputError
		if errors.As(err, &inputErr) {
			jsonError(c, inputErr.status, inputErr.message)
			return
		}
		jsonError(c, http.StatusInternalServerError, "评分保存失败")
		return
	}
	items, err := a.listGallery(*auth.User.ClassID, false, auth.User.ID)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "排行榜刷新失败")
		return
	}
	scoring, err := a.scoringForStudent(*auth.User.ClassID, auth.User.ID)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "评分信息刷新失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"works": items, "scoring": scoring})
}

func (a *App) handleGalleryRunToken(c *gin.Context) {
	workID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	var work Work
	if err := a.DB.Omit("thumbnail").First(&work, workID).Error; err != nil || !work.IsPublished || work.IsLocked {
		jsonError(c, http.StatusNotFound, "作品当前不可用")
		return
	}
	sameClass := auth.User.ClassID != nil && *auth.User.ClassID == work.ClassID
	if !sameClass && !work.IsFeatured {
		jsonError(c, http.StatusForbidden, "不能运行其他班级的普通作品")
		return
	}
	a.createRunTokenResponse(c, &work.ID, work.ClassID, work.PublishedCode)
}

func (a *App) RunnerRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/runner-assets/html2canvas.min.js", a.handleRunnerCaptureLibrary)
	router.GET("/run/:token", a.handleRun)
	return router
}

func (a *App) handleRunnerCaptureLibrary(c *gin.Context) {
	path := filepath.Join(a.Config.FrontendDist, "runner-assets", "html2canvas.min.js")
	if a.Config.FrontendDist == "" {
		c.Status(http.StatusNotFound)
		return
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(path)
}

func (a *App) handleRun(c *gin.Context) {
	var token RunToken
	if err := a.DB.Where("token_hash = ? AND expires_at > ?", hashToken(c.Param("token")), timeNow()).First(&token).Error; err != nil {
		c.Header("Cache-Control", "no-store")
		c.String(http.StatusUnauthorized, "运行链接无效或已过期")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), clipboard-read=(), clipboard-write=(), payment=(), usb=()")
	c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; style-src 'unsafe-inline' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; img-src data: blob: https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; media-src data: blob: https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; font-src data: https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; connect-src 'none'; frame-src 'none'; child-src 'none'; object-src 'none'; worker-src 'none'; base-uri 'none'; form-action 'none'; navigate-to 'none'")
	c.String(http.StatusOK, injectRunnerBridge(token.Code, c.Query("capture")))
}

var timeNow = func() time.Time { return time.Now() }
