package app

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type loginRequest struct {
	Role      string `json:"role"`
	ClassID   uint   `json:"class_id"`
	LoginName string `json:"login_name"`
	Password  string `json:"password"`
}

func (a *App) handlePublicClasses(c *gin.Context) {
	var classes []Class
	if err := a.DB.Where("active = ?", true).Order("name ASC").Find(&classes).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "班级列表加载失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"classes": classes})
}

func (a *App) handlePublicRoster(c *gin.Context) {
	classID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var class Class
	if err := a.DB.First(&class, classID).Error; err != nil || !class.Active {
		jsonError(c, http.StatusNotFound, "班级不存在")
		return
	}
	if !class.LoginOpen {
		jsonError(c, http.StatusForbidden, "教师暂未开放本班登录")
		return
	}
	var users []User
	if err := a.DB.Select("id", "name", "login_name").Where("class_id = ? AND role = ?", classID, RoleStudent).Order("name ASC").Find(&users).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "名单加载失败")
		return
	}
	type rosterItem struct {
		ID        uint   `json:"id"`
		Name      string `json:"name"`
		LoginName string `json:"login_name"`
	}
	items := make([]rosterItem, 0, len(users))
	for _, user := range users {
		items = append(items, rosterItem{ID: user.ID, Name: user.Name, LoginName: user.LoginName})
	}
	c.JSON(http.StatusOK, gin.H{"class": class, "students": items})
}

func (a *App) handleLogin(c *gin.Context) {
	var request loginRequest
	if !bindJSON(c, &request) {
		return
	}
	request.LoginName = strings.TrimSpace(request.LoginName)
	key := fmt.Sprintf("%s:%d:%s", c.ClientIP(), request.ClassID, strings.ToLower(request.LoginName))
	if allowed, wait := a.LoginLimiter.allowed(key); !allowed {
		c.Header("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())+1))
		jsonError(c, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}

	role := request.Role
	if role == "" {
		role = RoleStudent
	}
	var user User
	query := a.DB.Preload("Class").Where("role = ? AND login_name = ?", role, request.LoginName)
	if role == RoleStudent {
		query = query.Where("class_id = ?", request.ClassID)
	}
	err := query.First(&user).Error
	if err != nil || !checkPassword(user.PasswordHash, request.Password) {
		a.LoginLimiter.fail(key)
		a.audit(nil, nil, "login", "login_failed", "failed", "账号或密码不正确")
		jsonError(c, http.StatusUnauthorized, "班级、姓名或密码不正确")
		return
	}
	if user.Locked {
		jsonError(c, http.StatusForbidden, "账号当前不可用，请联系教师")
		return
	}
	if role == RoleStudent && (user.Class == nil || !user.Class.Active || !user.Class.LoginOpen) {
		jsonError(c, http.StatusForbidden, "教师暂未开放本班登录")
		return
	}
	if err := a.createSession(c, user); err != nil {
		jsonError(c, http.StatusInternalServerError, "登录失败，请稍后重试")
		return
	}
	now := time.Now()
	a.DB.Model(&user).Update("last_login_at", &now)
	a.LoginLimiter.clear(key)
	a.audit(&user, user.ClassID, "login", "login", "success", "")
	c.JSON(http.StatusOK, gin.H{"user": userView(user)})
}

func (a *App) handleMe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": userView(authFrom(c).User)})
}

func (a *App) handleLogout(c *gin.Context) {
	auth := authFrom(c)
	a.DB.Delete(&Session{}, auth.Session.ID)
	a.clearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) handleChangePassword(c *gin.Context) {
	var request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !bindJSON(c, &request) {
		return
	}
	auth := authFrom(c)
	if !checkPassword(auth.User.PasswordHash, request.CurrentPassword) {
		jsonError(c, http.StatusBadRequest, "当前密码不正确")
		return
	}
	if !validNewPassword(request.NewPassword) {
		jsonError(c, http.StatusBadRequest, "新密码至少六位，且不能继续使用 123456")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "密码修改失败")
		return
	}
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", auth.User.ID).Updates(map[string]any{
			"password_hash": string(hash), "must_change_password": false,
		}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", auth.User.ID).Delete(&Session{}).Error
	})
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "密码修改失败")
		return
	}
	a.clearSessionCookie(c)
	a.audit(&auth.User, auth.User.ClassID, "account", "change_password", "success", "")
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "密码已修改，请使用新密码重新登录"})
}

func userView(user User) gin.H {
	view := gin.H{
		"id": user.ID, "name": user.Name, "login_name": user.LoginName, "role": user.Role,
		"class_id": user.ClassID, "must_change_password": user.MustChangePassword,
		"locked": user.Locked, "ai_blocked": user.AIBlocked,
	}
	if user.Class != nil {
		view["class"] = user.Class
	}
	return view
}

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	var value uint
	if _, err := fmt.Sscanf(c.Param(name), "%d", &value); err != nil || value == 0 {
		jsonError(c, http.StatusBadRequest, "参数不正确")
		return 0, false
	}
	return value, true
}

func sortedSafetyText(issues []string) string {
	copyIssues := append([]string(nil), issues...)
	sort.Strings(copyIssues)
	return strings.Join(copyIssues, "\n")
}
