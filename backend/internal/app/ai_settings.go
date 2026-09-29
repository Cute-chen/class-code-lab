package app

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func defaultAISettings() AISettings {
	return AISettings{ID: 1, DefaultClassConcurrency: 6, DefaultStudentRequests: 12, HistoryMessages: 8, HistoryChars: 16000}
}

func initializeAISettings(db *gorm.DB, cfg Config) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var existing AISettings
		if result := tx.Find(&existing, 1); result.Error == nil && result.RowsAffected > 0 {
			return nil
		} else if result.Error != nil {
			return result.Error
		}
		settings := defaultAISettings()
		if cfg.AIMaxConcurrency > 0 {
			settings.DefaultClassConcurrency = cfg.AIMaxConcurrency
		}
		if cfg.AIRequestsPerStudent > 0 {
			settings.DefaultStudentRequests = cfg.AIRequestsPerStudent
		}
		if cfg.AIHistoryMessages > 0 {
			settings.HistoryMessages = cfg.AIHistoryMessages
		}
		if cfg.AIHistoryChars > 0 {
			settings.HistoryChars = cfg.AIHistoryChars
		}
		if err := tx.Create(&settings).Error; err != nil {
			return err
		}
		if err := tx.Model(&Class{}).Where("ai_concurrency <= 0").Update("ai_concurrency", settings.DefaultClassConcurrency).Error; err != nil {
			return err
		}
		if strings.TrimSpace(cfg.AIAPIKey) == "" || strings.TrimSpace(cfg.AIModel) == "" {
			return nil
		}
		provider := AIProvider{
			Name: "原有模型服务", BaseURL: normalizeAIBaseURL(cfg.AIBaseURL), APIKey: cfg.AIAPIKey,
			Model: cfg.AIModel, Enabled: true, ConfigVersion: 1, MaxConcurrency: 6, TimeoutSeconds: 600,
			MaxOutputTokens: 393216, ModificationMaxTokens: 393216, ReasoningEffort: "low",
		}
		if cfg.AITimeout > 0 {
			provider.TimeoutSeconds = int(cfg.AITimeout.Seconds())
		}
		if cfg.AIMaxConcurrency > 0 {
			provider.MaxConcurrency = cfg.AIMaxConcurrency
		}
		if cfg.AIMaxOutputTokens > 0 {
			provider.MaxOutputTokens = cfg.AIMaxOutputTokens
		}
		if cfg.AIModificationTokens > 0 {
			provider.ModificationMaxTokens = cfg.AIModificationTokens
		}
		if cfg.AIReasoningEffort != "" {
			provider.ReasoningEffort = cfg.AIReasoningEffort
		}
		if provider.ModificationMaxTokens > provider.MaxOutputTokens {
			provider.ModificationMaxTokens = provider.MaxOutputTokens
		}
		return tx.Create(&provider).Error
	})
}

func (a *App) aiSettings() AISettings {
	var settings AISettings
	if result := a.DB.Find(&settings, 1); result.Error != nil || result.RowsAffected == 0 {
		return defaultAISettings()
	}
	return settings
}

func (a *App) AIConfigured() bool {
	var count int64
	if a.DB.Model(&AIProvider{}).Where("enabled = ? AND api_key <> '' AND model <> ''", true).Count(&count).Error != nil {
		return false
	}
	return count > 0
}

func (a *App) handleAISettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"settings": a.aiSettings()})
}

func (a *App) handleUpdateAISettings(c *gin.Context) {
	var input struct {
		DefaultClassConcurrency *int `json:"default_class_concurrency"`
		DefaultStudentRequests  *int `json:"default_student_requests"`
		HistoryMessages         *int `json:"history_messages"`
		HistoryChars            *int `json:"history_chars"`
	}
	if !bindJSON(c, &input) {
		return
	}
	settings := a.aiSettings()
	if input.DefaultClassConcurrency != nil {
		settings.DefaultClassConcurrency = *input.DefaultClassConcurrency
	}
	if input.DefaultStudentRequests != nil {
		settings.DefaultStudentRequests = *input.DefaultStudentRequests
	}
	if input.HistoryMessages != nil {
		settings.HistoryMessages = *input.HistoryMessages
	}
	if input.HistoryChars != nil {
		settings.HistoryChars = *input.HistoryChars
	}
	if settings.DefaultClassConcurrency < 1 || settings.DefaultClassConcurrency > 100 || settings.DefaultStudentRequests < 1 || settings.DefaultStudentRequests > 1000 || settings.HistoryMessages < 1 || settings.HistoryMessages > 100 || settings.HistoryChars < 100 || settings.HistoryChars > 1000000 {
		jsonError(c, http.StatusBadRequest, "AI 全局设置超出允许范围")
		return
	}
	if err := a.DB.Save(&settings).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "保存失败")
		return
	}
	auth := authFrom(c)
	a.audit(&auth.User, nil, "ai_settings", "update", "success", "更新全局 AI 默认设置")
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

type aiProviderInput struct {
	Name                  *string `json:"name"`
	BaseURL               *string `json:"base_url"`
	APIKey                string  `json:"api_key"`
	Model                 *string `json:"model"`
	Enabled               *bool   `json:"enabled"`
	MaxConcurrency        *int    `json:"max_concurrency"`
	TimeoutSeconds        *int    `json:"timeout_seconds"`
	MaxOutputTokens       *int    `json:"max_output_tokens"`
	ModificationMaxTokens *int    `json:"modification_max_tokens"`
	ReasoningEffort       *string `json:"reasoning_effort"`
}

func applyProviderInput(provider *AIProvider, input aiProviderInput) error {
	if input.Name != nil {
		provider.Name = strings.TrimSpace(*input.Name)
	}
	if input.BaseURL != nil {
		provider.BaseURL = normalizeAIBaseURL(*input.BaseURL)
	}
	if input.APIKey != "" {
		provider.APIKey = strings.TrimSpace(input.APIKey)
	}
	if input.Model != nil {
		provider.Model = strings.TrimSpace(*input.Model)
	}
	if input.Enabled != nil {
		provider.Enabled = *input.Enabled
	}
	if input.MaxConcurrency != nil {
		provider.MaxConcurrency = *input.MaxConcurrency
	}
	if input.TimeoutSeconds != nil {
		provider.TimeoutSeconds = *input.TimeoutSeconds
	}
	if input.MaxOutputTokens != nil {
		provider.MaxOutputTokens = *input.MaxOutputTokens
	}
	if input.ModificationMaxTokens != nil {
		provider.ModificationMaxTokens = *input.ModificationMaxTokens
	}
	if input.ReasoningEffort != nil {
		provider.ReasoningEffort = strings.ToLower(strings.TrimSpace(*input.ReasoningEffort))
	}
	parsed, err := url.Parse(provider.BaseURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		provider.Name == "" || len([]rune(provider.Name)) > 120 || provider.APIKey == "" || provider.Model == "" || len(provider.Model) > 120 ||
		provider.MaxConcurrency < 1 || provider.MaxConcurrency > 100 || provider.TimeoutSeconds < 1 || provider.TimeoutSeconds > 3600 ||
		provider.MaxOutputTokens < 1 || provider.MaxOutputTokens > 1000000 || provider.ModificationMaxTokens < 1 || provider.ModificationMaxTokens > provider.MaxOutputTokens {
		return errors.New("服务配置不完整或超出允许范围")
	}
	switch provider.ReasoningEffort {
	case "none", "low", "high", "max", "auto":
	default:
		return errors.New("思考强度不支持")
	}
	return nil
}

func (a *App) handleAIProviders(c *gin.Context) {
	var providers []AIProvider
	if err := a.DB.Order("id ASC").Find(&providers).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "加载失败")
		return
	}
	views := make([]gin.H, 0, len(providers))
	for _, provider := range providers {
		views = append(views, a.providerView(provider))
	}
	c.JSON(http.StatusOK, gin.H{"providers": views})
}

func (a *App) handleCreateAIProvider(c *gin.Context) {
	var input aiProviderInput
	if !bindJSON(c, &input) {
		return
	}
	provider := AIProvider{Enabled: true, ConfigVersion: 1, MaxConcurrency: 6, TimeoutSeconds: 600, MaxOutputTokens: 393216, ModificationMaxTokens: 393216, ReasoningEffort: "auto"}
	if err := applyProviderInput(&provider, input); err != nil {
		jsonError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.DB.Create(&provider).Error; err != nil {
		jsonError(c, http.StatusInternalServerError, "创建失败")
		return
	}
	a.signalProviders()
	auth := authFrom(c)
	a.audit(&auth.User, nil, "ai_provider", "create", "success", provider.Name)
	c.JSON(http.StatusCreated, gin.H{"provider": a.providerView(provider)})
}

func (a *App) handleUpdateAIProvider(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var provider AIProvider
	if a.DB.First(&provider, id).Error != nil {
		jsonError(c, http.StatusNotFound, "服务不存在")
		return
	}
	var input aiProviderInput
	if !bindJSON(c, &input) {
		return
	}
	if err := applyProviderInput(&provider, input); err != nil {
		jsonError(c, http.StatusBadRequest, err.Error())
		return
	}
	previousVersion := provider.ConfigVersion
	provider.ConfigVersion++
	provider.AuthFailed = false
	provider.CooldownUntil = nil
	result := a.DB.Model(&AIProvider{}).Where("id = ? AND config_version = ?", id, previousVersion).Updates(map[string]any{
		"name": provider.Name, "base_url": provider.BaseURL, "api_key": provider.APIKey, "model": provider.Model,
		"enabled": provider.Enabled, "max_concurrency": provider.MaxConcurrency, "timeout_seconds": provider.TimeoutSeconds,
		"max_output_tokens": provider.MaxOutputTokens, "modification_max_tokens": provider.ModificationMaxTokens,
		"reasoning_effort": provider.ReasoningEffort, "config_version": provider.ConfigVersion,
		"auth_failed": false, "cooldown_until": nil,
	})
	if result.Error != nil {
		jsonError(c, http.StatusInternalServerError, "保存失败")
		return
	}
	if result.RowsAffected == 0 {
		jsonError(c, http.StatusConflict, "配置已被其他教师修改，请刷新后重试")
		return
	}
	a.signalProviders()
	auth := authFrom(c)
	a.audit(&auth.User, nil, "ai_provider", "update", "success", provider.Name)
	c.JSON(http.StatusOK, gin.H{"provider": a.providerView(provider)})
}

func (a *App) handleTestAIProvider(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var provider AIProvider
	if a.DB.First(&provider, id).Error != nil {
		jsonError(c, http.StatusNotFound, "服务不存在")
		return
	}
	started := time.Now()
	_, err := a.aiClientForProvider(provider).StreamWithOptionsOneAttempt(c.Request.Context(),
		[]ChatMessage{{Role: "system", Content: "请只回复：连接正常"}, {Role: "user", Content: "连接测试"}},
		AIRequestOptions{MaxOutputTokens: 16, ReasoningEffort: "none"}, func(string) error { return nil })
	if err != nil {
		a.markProviderFailure(provider, err)
		auth := authFrom(c)
		a.audit(&auth.User, nil, "ai_provider", "test", "failed", provider.Name)
		jsonError(c, http.StatusBadGateway, err.Error())
		return
	}
	a.clearProviderError(provider)
	auth := authFrom(c)
	a.audit(&auth.User, nil, "ai_provider", "test", "success", provider.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true, "duration_ms": time.Since(started).Milliseconds()})
}

func providerClientConfig(provider AIProvider) Config {
	return Config{AIBaseURL: provider.BaseURL, AIAPIKey: provider.APIKey, AIModel: provider.Model,
		AITimeout: time.Duration(provider.TimeoutSeconds) * time.Second, AIMaxOutputTokens: provider.MaxOutputTokens,
		AIReasoningEffort: provider.ReasoningEffort}
}
