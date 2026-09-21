package app

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func OpenDatabase(cfg Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DatabaseDSN), &gorm.Config{})
	if err != nil && strings.Contains(err.Error(), "Unknown database") {
		if createErr := createDatabase(cfg.DatabaseDSN); createErr != nil {
			return nil, fmt.Errorf("create database: %w", createErr)
		}
		db, err = gorm.Open(mysql.Open(cfg.DatabaseDSN), &gorm.Config{})
	}
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	if err := seedAdmin(db, cfg); err != nil {
		return nil, err
	}
	return db, nil
}

func migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Class{}, &User{}, &TeacherClassAccess{}, &Session{}, &Work{}, &WorkRevision{}, &WorkScore{},
		&AIConversation{}, &AIMessage{}, &AICodeProposal{}, &AIUsageLog{}, &AuditLog{}, &RunToken{},
	)
}

func createDatabase(dsn string) error {
	parts := strings.SplitN(dsn, "/", 2)
	if len(parts) != 2 {
		return errors.New("DATABASE_DSN format is invalid")
	}
	databasePart := parts[1]
	databaseName := strings.SplitN(databasePart, "?", 2)[0]
	if databaseName == "" || strings.ContainsAny(databaseName, "`'\"") {
		return errors.New("database name is invalid")
	}
	adminDSN := parts[0] + "/?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(adminDSN), &gorm.Config{})
	if err != nil {
		return err
	}
	return db.Exec("CREATE DATABASE IF NOT EXISTS `" + databaseName + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error
}

func seedAdmin(db *gorm.DB, cfg Config) error {
	var teacher User
	err := db.Where("role = ? AND login_name = ?", RoleTeacher, cfg.AdminLogin).First(&teacher).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	teacher = User{
		Role: RoleTeacher, Name: cfg.AdminName, LoginName: cfg.AdminLogin,
		PasswordHash: string(hash), MustChangePassword: true,
	}
	return db.Create(&teacher).Error
}
