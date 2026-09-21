package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const sessionCookieName = "class_code_lab_session"

type authContext struct {
	User    User
	Session Session
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string][]time.Time)}
}

func (l *loginLimiter) allowed(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-5 * time.Minute)
	items := l.attempts[key]
	kept := items[:0]
	for _, item := range items {
		if item.After(cutoff) {
			kept = append(kept, item)
		}
	}
	l.attempts[key] = kept
	if len(kept) >= 5 {
		return false, kept[0].Add(5 * time.Minute).Sub(now)
	}
	return true, 0
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.attempts[key], time.Now())
}

func (l *loginLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func validNewPassword(password string) bool {
	return len([]rune(password)) >= 6 && password != "123456"
}

func (a *App) setSessionCookie(c *gin.Context, token string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/api", HttpOnly: true,
		Secure: a.Config.CookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
	})
}

func (a *App) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/api", HttpOnly: true,
		Secure: a.Config.CookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(0, 0), MaxAge: -1,
	})
}

func (a *App) createSession(c *gin.Context, user User) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(a.Config.SessionTTL)
	session := Session{TokenHash: hashToken(token), UserID: user.ID, ExpiresAt: expires}
	if err := a.DB.Create(&session).Error; err != nil {
		return err
	}
	a.setSessionCookie(c, token, expires)
	return nil
}

func (a *App) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(cookie) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
			return
		}
		var session Session
		err = a.DB.Preload("User.Class").Where("token_hash = ? AND expires_at > ?", hashToken(cookie), time.Now()).First(&session).Error
		if err != nil {
			a.clearSessionCookie(c)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已过期，请重新登录"})
			return
		}
		if session.User.Locked {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "账号当前不可用，请联系教师"})
			return
		}
		c.Set("auth", authContext{User: session.User, Session: session})
		c.Next()
	}
}

func authFrom(c *gin.Context) authContext {
	value, _ := c.Get("auth")
	ctx, _ := value.(authContext)
	return ctx
}

func requireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authFrom(c).User.Role != role {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "没有访问权限"})
			return
		}
		c.Next()
	}
}

func requirePasswordChanged() gin.HandlerFunc {
	return func(c *gin.Context) {
		if authFrom(c).User.MustChangePassword {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "请先修改初始密码", "code": "PASSWORD_CHANGE_REQUIRED"})
			return
		}
		c.Next()
	}
}

func (a *App) canManageClass(teacherID, classID uint) bool {
	var count int64
	a.DB.Model(&TeacherClassAccess{}).Where("teacher_id = ? AND class_id = ?", teacherID, classID).Count(&count)
	return count > 0
}

func (a *App) cleanupExpired() {
	a.DB.Where("expires_at <= ?", time.Now()).Delete(&Session{})
	a.DB.Where("expires_at <= ?", time.Now()).Delete(&RunToken{})
}

func isNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}
