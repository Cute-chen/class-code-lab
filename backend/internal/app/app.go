package app

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type App struct {
	Config            Config
	DB                *gorm.DB
	LoginLimiter      *loginLimiter
	providerMu        sync.Mutex
	providerActive    map[uint]int
	providerNotify    chan struct{}
	providerCursor    uint
	providerClientsMu sync.Mutex
	providerClients   map[uint]providerClientEntry
	classLocksMu      sync.Mutex
	classLocks        map[uint]chan struct{}
	studentAIMu       sync.Mutex
	studentAI         map[uint]*studentAILease
}

type providerClientEntry struct {
	version uint
	client  *AIClient
}

type studentAILease struct {
	done <-chan struct{}
}

func (a *App) acquireStudentAI(userID uint, ctx context.Context) (func(), bool) {
	a.studentAIMu.Lock()
	if existing, ok := a.studentAI[userID]; ok {
		select {
		case <-existing.done:
			delete(a.studentAI, userID)
		default:
			a.studentAIMu.Unlock()
			return nil, false
		}
	}
	lease := &studentAILease{done: ctx.Done()}
	a.studentAI[userID] = lease
	a.studentAIMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.studentAIMu.Lock()
			if a.studentAI[userID] == lease {
				delete(a.studentAI, userID)
			}
			a.studentAIMu.Unlock()
		})
	}, true
}

func (a *App) acquireClassAIWait(ctx context.Context, classID uint, limit int) (func(), bool, bool) {
	if limit <= 0 {
		limit = defaultAISettings().DefaultClassConcurrency
	}
	a.classLocksMu.Lock()
	sem, ok := a.classLocks[classID]
	if !ok || cap(sem) != limit {
		sem = make(chan struct{}, limit)
		a.classLocks[classID] = sem
	}
	a.classLocksMu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true, false
	default:
	}
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true, true
	case <-ctx.Done():
		return nil, false, true
	}
}

func New(cfg Config, db *gorm.DB) *App {
	return &App{
		Config: cfg, DB: db, LoginLimiter: newLoginLimiter(),
		classLocks:     make(map[uint]chan struct{}),
		providerActive: make(map[uint]int), providerNotify: make(chan struct{}),
		studentAI:       make(map[uint]*studentAILease),
		providerClients: make(map[uint]providerClientEntry),
	}
}

func (a *App) audit(actor *User, classID *uint, target, action, result, detail string) {
	logEntry := AuditLog{ClassID: classID, Target: target, Action: action, Result: result, Detail: detail}
	if actor != nil {
		logEntry.ActorID = &actor.ID
		logEntry.ActorName = actor.Name
	}
	if err := a.DB.Create(&logEntry).Error; err != nil {
		log.Printf("audit log failed: %v", err)
	}
}

func (a *App) acquireClassAI(classID uint, limit int) (func(), bool) {
	if limit <= 0 {
		limit = defaultAISettings().DefaultClassConcurrency
	}
	a.classLocksMu.Lock()
	sem, ok := a.classLocks[classID]
	if !ok || cap(sem) != limit {
		sem = make(chan struct{}, limit)
		a.classLocks[classID] = sem
	}
	a.classLocksMu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true
	default:
		return nil, false
	}
}

func jsonError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		jsonError(c, http.StatusBadRequest, "请求内容格式不正确")
		return false
	}
	return true
}

func (a *App) StartCleanupLoop() {
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			a.cleanupExpired()
		}
	}()
}
