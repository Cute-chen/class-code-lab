package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

var errNoAIProvider = errors.New("当前没有可用的模型服务，请联系教师")

func (a *App) signalProvidersLocked() {
	close(a.providerNotify)
	a.providerNotify = make(chan struct{})
}

func (a *App) signalProviders() {
	a.providerMu.Lock()
	a.signalProvidersLocked()
	a.providerMu.Unlock()
}

func (a *App) clearProviderError(provider AIProvider) {
	a.DB.Model(&AIProvider{}).Where("id = ? AND config_version = ?", provider.ID, provider.ConfigVersion).
		UpdateColumns(map[string]any{"auth_failed": false, "cooldown_until": nil})
	a.signalProviders()
}

func (a *App) markProviderFailure(provider AIProvider, err error) {
	var upstream *AIUpstreamError
	if !errors.As(err, &upstream) {
		return
	}
	updates := map[string]any{}
	switch upstream.Category {
	case "authentication":
		updates["auth_failed"] = true
	case "rate_limit", "temporary", "network", "timeout", "upstream":
		updates["cooldown_until"] = time.Now().Add(60 * time.Second)
	}
	if len(updates) == 0 {
		return
	}
	a.DB.Model(&AIProvider{}).Where("id = ? AND config_version = ?", provider.ID, provider.ConfigVersion).UpdateColumns(updates)
	a.signalProviders()
}

func retryOnOtherProvider(err error) bool {
	var upstream *AIUpstreamError
	if !errors.As(err, &upstream) {
		return false
	}
	switch upstream.Category {
	case "authentication", "rate_limit", "temporary", "network", "timeout", "upstream":
		return true
	default:
		return false
	}
}

func (a *App) providerView(provider AIProvider) gin.H {
	a.providerMu.Lock()
	active := a.providerActive[provider.ID]
	a.providerMu.Unlock()
	cooldown := time.Time{}
	if provider.CooldownUntil != nil {
		cooldown = *provider.CooldownUntil
	}
	status := "ready"
	if !provider.Enabled {
		status = "disabled"
	} else if provider.AuthFailed {
		status = "authentication"
	} else if cooldown.After(time.Now()) {
		status = "cooldown"
	} else if active >= provider.MaxConcurrency {
		status = "full"
	}
	var total, failed int64
	a.DB.Model(&AIUsageLog{}).Where("provider_id = ?", provider.ID).Count(&total)
	a.DB.Model(&AIUsageLog{}).Where("provider_id = ? AND status = ?", provider.ID, "failed").Count(&failed)
	return gin.H{"id": provider.ID, "name": provider.Name, "base_url": provider.BaseURL, "model": provider.Model,
		"api_key_present": provider.APIKey != "", "enabled": provider.Enabled, "max_concurrency": provider.MaxConcurrency,
		"timeout_seconds": provider.TimeoutSeconds, "max_output_tokens": provider.MaxOutputTokens,
		"modification_max_tokens": provider.ModificationMaxTokens, "reasoning_effort": provider.ReasoningEffort,
		"active_requests": active, "status": status, "cooldown_until": cooldown, "total_requests": total, "failed_requests": failed}
}

// acquireProvider reserves one provider slot. It rereads the database, so edits
// apply to the next request without restarting the server. In-flight requests
// keep their selected configuration snapshot.
func (a *App) acquireProvider(ctx context.Context, excluded map[uint]bool, onQueue func()) (AIProvider, func(), error) {
	queued := false
	for {
		var providers []AIProvider
		if err := a.DB.Where("enabled = ? AND api_key <> '' AND model <> ''", true).Order("id ASC").Find(&providers).Error; err != nil {
			return AIProvider{}, nil, err
		}
		a.providerMu.Lock()
		best := make([]AIProvider, 0)
		available := false
		for _, provider := range providers {
			if excluded[provider.ID] || provider.AuthFailed || (provider.CooldownUntil != nil && provider.CooldownUntil.After(time.Now())) {
				continue
			}
			available = true
			if provider.MaxConcurrency < 1 || a.providerActive[provider.ID] >= provider.MaxConcurrency {
				continue
			}
			if len(best) == 0 {
				best = append(best, provider)
				continue
			}
			left := a.providerActive[provider.ID] * best[0].MaxConcurrency
			right := a.providerActive[best[0].ID] * provider.MaxConcurrency
			if left < right {
				best = []AIProvider{provider}
			} else if left == right {
				best = append(best, provider)
			}
		}
		if len(best) > 0 {
			selected := best[0]
			for _, provider := range best {
				if provider.ID > a.providerCursor {
					selected = provider
					break
				}
			}
			a.providerCursor = selected.ID
			a.providerActive[selected.ID]++
			a.providerMu.Unlock()
			return selected, func() {
				a.providerMu.Lock()
				a.providerActive[selected.ID]--
				a.signalProvidersLocked()
				a.providerMu.Unlock()
			}, nil
		}
		if !available {
			a.providerMu.Unlock()
			return AIProvider{}, nil, errNoAIProvider
		}
		wake := a.providerNotify
		a.providerMu.Unlock()
		if !queued {
			onQueue()
			queued = true
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return AIProvider{}, nil, ctx.Err()
		}
	}
}

func (a *App) aiClientForProvider(provider AIProvider) *AIClient {
	a.providerClientsMu.Lock()
	defer a.providerClientsMu.Unlock()
	if entry, ok := a.providerClients[provider.ID]; ok && entry.version == provider.ConfigVersion {
		return entry.client
	}
	client := NewAIClient(providerClientConfig(provider))
	a.providerClients[provider.ID] = providerClientEntry{version: provider.ConfigVersion, client: client}
	return client
}

func (a *App) handleLegacyTeacherTestAI(c *gin.Context) {
	provider, release, err := a.acquireProvider(c.Request.Context(), map[uint]bool{}, func() {})
	if err != nil {
		jsonError(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer release()
	started := time.Now()
	_, err = a.aiClientForProvider(provider).StreamWithOptionsOneAttempt(c.Request.Context(),
		[]ChatMessage{{Role: "system", Content: "请只回复：连接正常"}, {Role: "user", Content: "连接测试"}},
		AIRequestOptions{MaxOutputTokens: 16, ReasoningEffort: "none"}, func(string) error { return nil })
	if err != nil {
		jsonError(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "duration_ms": time.Since(started).Milliseconds()})
}
