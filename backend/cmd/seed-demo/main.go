package main

import (
	"fmt"
	"log"
	"strings"

	"class-code-lab/backend/internal/app"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type demoClass struct {
	Name     string
	Students []string
}

var demoClasses = []demoClass{
	{Name: "演示班 A", Students: []string{"演示学生 01", "演示学生 02", "演示学生 03", "演示学生 04"}},
	{Name: "演示班 B", Students: []string{"演示学生 05", "演示学生 06", "演示学生 07"}},
}

func main() {
	cfg := app.LoadConfig()
	db, err := app.OpenDatabase(cfg)
	if err != nil {
		log.Fatal(err)
	}
	db.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	if err := seed(db, cfg); err != nil {
		log.Fatal(err)
	}
	fmt.Println("模拟账号已重新导入。")
	fmt.Println("学生和教师账号的初始密码均为：123456")
	fmt.Println("所有模拟账号首次登录后都必须修改密码。")
}

func seed(db *gorm.DB, cfg app.Config) error {
	var defaultTeacher app.User
	if err := db.Where("role = ? AND login_name = ?", app.RoleTeacher, cfg.AdminLogin).First(&defaultTeacher).Error; err != nil {
		return fmt.Errorf("找不到默认教师账号 %q: %w", cfg.AdminLogin, err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	classIDs := make([]uint, 0, len(demoClasses))
	for _, spec := range demoClasses {
		class, err := upsertClass(db, spec.Name)
		if err != nil {
			return err
		}
		classIDs = append(classIDs, class.ID)
		if err := clearManagedClassData(db, class.ID); err != nil {
			return err
		}
		if err := ensureAccess(db, defaultTeacher.ID, class.ID); err != nil {
			return err
		}
		for _, studentName := range spec.Students {
			if _, err := createAccount(db, class.ID, app.RoleStudent, studentName, string(passwordHash)); err != nil {
				return err
			}
			teacherName := strings.Replace(studentName, "学生", "教师", 1)
			teacher, err := createAccount(db, class.ID, app.RoleTeacher, teacherName, string(passwordHash))
			if err != nil {
				return err
			}
			if err := ensureAccess(db, teacher.ID, class.ID); err != nil {
				return err
			}
		}
	}

	var demoTeachers []app.User
	if err := db.Where("role = ? AND class_id IN ?", app.RoleTeacher, classIDs).Find(&demoTeachers).Error; err != nil {
		return err
	}
	for _, teacher := range demoTeachers {
		for _, classID := range classIDs {
			if err := ensureAccess(db, teacher.ID, classID); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertClass(db *gorm.DB, name string) (app.Class, error) {
	var class app.Class
	err := db.Where("name = ?", name).First(&class).Error
	if err == gorm.ErrRecordNotFound {
		class = app.Class{Name: name, Active: true, LoginOpen: true, AIEnabled: true, PublishEnabled: true, AllowEditPublished: true, AIRequestLimit: 12, AIConcurrency: 6, AIRequestCooldownSeconds: 1}
		if err := db.Create(&class).Error; err != nil {
			return class, err
		}
		return class, nil
	}
	if err != nil {
		return class, err
	}
	err = db.Model(&class).Updates(map[string]any{
		"active": true, "login_open": true, "ai_enabled": true,
		"publish_enabled": true, "allow_edit_published": true,
		"ai_request_limit": 12, "ai_concurrency": 6, "ai_request_cooldown_seconds": 1,
	}).Error
	return class, err
}

func createAccount(db *gorm.DB, classID uint, role, name, passwordHash string) (app.User, error) {
	user := app.User{ClassID: &classID, Role: role, Name: name, LoginName: name, PasswordHash: passwordHash, MustChangePassword: true}
	if err := db.Create(&user).Error; err != nil {
		return user, err
	}
	return user, nil
}

func ensureAccess(db *gorm.DB, teacherID, classID uint) error {
	var count int64
	if err := db.Model(&app.TeacherClassAccess{}).Where("teacher_id = ? AND class_id = ?", teacherID, classID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return db.Create(&app.TeacherClassAccess{TeacherID: teacherID, ClassID: classID}).Error
}

func clearManagedClassData(db *gorm.DB, classID uint) error {
	var userIDs []uint
	if err := db.Model(&app.User{}).Where("class_id = ?", classID).Pluck("id", &userIDs).Error; err != nil {
		return err
	}
	var workIDs []uint
	if err := db.Model(&app.Work{}).Where("class_id = ?", classID).Pluck("id", &workIDs).Error; err != nil {
		return err
	}
	var conversationIDs []uint
	if err := db.Model(&app.AIConversation{}).Where("class_id = ?", classID).Pluck("id", &conversationIDs).Error; err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if len(userIDs) > 0 {
			if err := tx.Where("user_id IN ?", userIDs).Delete(&app.Session{}).Error; err != nil {
				return err
			}
			if err := tx.Where("class_id = ? AND teacher_id IN ?", classID, userIDs).Delete(&app.TeacherClassAccess{}).Error; err != nil {
				return err
			}
		}
		if len(workIDs) > 0 {
			if err := tx.Where("work_id IN ?", workIDs).Delete(&app.WorkRevision{}).Error; err != nil {
				return err
			}
		}
		if len(conversationIDs) > 0 {
			if err := tx.Where("conversation_id IN ?", conversationIDs).Delete(&app.AICodeProposal{}).Error; err != nil {
				return err
			}
			if err := tx.Where("conversation_id IN ?", conversationIDs).Delete(&app.AIMessage{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("class_id = ?", classID).Delete(&app.AIUsageLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("class_id = ?", classID).Delete(&app.AIConversation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("class_id = ?", classID).Delete(&app.RunToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("class_id = ?", classID).Delete(&app.Work{}).Error; err != nil {
			return err
		}
		if err := tx.Where("class_id = ?", classID).Delete(&app.AuditLog{}).Error; err != nil {
			return err
		}
		if len(userIDs) > 0 {
			if err := tx.Where("id IN ?", userIDs).Delete(&app.User{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
