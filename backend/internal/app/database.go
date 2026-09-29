package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const sqliteScheme = "sqlite://"

func OpenDatabase(cfg Config) (*gorm.DB, error) {
	var db *gorm.DB
	var err error
	if strings.HasPrefix(cfg.DatabaseDSN, sqliteScheme) {
		db, err = openSQLite(strings.TrimPrefix(cfg.DatabaseDSN, sqliteScheme))
	} else {
		db, err = openMySQL(cfg.DatabaseDSN)
	}
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	if err := initializeAISettings(db, cfg); err != nil {
		return nil, err
	}
	if err := seedAdmin(db, cfg); err != nil {
		return nil, err
	}
	return db, nil
}

// openSQLite opens (and auto-creates) a SQLite database file. gorm.Open will
// create the file if it does not exist; the parent directory must be present.
func openSQLite(path string) (*gorm.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite dir: %w", err)
		}
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	// SQLite tuning: WAL avoids reader/writer blocking under concurrent
	// submissions; busy_timeout waits on locks instead of failing; foreign
	// keys enable OnDelete:CASCADE (off by default in SQLite).
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if err := db.Exec(pragma).Error; err != nil {
			return nil, fmt.Errorf("sqlite pragma: %w", err)
		}
	}
	return db, nil
}

func openMySQL(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil && strings.Contains(err.Error(), "Unknown database") {
		if createErr := createDatabase(dsn); createErr != nil {
			return nil, fmt.Errorf("create database: %w", createErr)
		}
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	}
	return db, err
}

func migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Class{}, &User{}, &TeacherClassAccess{}, &Session{}, &Work{}, &WorkRevision{}, &WorkScore{},
		&AIConversation{}, &AIMessage{}, &AICodeProposal{}, &AIUsageLog{}, &AuditLog{}, &RunToken{},
		&AISettings{}, &AIProvider{},
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
