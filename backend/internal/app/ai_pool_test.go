package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func testProvider(name, baseURL string, capacity int) AIProvider {
	return AIProvider{Name: name, BaseURL: baseURL + "/v1", APIKey: "private-key", Model: "test-model", Enabled: true, ConfigVersion: 1,
		MaxConcurrency: capacity, TimeoutSeconds: 2, MaxOutputTokens: 512, ModificationMaxTokens: 256, ReasoningEffort: "low"}
}

func TestLegacyAIImportOnceAndEmptyInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	cfg := Config{DatabaseDSN: "sqlite://" + path, AdminLogin: "teacher", AdminPassword: "test-password",
		AIBaseURL: "https://example.com/v1", AIAPIKey: "original-secret", AIModel: "old-model",
		AIHistoryChars: 12345, AIMaxConcurrency: 4, AIRequestsPerStudent: 9}
	db, err := OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var provider AIProvider
	if err := db.First(&provider).Error; err != nil || provider.APIKey != "original-secret" || provider.Model != "old-model" || provider.MaxConcurrency != 4 {
		t.Fatalf("import: %#v %v", provider, err)
	}
	var settings AISettings
	if err := db.First(&settings, 1).Error; err != nil || settings.HistoryChars != 12345 || settings.DefaultClassConcurrency != 4 {
		t.Fatalf("settings: %#v %v", settings, err)
	}
	db.Model(&provider).Update("model", "edited-in-db")
	sqlDB, _ := db.DB()
	sqlDB.Close()
	cfg.AIAPIKey, cfg.AIModel = "changed-env", "changed-env-model"
	db, err = OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var providers []AIProvider
	db.Find(&providers)
	if len(providers) != 1 || providers[0].Model != "edited-in-db" || providers[0].APIKey != "original-secret" {
		t.Fatalf("legacy import repeated: %#v", providers)
	}
	sqlDB, _ = db.DB()
	sqlDB.Close()

	emptyCfg := Config{DatabaseDSN: "sqlite://" + filepath.Join(t.TempDir(), "new.db"), AdminLogin: "teacher", AdminPassword: "test-password"}
	emptyDB, err := OpenDatabase(emptyCfg)
	if err != nil {
		t.Fatal(err)
	}
	if New(emptyCfg, emptyDB).AIConfigured() {
		t.Fatal("empty install should not have AI")
	}
	emptySQL, _ := emptyDB.DB()
	emptySQL.Close()
	emptyCfg.AIAPIKey, emptyCfg.AIModel, emptyCfg.AIBaseURL = "later-secret", "later-model", "https://example.com/v1"
	emptyDB, err = OpenDatabase(emptyCfg)
	if err != nil {
		t.Fatal(err)
	}
	if New(emptyCfg, emptyDB).AIConfigured() {
		t.Fatal("later environment must not import after first initialization")
	}
}

func TestMySQLAISettingsMigration(t *testing.T) {
	rootDSN := os.Getenv("AI_TEST_MYSQL_ROOT_DSN")
	if rootDSN == "" {
		t.Skip("set AI_TEST_MYSQL_ROOT_DSN for a disposable MySQL schema")
	}
	name := fmt.Sprintf("class_code_lab_ai_test_%d", time.Now().UnixNano())
	dsn := strings.TrimSuffix(rootDSN, "/") + "/" + name + "?charset=utf8mb4&parseTime=True&loc=Local"
	cfg := Config{DatabaseDSN: dsn, AdminLogin: "teacher", AdminPassword: "test-password",
		AIBaseURL: "https://example.com/v1", AIAPIKey: "mysql-secret", AIModel: "mysql-model"}
	rootDB, err := openMySQL(rootDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { rootDB.Exec("DROP DATABASE `" + name + "`") }()
	db, err := OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var settings AISettings
	var provider AIProvider
	if err := db.First(&settings, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&provider).Error; err != nil || provider.APIKey != "mysql-secret" {
		t.Fatalf("provider: %#v %v", provider, err)
	}
	a := New(cfg, db)
	a.markProviderFailure(provider, &AIUpstreamError{Category: "authentication"})
	db.First(&provider, provider.ID)
	if !provider.AuthFailed {
		t.Fatal("MySQL did not persist authentication failure")
	}
	a.clearProviderError(provider)
	db.First(&provider, provider.ID)
	if provider.AuthFailed {
		t.Fatal("MySQL did not clear authentication failure")
	}
	sqlDB, _ := db.DB()
	sqlDB.Close()
}

func TestProviderAPIHidesKeyAndUpdatesImmediately(t *testing.T) {
	a := newTestApp(t)
	teacher := User{Role: RoleTeacher, Name: "Teacher", LoginName: "teacher", PasswordHash: "x"}
	a.DB.Create(&teacher)
	c, w := testContext(http.MethodPost, "/api/teacher/ai/providers", `{"name":"A","base_url":"https://example.com/v1","api_key":"super-secret","model":"model-a","max_concurrency":2}`)
	c.Set("auth", authContext{User: teacher})
	a.handleCreateAIProvider(c)
	if w.Code != http.StatusCreated || strings.Contains(w.Body.String(), "super-secret") || !a.AIConfigured() {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var provider AIProvider
	a.DB.First(&provider)
	c, w = testContext(http.MethodPatch, fmt.Sprintf("/api/teacher/ai/providers/%d", provider.ID), `{"model":"model-b","api_key":""}`)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(provider.ID)}}
	c.Set("auth", authContext{User: teacher})
	a.handleUpdateAIProvider(c)
	a.DB.First(&provider, provider.ID)
	if w.Code != http.StatusOK || provider.Model != "model-b" || provider.APIKey != "super-secret" {
		t.Fatalf("edit: %d %#v", w.Code, provider)
	}
	stale := provider
	stale.ConfigVersion--
	a.markProviderFailure(stale, &AIUpstreamError{Category: "authentication"})
	a.DB.First(&provider, provider.ID)
	if provider.AuthFailed {
		t.Fatal("old request blocked an edited provider")
	}
	selected, release, err := a.acquireProvider(context.Background(), nil, func() {})
	if err != nil || selected.Model != "model-b" {
		t.Fatalf("edit not live: %#v %v", selected, err)
	}
	release()
	c, w = testContext(http.MethodGet, "/api/teacher/ai/providers", "")
	a.handleAIProviders(c)
	if strings.Contains(w.Body.String(), "super-secret") || !strings.Contains(w.Body.String(), `"api_key_present":true`) {
		t.Fatalf("key exposed or flag absent: %s", w.Body.String())
	}
	c, w = testContext(http.MethodPatch, fmt.Sprintf("/api/teacher/ai/providers/%d", provider.ID), `{"enabled":false}`)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(provider.ID)}}
	c.Set("auth", authContext{User: teacher})
	a.handleUpdateAIProvider(c)
	if w.Code != http.StatusOK || a.AIConfigured() {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
}

func TestAISettingsApplyOnlyToNewClasses(t *testing.T) {
	a := newTestApp(t)
	if err := initializeAISettings(a.DB, Config{}); err != nil {
		t.Fatal(err)
	}
	teacher := User{Role: RoleTeacher, Name: "Teacher", LoginName: "teacher", PasswordHash: "x"}
	a.DB.Create(&teacher)
	oldClass := Class{Name: "旧班级", AIRequestLimit: 12, AIConcurrency: 6}
	a.DB.Create(&oldClass)
	zeroClass := Class{Name: "暂停 AI 次数的班级", AIConcurrency: 6}
	a.DB.Create(&zeroClass)
	a.DB.Model(&zeroClass).Update("ai_request_limit", 0)
	student := User{ClassID: &zeroClass.ID, Role: RoleStudent, Name: "学生", LoginName: "zero-student", PasswordHash: "x"}
	a.DB.Create(&student)
	c, w := testContext(http.MethodPatch, "/api/teacher/ai/settings", `{"default_class_concurrency":3,"default_student_requests":7,"history_messages":5,"history_chars":9000}`)
	c.Set("auth", authContext{User: teacher})
	a.handleUpdateAISettings(c)
	if w.Code != http.StatusOK {
		t.Fatalf("settings update: %d %s", w.Code, w.Body.String())
	}
	c, w = testContext(http.MethodPost, "/api/teacher/classes", `{"name":"新班级"}`)
	c.Set("auth", authContext{User: teacher})
	a.handleCreateClass(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("class create: %d %s", w.Code, w.Body.String())
	}
	var newClass Class
	a.DB.Where("name = ?", "新班级").First(&newClass)
	a.DB.First(&oldClass, oldClass.ID)
	if newClass.AIRequestLimit != 7 || newClass.AIConcurrency != 3 || oldClass.AIRequestLimit != 12 || oldClass.AIConcurrency != 6 {
		t.Fatalf("old=%#v new=%#v", oldClass, newClass)
	}
	if remaining := a.studentAIRemaining(student, zeroClass.ID); remaining != 0 {
		t.Fatalf("zero class limit became %d", remaining)
	}
}

func TestProviderSchedulerCapacityQueueAndCooldown(t *testing.T) {
	a := newTestApp(t)
	first := testProvider("A", "https://a.example", 1)
	second := testProvider("B", "https://b.example", 2)
	a.DB.Create(&first)
	a.DB.Create(&second)
	p1, release1, err := a.acquireProvider(context.Background(), nil, func() {})
	if err != nil || p1.ID != first.ID {
		t.Fatalf("first selection: %#v %v", p1, err)
	}
	p2, release2, _ := a.acquireProvider(context.Background(), nil, func() {})
	p3, release3, _ := a.acquireProvider(context.Background(), nil, func() {})
	if p2.ID != second.ID || p3.ID != second.ID {
		t.Fatalf("capacity selection: %d %d", p2.ID, p3.ID)
	}
	queued := make(chan struct{}, 1)
	acquired := make(chan AIProvider, 1)
	go func() {
		p, release, err := a.acquireProvider(context.Background(), nil, func() { queued <- struct{}{} })
		if err == nil {
			acquired <- p
			release()
		}
	}()
	select {
	case <-queued:
	case <-time.After(time.Second):
		t.Fatal("expected queue")
	}
	release2()
	select {
	case p := <-acquired:
		if p.ID != second.ID {
			t.Fatalf("queue chose %d", p.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("queue did not wake")
	}
	release1()
	release3()
	a.markProviderFailure(first, &AIUpstreamError{Category: "rate_limit"})
	p, release, err := a.acquireProvider(context.Background(), nil, func() {})
	if err != nil || p.ID != second.ID {
		t.Fatalf("cooldown selection: %#v %v", p, err)
	}
	release()
	a.clearProviderError(first)
	a.markProviderFailure(first, &AIUpstreamError{Category: "authentication"})
	restarted := New(Config{}, a.DB)
	p, release, err = restarted.acquireProvider(context.Background(), nil, func() {})
	if err != nil || p.ID != second.ID {
		t.Fatalf("authentication block after restart: %#v %v", p, err)
	}
	release()
	restarted.clearProviderError(first)
}

func makeStudentRequestApp(t *testing.T) (*App, User) {
	t.Helper()
	a := newTestApp(t)
	class := Class{Name: "AI班", Active: true, AIEnabled: true, AIRequestLimit: 10, AIConcurrency: 4}
	a.DB.Create(&class)
	student := User{ClassID: &class.ID, Class: &class, Role: RoleStudent, Name: "学生", LoginName: "student", PasswordHash: "x"}
	a.DB.Create(&student)
	return a, student
}

func TestAIRequestFallsBackBeforeOutput(t *testing.T) {
	var failedCalls, goodCalls atomic.Int32
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failedCalls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer failed.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"good-model","choices":[{"message":{"content":"你好"}}]}`)
	}))
	defer good.Close()
	a, student := makeStudentRequestApp(t)
	p1, p2 := testProvider("failed", failed.URL, 1), testProvider("good", good.URL, 1)
	a.DB.Create(&p1)
	a.DB.Create(&p2)
	c, w := testContext(http.MethodPost, "/api/student/ai/messages", `{"message":"你好"}`)
	c.Set("auth", authContext{User: student})
	a.handleAIMessage(c)
	if failedCalls.Load() != 1 || goodCalls.Load() != 1 || !strings.Contains(w.Body.String(), "event: done") {
		t.Fatalf("fallback failed: %s, calls %d/%d", w.Body.String(), failedCalls.Load(), goodCalls.Load())
	}
	var logs []AIUsageLog
	a.DB.Order("id asc").Find(&logs)
	if len(logs) != 2 || logs[0].ProviderName != "failed" || logs[0].Status != "failed" || logs[1].ProviderName != "good" || logs[1].Status != "success" {
		t.Fatalf("usage: %#v", logs)
	}
}

func TestAIRequestNeverReplaysAfterFirstDelta(t *testing.T) {
	var secondCalls atomic.Int32
	partial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"半句\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"failed\"}}\n\n")
	}))
	defer partial.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "second"}}}})
	}))
	defer second.Close()
	a, student := makeStudentRequestApp(t)
	p1, p2 := testProvider("partial", partial.URL, 1), testProvider("second", second.URL, 1)
	a.DB.Create(&p1)
	a.DB.Create(&p2)
	c, w := testContext(http.MethodPost, "/api/student/ai/messages", `{"message":"你好"}`)
	c.Set("auth", authContext{User: student})
	a.handleAIMessage(c)
	if secondCalls.Load() != 0 || !strings.Contains(w.Body.String(), "event: delta") || !strings.Contains(w.Body.String(), "event: error") || strings.Contains(w.Body.String(), "event: done") {
		t.Fatalf("partial output replayed: %s, second=%d", w.Body.String(), secondCalls.Load())
	}
}

func TestStudentAILeaseCanBeReplacedAfterRequestContextEnds(t *testing.T) {
	a := newTestApp(t)
	firstCtx, cancel := context.WithCancel(context.Background())
	release, ok := a.acquireStudentAI(42, firstCtx)
	if !ok || release == nil {
		t.Fatal("first request should acquire the student lease")
	}
	if _, ok := a.acquireStudentAI(42, context.Background()); ok {
		t.Fatal("active request should block a duplicate")
	}
	cancel()
	secondRelease, ok := a.acquireStudentAI(42, context.Background())
	if !ok {
		t.Fatal("request with a canceled predecessor should acquire the lease")
	}
	secondRelease()
	release()
}

func TestProviderAIClientIsReusedUntilConfigVersionChanges(t *testing.T) {
	a := newTestApp(t)
	provider := testProvider("cached", "https://example.com", 1)
	provider.ID = 7
	first := a.aiClientForProvider(provider)
	second := a.aiClientForProvider(provider)
	if first != second {
		t.Fatal("expected provider HTTP client to be reused")
	}
	provider.ConfigVersion++
	third := a.aiClientForProvider(provider)
	if third == first {
		t.Fatal("expected edited provider to receive a new client")
	}
}
