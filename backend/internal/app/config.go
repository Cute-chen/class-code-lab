package app

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseDSN          string
	AppAddress           string
	RunnerAddress        string
	FrontendDist         string
	CookieSecure         bool
	SessionTTL           time.Duration
	RunTokenTTL          time.Duration
	AdminName            string
	AdminLogin           string
	AdminPassword        string
	AIBaseURL            string
	AIAPIKey             string
	AIModel              string
	AITimeout            time.Duration
	AIMaxConcurrency     int
	AIRequestsPerStudent int
	AIMaxOutputTokens    int
	AIModificationTokens int
	AIReasoningEffort    string
	AIHistoryMessages    int
	AIHistoryChars       int
}

func LoadConfig() Config {
	loadDotEnv()
	return Config{
		DatabaseDSN:          env("DATABASE_DSN", "sqlite://./class-code-lab.db"),
		AppAddress:           env("APP_ADDRESS", ":8080"),
		RunnerAddress:        env("RUNNER_ADDRESS", ":8081"),
		FrontendDist:         env("FRONTEND_DIST", ""),
		CookieSecure:         envBool("COOKIE_SECURE", false),
		SessionTTL:           time.Duration(envInt("SESSION_HOURS", 10)) * time.Hour,
		RunTokenTTL:          time.Duration(envInt("RUN_TOKEN_MINUTES", 5)) * time.Minute,
		AdminName:            env("ADMIN_NAME", "任课教师"),
		AdminLogin:           env("ADMIN_LOGIN", "teacher"),
		AdminPassword:        env("ADMIN_PASSWORD", "123456"),
		AIBaseURL:            normalizeAIBaseURL(env("AI_BASE_URL", "https://api.openai.com/v1")),
		AIAPIKey:             strings.TrimSpace(os.Getenv("AI_API_KEY")),
		AIModel:              strings.TrimSpace(os.Getenv("AI_MODEL")),
		AITimeout:            time.Duration(envInt("AI_TIMEOUT_SECONDS", 600)) * time.Second,
		AIMaxConcurrency:     envInt("AI_MAX_CONCURRENCY", 6),
		AIRequestsPerStudent: envInt("AI_REQUESTS_PER_STUDENT", 12),
		AIMaxOutputTokens:    envInt("AI_MAX_OUTPUT_TOKENS", 393216),
		AIModificationTokens: envInt("AI_MODIFICATION_MAX_TOKENS", 393216),
		AIReasoningEffort:    envChoice("AI_REASONING_EFFORT", "low", "none", "low", "high", "max", "auto"),
		AIHistoryMessages:    envInt("AI_HISTORY_MESSAGES", 8),
		AIHistoryChars:       envInt("AI_HISTORY_CHARS", 16000),
	}
}

// loadDotEnv loads a local .env file for the standalone Go binary. Existing
// process environment variables always win, so deployment environments can
// override local development values without editing files.
func loadDotEnv() {
	candidates := []string{
		".env",
		filepath.Join("backend", ".env"),
		filepath.Join("..", ".env"),
	}
	// Portable exe: also look beside the binary so teachers can drop a .env
	// next to class-code-lab.exe to override defaults.
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, path := range candidates {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		if err := loadDotEnvFile(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			// A malformed or unreadable optional .env must not prevent the
			// service from starting with normal process environment variables.
			return
		}
	}
}

func loadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
				value = value[1 : len(value)-1]
			}
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envChoice(key, fallback string, allowed ...string) string {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return fallback
}

func normalizeAIBaseURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if !strings.HasSuffix(value, "/v1") {
		value += "/v1"
	}
	return value
}
