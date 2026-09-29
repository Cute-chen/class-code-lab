package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	return New(Config{RunTokenTTL: 5 * time.Minute, AIMaxConcurrency: 2, AIRequestsPerStudent: 12}, db)
}

func testContext(method, path string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestGalleryPreventsCrossClassAccessExceptFeatured(t *testing.T) {
	a := newTestApp(t)
	classA := Class{Name: "A班", Active: true, LoginOpen: true}
	classB := Class{Name: "B班", Active: true, LoginOpen: true}
	a.DB.Create(&classA)
	a.DB.Create(&classB)
	userA := User{ClassID: &classA.ID, Class: &classA, Role: RoleStudent, Name: "甲", LoginName: "a", PasswordHash: "x"}
	userB := User{ClassID: &classB.ID, Class: &classB, Role: RoleStudent, Name: "乙", LoginName: "b", PasswordHash: "x"}
	a.DB.Create(&userA)
	a.DB.Create(&userB)
	work := Work{UserID: userB.ID, ClassID: classB.ID, Title: "B班作品", DraftCode: "<html></html>", PublishedCode: "<html></html>", IsPublished: true}
	a.DB.Create(&work)

	c, recorder := testContext(http.MethodGet, fmt.Sprintf("/api/gallery/%d", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}}
	c.Set("auth", authContext{User: userA})
	a.handleGalleryWork(c)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	a.DB.Model(&work).Update("is_featured", true)
	c, recorder = testContext(http.MethodGet, fmt.Sprintf("/api/gallery/%d", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}}
	c.Set("auth", authContext{User: userA})
	a.handleGalleryWork(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected featured work to be readable, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPublishCopiesImmutableSnapshot(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "测试班", Active: true, LoginOpen: true, PublishEnabled: true}
	a.DB.Create(&class)
	user := User{ClassID: &class.ID, Class: &class, Role: RoleStudent, Name: "同学", LoginName: "student", PasswordHash: "x"}
	a.DB.Create(&user)
	original := `<!doctype html><html><body><script>function start(){ document.body.textContent='v1' }</script></body></html>`
	work := Work{UserID: user.ID, ClassID: class.ID, Title: "快照测试", DraftCode: original, Thumbnail: []byte("old"), ThumbnailMediaType: "image/png", ThumbnailVersion: 1}
	a.DB.Create(&work)
	c, recorder := testContext(http.MethodPost, "/api/student/work/publish", "")
	c.Set("auth", authContext{User: user})
	a.handlePublishWork(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish failed: %d %s", recorder.Code, recorder.Body.String())
	}
	a.DB.First(&work, work.ID)
	if work.PublishedCode != original || !work.IsPublished || work.PublishedVersion != 1 || len(work.Thumbnail) != 0 || work.ThumbnailVersion != 0 {
		t.Fatalf("unexpected published work: %#v", work)
	}
	var publishResponse struct {
		CaptureToken string `json:"capture_token"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &publishResponse); err != nil || publishResponse.CaptureToken == "" {
		t.Fatalf("publish should return a thumbnail capture token: err=%v body=%s", err, recorder.Body.String())
	}
	a.DB.Model(&work).Update("draft_code", "<html><body>v2</body></html>")
	a.DB.First(&work, work.ID)
	if work.PublishedCode != original {
		t.Fatal("draft edit changed the published snapshot")
	}
}

func TestClearDraftForNewConversationKeepsRecoverableRevision(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "清空测试班", Active: true}
	a.DB.Create(&class)
	user := User{ClassID: &class.ID, Class: &class, Role: RoleStudent, Name: "同学", LoginName: "clear-draft", PasswordHash: "x"}
	a.DB.Create(&user)
	original := `<!doctype html><html><body>旧作品</body></html>`
	work := Work{UserID: user.ID, ClassID: class.ID, Title: "旧作品", DraftCode: original}
	a.DB.Create(&work)

	c, recorder := testContext(http.MethodPut, "/api/student/work", `{"title":"旧作品","description":"","code":"","source":"new-conversation-clear"}`)
	c.Set("auth", authContext{User: user})
	a.handleSaveStudentWork(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("clear draft failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	a.DB.First(&work, work.ID)
	if work.DraftCode != "" || !work.DraftCleared {
		t.Fatalf("draft was not marked cleared: %#v", work)
	}
	var revision WorkRevision
	if err := a.DB.Where("work_id = ?", work.ID).Order("id DESC").First(&revision).Error; err != nil || revision.Code != original {
		t.Fatalf("clear did not preserve recoverable revision: revision=%#v err=%v", revision, err)
	}

	c, recorder = testContext(http.MethodPost, fmt.Sprintf("/api/student/work/restore/%d", revision.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(revision.ID)}}
	c.Set("auth", authContext{User: user})
	a.handleRestoreRevision(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("restore cleared draft failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	a.DB.First(&work, work.ID)
	if work.DraftCode != original || work.DraftCleared {
		t.Fatalf("restored draft has wrong cleared state: %#v", work)
	}
}

func TestPasswordChangeMiddlewareBlocksWorkspace(t *testing.T) {
	called := false
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth", authContext{User: User{Role: RoleStudent, MustChangePassword: true}})
	})
	router.Use(requirePasswordChanged())
	router.GET("/workspace", func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/workspace", nil))
	if recorder.Code != http.StatusForbidden || called {
		t.Fatalf("expected blocked workspace, code=%d called=%v", recorder.Code, called)
	}
}

func TestPreviewRejectsUnsafeCode(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "测试班", Active: true}
	a.DB.Create(&class)
	user := User{ClassID: &class.ID, Class: &class, Role: RoleStudent, Name: "同学", LoginName: "student", PasswordHash: "x"}
	a.DB.Create(&user)
	a.DB.Create(&Work{UserID: user.ID, ClassID: class.ID, DraftCode: `<html><script>fetch('https://example.com')</script></html>`})
	c, recorder := testContext(http.MethodPost, "/api/student/work/preview-token", "")
	c.Set("auth", authContext{User: user})
	a.handlePreviewToken(c)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "fetch") {
		t.Fatalf("expected unsafe preview rejection, code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRunnerRejectsExpiredTokenAndSetsCSP(t *testing.T) {
	a := newTestApp(t)
	expiredRaw := "expired"
	a.DB.Create(&RunToken{TokenHash: hashToken(expiredRaw), ClassID: 1, Code: "<html></html>", ExpiresAt: time.Now().Add(-time.Minute)})
	c, recorder := testContext(http.MethodGet, "/run/expired", "")
	c.Params = gin.Params{{Key: "token", Value: expiredRaw}}
	a.handleRun(c)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected expired token rejection, got %d", recorder.Code)
	}

	validRaw := "valid"
	a.DB.Create(&RunToken{TokenHash: hashToken(validRaw), ClassID: 1, Code: "<html><head></head><body>ok</body></html>", ExpiresAt: time.Now().Add(time.Minute)})
	c, recorder = testContext(http.MethodGet, "/run/valid", "")
	c.Params = gin.Params{{Key: "token", Value: validRaw}}
	a.handleRun(c)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "connect-src 'none'") || !strings.Contains(recorder.Body.String(), "class-code-lab-runner") {
		t.Fatalf("runner response missing security controls: code=%d csp=%q", recorder.Code, recorder.Header().Get("Content-Security-Policy"))
	}

	c, recorder = testContext(http.MethodGet, "/run/valid?capture=capture-1", "")
	c.Params = gin.Params{{Key: "token", Value: validRaw}}
	a.handleRun(c)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "/runner-assets/html2canvas.min.js") || !strings.Contains(recorder.Body.String(), `var captureId="capture-1"`) {
		t.Fatalf("capture runner was not injected: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRunnerServesBundledCaptureLibrary(t *testing.T) {
	a := newTestApp(t)
	dist := t.TempDir()
	assetPath := filepath.Join(dist, "runner-assets", "html2canvas.min.js")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("window.html2canvas=function(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Config.FrontendDist = dist
	recorder := httptest.NewRecorder()
	a.RunnerRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/runner-assets/html2canvas.min.js", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "window.html2canvas") || !strings.Contains(recorder.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("runner capture library response invalid: code=%d cache=%q body=%s", recorder.Code, recorder.Header().Get("Cache-Control"), recorder.Body.String())
	}
}

func testThumbnailDataURL(t *testing.T) (string, []byte) {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.Set(x, y, color.RGBA{R: uint8(30 + x*20), G: uint8(80 + y*20), B: 180, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), data
}

func TestPublishedWorkThumbnailUploadAndAccess(t *testing.T) {
	a := newTestApp(t)
	classA := Class{Name: "封面 A 班", Active: true}
	classB := Class{Name: "封面 B 班", Active: true}
	a.DB.Create(&classA)
	a.DB.Create(&classB)
	owner := User{ClassID: &classA.ID, Class: &classA, Role: RoleStudent, Name: "作者", LoginName: "thumbnail-owner", PasswordHash: "x"}
	classmate := User{ClassID: &classA.ID, Class: &classA, Role: RoleStudent, Name: "同班", LoginName: "thumbnail-classmate", PasswordHash: "x"}
	outsider := User{ClassID: &classB.ID, Class: &classB, Role: RoleStudent, Name: "外班", LoginName: "thumbnail-outsider", PasswordHash: "x"}
	teacher := User{Role: RoleTeacher, Name: "本班教师", LoginName: "thumbnail-teacher", PasswordHash: "x"}
	otherTeacher := User{Role: RoleTeacher, Name: "外班教师", LoginName: "thumbnail-other-teacher", PasswordHash: "x"}
	a.DB.Create(&owner)
	a.DB.Create(&classmate)
	a.DB.Create(&outsider)
	a.DB.Create(&teacher)
	a.DB.Create(&otherTeacher)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: classA.ID})
	now := time.Now()
	work := Work{UserID: owner.ID, ClassID: classA.ID, Title: "有封面的作品", PublishedCode: "<html></html>", PublishedVersion: 2, PublishedAt: &now, IsPublished: true}
	a.DB.Create(&work)

	dataURL, imageBytes := testThumbnailDataURL(t)
	body, _ := json.Marshal(gin.H{"image": dataURL, "version": 2})
	c, recorder := testContext(http.MethodPost, "/api/student/work/thumbnail", string(body))
	c.Set("auth", authContext{User: owner})
	a.handleSaveWorkThumbnail(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("thumbnail upload failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}

	items, err := a.listGallery(classA.ID, false, classmate.ID)
	if err != nil || len(items) != 1 || items[0].ThumbnailURL != fmt.Sprintf("/api/gallery/%d/thumbnail?v=2", work.ID) {
		t.Fatalf("gallery thumbnail URL missing: items=%#v err=%v", items, err)
	}

	c, recorder = testContext(http.MethodGet, fmt.Sprintf("/api/gallery/%d/thumbnail", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}}
	c.Set("auth", authContext{User: classmate})
	a.handleGalleryThumbnail(c)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || !bytes.Equal(recorder.Body.Bytes(), imageBytes) {
		t.Fatalf("same-class thumbnail response invalid: code=%d type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}

	c, recorder = testContext(http.MethodGet, fmt.Sprintf("/api/gallery/%d/thumbnail", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}}
	c.Set("auth", authContext{User: outsider})
	a.handleGalleryThumbnail(c)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ordinary cross-class thumbnail should be forbidden, got %d", recorder.Code)
	}

	a.DB.Model(&work).Update("is_featured", true)
	c, recorder = testContext(http.MethodGet, fmt.Sprintf("/api/gallery/%d/thumbnail", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}}
	c.Set("auth", authContext{User: outsider})
	a.handleGalleryThumbnail(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("featured thumbnail should be readable cross-class, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	c, recorder = testContext(http.MethodGet, fmt.Sprintf("/api/teacher/works/%d/thumbnail", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}}
	c.Set("auth", authContext{User: teacher})
	a.handleTeacherWorkThumbnail(c)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || !bytes.Equal(recorder.Body.Bytes(), imageBytes) {
		t.Fatalf("teacher thumbnail response invalid: code=%d type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}

	c, recorder = testContext(http.MethodGet, fmt.Sprintf("/api/teacher/works/%d/thumbnail", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}}
	c.Set("auth", authContext{User: otherTeacher})
	a.handleTeacherWorkThumbnail(c)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("teacher without class access should be forbidden, got %d", recorder.Code)
	}
}

func TestTeacherWorksIncludeThumbnailURL(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "教师封面班", Active: true}
	a.DB.Create(&class)
	teacher := User{Role: RoleTeacher, Name: "教师", LoginName: "teacher-work-thumbnail-list", PasswordHash: "x"}
	student := User{ClassID: &class.ID, Class: &class, Role: RoleStudent, Name: "学生", LoginName: "student-work-thumbnail-list", PasswordHash: "x"}
	a.DB.Create(&teacher)
	a.DB.Create(&student)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: class.ID})
	now := time.Now()
	work := Work{
		UserID: student.ID, ClassID: class.ID, Title: "有截图的作品", PublishedCode: "<html></html>",
		PublishedVersion: 3, Thumbnail: []byte("preview"), ThumbnailMediaType: "image/png", ThumbnailVersion: 3,
		PublishedAt: &now, IsPublished: true,
	}
	a.DB.Create(&work)

	c, recorder := testContext(http.MethodGet, fmt.Sprintf("/api/teacher/classes/%d/works", class.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(class.ID)}}
	c.Set("auth", authContext{User: teacher})
	a.handleTeacherWorks(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("teacher works failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Works []struct {
			ID           uint   `json:"id"`
			ThumbnailURL string `json:"thumbnail_url"`
		} `json:"works"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	wantURL := fmt.Sprintf("/api/teacher/works/%d/thumbnail?v=3", work.ID)
	if len(response.Works) != 1 || response.Works[0].ID != work.ID || response.Works[0].ThumbnailURL != wantURL {
		t.Fatalf("teacher work thumbnail URL missing: %#v", response.Works)
	}
}

func TestWorkThumbnailRejectsStalePublishedVersion(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "封面版本班", Active: true}
	a.DB.Create(&class)
	owner := User{ClassID: &class.ID, Class: &class, Role: RoleStudent, Name: "作者", LoginName: "thumbnail-version-owner", PasswordHash: "x"}
	a.DB.Create(&owner)
	a.DB.Create(&Work{UserID: owner.ID, ClassID: class.ID, Title: "版本作品", PublishedCode: "<html></html>", PublishedVersion: 3, IsPublished: true})
	dataURL, _ := testThumbnailDataURL(t)
	body, _ := json.Marshal(gin.H{"image": dataURL, "version": 2})
	c, recorder := testContext(http.MethodPost, "/api/student/work/thumbnail", string(body))
	c.Set("auth", authContext{User: owner})
	a.handleSaveWorkThumbnail(c)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale thumbnail should be rejected, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestTeacherClassesUsesMigratedAccessTable(t *testing.T) {
	a := newTestApp(t)
	teacher := User{Role: RoleTeacher, Name: "教师", LoginName: "teacher-test", PasswordHash: "x"}
	class := Class{Name: "授权班级", Active: true}
	a.DB.Create(&teacher)
	a.DB.Create(&class)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: class.ID})
	c, recorder := testContext(http.MethodGet, "/api/teacher/classes", "")
	c.Set("auth", authContext{User: teacher})
	a.handleTeacherClasses(c)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "授权班级") {
		t.Fatalf("teacher class query failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestTeacherStudentsIncludesAIRemaining(t *testing.T) {
	a := newTestApp(t)
	teacher := User{Role: RoleTeacher, Name: "教师", LoginName: "ai-count-teacher", PasswordHash: "x"}
	class := Class{Name: "AI 次数班", Active: true, AIRequestLimit: 5}
	a.DB.Create(&teacher)
	a.DB.Create(&class)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: class.ID})
	student := User{ClassID: &class.ID, Role: RoleStudent, Name: "学生", LoginName: "ai-count-student", PasswordHash: "x", AIExtraRequests: 2}
	a.DB.Create(&student)
	a.DB.Create(&AIUsageLog{UserID: student.ID, ClassID: class.ID, Status: "success"})
	a.DB.Create(&AIUsageLog{UserID: student.ID, ClassID: class.ID, Status: "success"})
	a.DB.Create(&AIUsageLog{UserID: student.ID, ClassID: class.ID, Status: "failed"})

	c, recorder := testContext(http.MethodGet, fmt.Sprintf("/api/teacher/classes/%d/students", class.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(class.ID)}}
	c.Set("auth", authContext{User: teacher})
	a.handleTeacherStudents(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("student list failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Students []struct {
			AIRemaining int `json:"ai_remaining"`
		} `json:"students"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode student list: %v", err)
	}
	if len(response.Students) != 1 || response.Students[0].AIRemaining != 5 {
		t.Fatalf("expected 5 remaining AI requests, got %#v", response.Students)
	}
}

func TestGalleryListMapsJoinedFields(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "作品班", Active: true}
	a.DB.Create(&class)
	user := User{ClassID: &class.ID, Role: RoleStudent, Name: "作者同学", LoginName: "author", PasswordHash: "x"}
	a.DB.Create(&user)
	now := time.Now()
	work := Work{UserID: user.ID, ClassID: class.ID, Title: "映射测试作品", Description: "简介", PublishedCode: "<html></html>", PublishedVersion: 3, PublishedAt: &now, IsPublished: true}
	a.DB.Create(&work)
	items, err := a.listGallery(class.ID, false, user.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("list gallery: items=%#v err=%v", items, err)
	}
	item := items[0]
	if item.ID != work.ID || item.Title != "映射测试作品" || item.Author != "作者同学" || item.ClassName != "作品班" || item.PublishedVersion != 3 || !item.IsMine {
		t.Fatalf("joined fields were mapped incorrectly: %#v", item)
	}
}

func TestPruneRevisionsKeepsLatestThirty(t *testing.T) {
	a := newTestApp(t)
	for index := 0; index < 35; index++ {
		a.DB.Create(&WorkRevision{WorkID: 99, Source: "test", Summary: fmt.Sprintf("revision-%d", index), Code: "x"})
	}
	a.pruneRevisions(99)
	var revisions []WorkRevision
	a.DB.Where("work_id = ?", 99).Order("id DESC").Find(&revisions)
	if len(revisions) != 30 || revisions[0].Summary != "revision-34" || revisions[29].Summary != "revision-5" {
		t.Fatalf("unexpected revisions after pruning: len=%d first=%q last=%q", len(revisions), revisions[0].Summary, revisions[len(revisions)-1].Summary)
	}
}

func TestTeacherAIUsageAggregatesCacheMetrics(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "缓存班", Active: true}
	a.DB.Create(&class)
	teacher := User{Role: RoleTeacher, Name: "教师", LoginName: "cache-teacher", PasswordHash: "x"}
	a.DB.Create(&teacher)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: class.ID})
	studentA := User{ClassID: &class.ID, Role: RoleStudent, Name: "小明", LoginName: "cache-student-a", PasswordHash: "x"}
	studentB := User{ClassID: &class.ID, Role: RoleStudent, Name: "小红", LoginName: "cache-student-b", PasswordHash: "x"}
	a.DB.Create(&studentA)
	a.DB.Create(&studentB)
	hitA, missA, promptA, completionA := 80, 20, 100, 30
	hitB, missB, promptB, completionB := 10, 90, 100, 40
	a.DB.Create(&AIUsageLog{UserID: studentA.ID, ClassID: class.ID, Status: "success", PromptTokens: &promptA, CompletionTokens: &completionA, PromptCacheHitTokens: &hitA, PromptCacheMissTokens: &missA})
	a.DB.Create(&AIUsageLog{UserID: studentB.ID, ClassID: class.ID, Status: "success", PromptTokens: &promptB, CompletionTokens: &completionB, PromptCacheHitTokens: &hitB, PromptCacheMissTokens: &missB})

	c, recorder := testContext(http.MethodGet, "/api/teacher/ai/usage", "")
	c.Set("auth", authContext{User: teacher})
	a.handleTeacherAIUsage(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected usage response, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Summary struct {
			PromptTokens          int64   `json:"prompt_tokens"`
			CompletionTokens      int64   `json:"completion_tokens"`
			PromptCacheHitTokens  int64   `json:"prompt_cache_hit_tokens"`
			PromptCacheMissTokens int64   `json:"prompt_cache_miss_tokens"`
			CacheHitRate          float64 `json:"cache_hit_rate"`
		} `json:"summary"`
		Logs []struct {
			ClassName   string `json:"class_name"`
			StudentName string `json:"student_name"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Summary.PromptTokens != 200 || response.Summary.CompletionTokens != 70 || response.Summary.PromptCacheHitTokens != 90 || response.Summary.PromptCacheMissTokens != 110 || response.Summary.CacheHitRate != 45 {
		t.Fatalf("unexpected cache summary: %#v", response.Summary)
	}
	if len(response.Logs) != 2 || response.Logs[0].ClassName != class.Name || response.Logs[0].StudentName != studentB.Name || response.Logs[1].StudentName != studentA.Name {
		t.Fatalf("usage names were not populated: %#v", response.Logs)
	}
}

func TestTeacherConversationsIncludeClassAndStudentNames(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "创意编程班", Active: true}
	a.DB.Create(&class)
	teacher := User{Role: RoleTeacher, Name: "教师", LoginName: "conversation-teacher", PasswordHash: "x"}
	student := User{ClassID: &class.ID, Role: RoleStudent, Name: "陈同学", LoginName: "conversation-student", PasswordHash: "x"}
	a.DB.Create(&teacher)
	a.DB.Create(&student)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: class.ID})
	conversation := AIConversation{UserID: student.ID, ClassID: class.ID, Title: "星空动画"}
	a.DB.Create(&conversation)
	a.DB.Create(&AIMessage{ConversationID: conversation.ID, Role: "user", Content: "做一个星空动画"})

	c, recorder := testContext(http.MethodGet, "/api/teacher/ai/conversations", "")
	c.Set("auth", authContext{User: teacher})
	a.handleTeacherConversations(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected conversations response, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Conversations []AIConversation `json:"conversations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Conversations) != 1 || response.Conversations[0].ClassName != class.Name || response.Conversations[0].StudentName != student.Name || len(response.Conversations[0].Messages) != 1 {
		t.Fatalf("conversation names or messages were not populated: %#v", response.Conversations)
	}
}

func TestAppliedAIProposalCanBeAppliedAgain(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "测试班", Active: true}
	a.DB.Create(&class)
	user := User{ClassID: &class.ID, Class: &class, Role: RoleStudent, Name: "同学", LoginName: "student-ai", PasswordHash: "x"}
	a.DB.Create(&user)
	conversation := AIConversation{UserID: user.ID, ClassID: class.ID, Title: "重复应用"}
	a.DB.Create(&conversation)
	message := AIMessage{ConversationID: conversation.ID, Role: "assistant", Content: "代码", Status: "success"}
	a.DB.Create(&message)
	now := time.Now()
	proposalCode := `<!doctype html><html><body><main>AI 提案</main></body></html>`
	proposal := AICodeProposal{MessageID: message.ID, ConversationID: conversation.ID, Code: proposalCode, SafetyOK: true, AppliedAt: &now}
	a.DB.Create(&proposal)
	work := Work{UserID: user.ID, ClassID: class.ID, DraftCode: `<!doctype html><html><body><main>学生修改</main></body></html>`}
	a.DB.Create(&work)

	apply := func() *httptest.ResponseRecorder {
		c, recorder := testContext(http.MethodPost, fmt.Sprintf("/api/student/ai/proposals/%d/apply", proposal.ID), "")
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(proposal.ID)}}
		c.Set("auth", authContext{User: user})
		a.handleApplyProposal(c)
		return recorder
	}

	if recorder := apply(); recorder.Code != http.StatusOK {
		t.Fatalf("expected reapplied proposal to succeed, code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	a.DB.First(&work, work.ID)
	if work.DraftCode != proposalCode {
		t.Fatalf("proposal code was not restored: %q", work.DraftCode)
	}
	var revisionCount int64
	a.DB.Model(&WorkRevision{}).Where("work_id = ?", work.ID).Count(&revisionCount)
	if revisionCount != 1 {
		t.Fatalf("expected one revision after replacing edited code, got %d", revisionCount)
	}

	if recorder := apply(); recorder.Code != http.StatusOK {
		t.Fatalf("expected identical proposal reapply to succeed, code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	a.DB.Model(&WorkRevision{}).Where("work_id = ?", work.ID).Count(&revisionCount)
	if revisionCount != 1 {
		t.Fatalf("identical reapply should not create another revision, got %d", revisionCount)
	}
}

func TestTeacherCanCreateRunTokenForAuthorizedDraft(t *testing.T) {
	a := newTestApp(t)
	teacher := User{Role: RoleTeacher, Name: "教师", LoginName: "preview-teacher", PasswordHash: "x"}
	class := Class{Name: "预览班", Active: true}
	a.DB.Create(&teacher)
	a.DB.Create(&class)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: class.ID})
	student := User{ClassID: &class.ID, Role: RoleStudent, Name: "学生", LoginName: "preview-student", PasswordHash: "x"}
	a.DB.Create(&student)
	work := Work{UserID: student.ID, ClassID: class.ID, DraftCode: "<html><body>preview</body></html>"}
	a.DB.Create(&work)
	c, recorder := testContext(http.MethodPost, fmt.Sprintf("/api/teacher/works/%d/run-token", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}, {Key: "action", Value: "run-token"}}
	c.Set("auth", authContext{User: teacher})
	a.handleTeacherWorkAction(c)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "token") {
		t.Fatalf("teacher preview token failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
