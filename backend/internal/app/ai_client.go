package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AIResult struct {
	Content               string
	PromptTokens          *int
	CompletionTokens      *int
	PromptCacheHitTokens  *int
	PromptCacheMissTokens *int
	Model                 string
	FinishReason          string
}

type AIRequestOptions struct {
	MaxOutputTokens int
	ReasoningEffort string
}

type aiUsage struct {
	PromptTokens          int  `json:"prompt_tokens"`
	CompletionTokens      int  `json:"completion_tokens"`
	PromptCacheHitTokens  *int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens *int `json:"prompt_cache_miss_tokens"`
}

type AIUpstreamError struct {
	StatusCode int
	Category   string
	Message    string
}

func (e *AIUpstreamError) Error() string { return e.Message }

type AIClient struct {
	cfg    Config
	client *http.Client
}

func NewAIClient(cfg Config) *AIClient {
	return &AIClient{cfg: cfg, client: &http.Client{Timeout: cfg.AITimeout}}
}

func (c *AIClient) Configured() bool {
	return c.cfg.AIAPIKey != "" && c.cfg.AIModel != ""
}

func (c *AIClient) Stream(ctx context.Context, messages []ChatMessage, onDelta func(string) error) (AIResult, error) {
	return c.StreamWithOptions(ctx, messages, AIRequestOptions{}, onDelta)
}

func (c *AIClient) StreamWithOptions(ctx context.Context, messages []ChatMessage, options AIRequestOptions, onDelta func(string) error) (AIResult, error) {
	if !c.Configured() {
		return AIResult{}, &AIUpstreamError{StatusCode: http.StatusServiceUnavailable, Category: "not_configured", Message: "模型服务未配置"}
	}
	maxOutputTokens := options.MaxOutputTokens
	if maxOutputTokens <= 0 {
		maxOutputTokens = c.cfg.AIMaxOutputTokens
	}
	reasoningEffort := normalizeReasoningEffort(options.ReasoningEffort)
	if options.ReasoningEffort == "" {
		reasoningEffort = c.reasoningEffort()
	}
	requestBody := map[string]any{
		"model":          c.cfg.AIModel,
		"messages":       messages,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if c.usesDeepSeekAPI() {
		requestBody["max_tokens"] = maxOutputTokens
		requestBody["reasoning_effort"] = reasoningEffort
	} else {
		requestBody["max_completion_tokens"] = maxOutputTokens
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return AIResult{}, err
	}
	var lastErr error
	var lastResult AIResult
	for attempt := 0; attempt < 2; attempt++ {
		result, retry, err := c.doRequest(ctx, payload, onDelta)
		if err == nil {
			if result.FinishReason == "length" {
				return result, &AIUpstreamError{Category: "output_truncated", Message: "AI 回答超出输出上限，未保存不完整代码。请简化需求后重试"}
			}
			return result, nil
		}
		lastResult = result
		lastErr = err
		if !retry || attempt == 1 {
			break
		}
		select {
		case <-ctx.Done():
			return lastResult, ctx.Err()
		case <-time.After(600 * time.Millisecond):
		}
	}
	return lastResult, lastErr
}

func (c *AIClient) usesDeepSeekAPI() bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.cfg.AIModel)), "deepseek-") {
		return true
	}
	parsed, err := url.Parse(c.cfg.AIBaseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "deepseek.com" || strings.HasSuffix(host, ".deepseek.com")
}

func (c *AIClient) reasoningEffort() string {
	return normalizeReasoningEffort(c.cfg.AIReasoningEffort)
}

func normalizeReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "high", "max":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "none"
	}
}

func (c *AIClient) doRequest(ctx context.Context, payload []byte, onDelta func(string) error) (AIResult, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.AIBaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return AIResult{}, false, err
	}
	request.Header.Set("Authorization", "Bearer "+c.cfg.AIAPIKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream, application/json")
	response, err := c.client.Do(request)
	if err != nil {
		category := "network"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			category = "timeout"
		}
		return AIResult{}, false, &AIUpstreamError{Category: category, Message: "模型服务暂时不可用，请稍后重试"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		category := "upstream"
		message := "模型服务请求失败"
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			category, message = "authentication", "模型服务鉴权失败，请联系教师"
		case http.StatusTooManyRequests:
			category, message = "rate_limit", "模型服务繁忙，请稍后重试"
		default:
			if response.StatusCode >= 500 {
				category, message = "temporary", "模型服务暂时不可用，请稍后重试"
			}
		}
		var upstream struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &upstream) == nil && upstream.Error.Message != "" && response.StatusCode < 500 {
			message += "：" + upstream.Error.Message
		}
		retry := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return AIResult{}, retry, &AIUpstreamError{StatusCode: response.StatusCode, Category: category, Message: message}
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		result, err := parseJSONCompletion(response.Body)
		if err == nil && result.Content != "" {
			if deltaErr := onDelta(result.Content); deltaErr != nil {
				return AIResult{}, false, deltaErr
			}
		}
		return result, false, err
	}
	result, err := parseSSECompletion(response.Body, onDelta)
	return result, false, err
}

func parseJSONCompletion(reader io.Reader) (AIResult, error) {
	var response struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *aiUsage `json:"usage"`
	}
	if err := json.NewDecoder(reader).Decode(&response); err != nil {
		return AIResult{}, fmt.Errorf("parse model response: %w", err)
	}
	if len(response.Choices) == 0 {
		return AIResult{}, errors.New("模型没有返回内容")
	}
	result := AIResult{Content: response.Choices[0].Message.Content, Model: response.Model, FinishReason: response.Choices[0].FinishReason}
	if response.Usage != nil {
		result.PromptTokens = &response.Usage.PromptTokens
		result.CompletionTokens = &response.Usage.CompletionTokens
		result.PromptCacheHitTokens = response.Usage.PromptCacheHitTokens
		result.PromptCacheMissTokens = response.Usage.PromptCacheMissTokens
	}
	return result, nil
}

func parseSSECompletion(reader io.Reader, onDelta func(string) error) (AIResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var result AIResult
	var content strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *aiUsage `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			result.Content = content.String()
			return result, &AIUpstreamError{Category: "upstream", Message: "模型服务返回错误：" + chunk.Error.Message}
		}
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
		if chunk.Usage != nil {
			result.PromptTokens = &chunk.Usage.PromptTokens
			result.CompletionTokens = &chunk.Usage.CompletionTokens
			result.PromptCacheHitTokens = chunk.Usage.PromptCacheHitTokens
			result.PromptCacheMissTokens = chunk.Usage.PromptCacheMissTokens
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil {
				result.FinishReason = *choice.FinishReason
			}
			if choice.Delta.Content == "" {
				continue
			}
			content.WriteString(choice.Delta.Content)
			if err := onDelta(choice.Delta.Content); err != nil {
				result.Content = content.String()
				return result, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		result.Content = content.String()
		return result, err
	}
	result.Content = content.String()
	if result.Content == "" {
		return AIResult{}, errors.New("模型没有返回内容")
	}
	return result, nil
}

var htmlBlockPattern = regexp.MustCompile("(?is)```(?:html)?\\s*\\n?(.*?)```")

func extractLargestHTMLBlock(content string) string {
	matches := htmlBlockPattern.FindAllStringSubmatch(content, -1)
	var largest string
	for _, match := range matches {
		if len(match) > 1 && len(match[1]) > len(largest) && strings.Contains(strings.ToLower(match[1]), "<html") {
			largest = strings.TrimSpace(match[1])
		}
	}
	return largest
}

func studentSystemPrompt() string {
	return `你是面向中学生的中文编程助教。回答要简短、友好，避免堆砌术语。想法不清楚时最多提出两个简单问题。
首次生成作品时，返回一份完整的单文件 HTML，并放在一个 html 代码块中。
修改已有作品时，优先只返回一个 json 代码块，格式为：{"type":"patch","summary":"修改说明","replacements":[{"old":"原代码中唯一存在的完整文本","new":"替换后的文本"}]}。old 必须与当前代码完全一致且只出现一次，不要使用行号、省略号或正则表达式。可以提供多个 replacements。
只有无法用少量精确替换完成修改时，才回退为完整 html 代码块。不需要改代码时直接简短回答，不要返回 patch。
禁止生成支付、账号收集、密码输入、文件上传、个人信息采集、页面跳转、弹窗、下载、摄像头、麦克风、任意网络请求、iframe、Worker、WebAssembly、eval 或 new Function。
只允许使用带明确版本号的 https://cdn.jsdelivr.net 或 https://cdnjs.cloudflare.com 资源。游戏应有明确玩法、开始方式和重新开始方式。`
}

func currentCodeSystemPrompt(currentCode string) string {
	if strings.TrimSpace(currentCode) == "" {
		return ""
	}
	return "当前作品代码如下。修改时优先返回系统约定的 patch JSON，只有无法安全增量修改时才返回完整 HTML：\n```html\n" + currentCode + "\n```"
}

func compactAIHistoryContent(content string) string {
	const replacement = "\n[完整 HTML 代码已作为代码提案保存，本轮上下文不重复携带。]\n"
	compacted := htmlBlockPattern.ReplaceAllStringFunc(content, func(block string) string {
		if extractLargestHTMLBlock(block) != "" {
			return replacement
		}
		return block
	})
	return strings.TrimSpace(compacted)
}
