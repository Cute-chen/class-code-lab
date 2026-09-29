package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	aiRequestModeGenerate      = "generate_full"
	aiRequestModeConversation  = "conversation_start"
	aiRequestModeModifyPatch   = "modify_patch"
	aiRequestModeModifyComplex = "modify_complex"
	aiRequestModeRetryFull     = "retry_full"
)

var complexAIRequestKeywords = []string{
	"报错", "错误", "修复", "调试", "卡顿", "性能", "逻辑", "算法", "碰撞", "关卡", "bug", "debug",
}

// simpleAIRequestKeywords identifies changes that usually only need a quick
// patch. Auto mode keeps these requests fast while reserving deeper reasoning
// for requests that can change behavior or require debugging.
var simpleAIRequestKeywords = []string{
	"颜色", "色彩", "背景", "字体", "字号", "文字", "文本", "标题", "文案", "样式", "外观",
	"间距", "边距", "圆角", "阴影", "透明度", "大小", "宽度", "高度", "布局", "对齐", "居中",
	"换成", "改成", "改为", "替换", "美化", "好看", "配色",
}

func (a *App) handleAIHistory(c *gin.Context) {
	auth := authFrom(c)
	var conversations []AIConversation
	err := a.DB.Preload("Messages", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).
		Where("user_id = ?", auth.User.ID).Order("id DESC").Limit(10).Find(&conversations).Error
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "对话记录加载失败")
		return
	}
	ids := make([]uint, 0)
	for _, conversation := range conversations {
		for _, message := range conversation.Messages {
			ids = append(ids, message.ID)
		}
	}
	var proposals []AICodeProposal
	if len(ids) > 0 {
		a.DB.Where("message_id IN ?", ids).Find(&proposals)
	}
	c.JSON(http.StatusOK, gin.H{
		"conversations": conversations, "proposals": proposals,
		"remaining":  a.studentAIRemaining(auth.User, *auth.User.ClassID),
		"configured": a.AIConfigured(),
	})
}

func (a *App) handleAIMessage(c *gin.Context) {
	var request struct {
		ConversationID uint   `json:"conversation_id"`
		Message        string `json:"message"`
		RetryMessageID uint   `json:"retry_message_id"`
		ForceFull      bool   `json:"force_full"`
	}
	if !bindJSON(c, &request) {
		return
	}
	request.Message = strings.TrimSpace(request.Message)
	isRetry := request.RetryMessageID != 0
	if (!isRetry && request.Message == "") || len([]rune(request.Message)) > 4000 {
		jsonError(c, http.StatusBadRequest, "请输入 1-4000 字的需求")
		return
	}
	auth := authFrom(c)
	if auth.User.ClassID == nil || auth.User.Class == nil {
		jsonError(c, http.StatusBadRequest, "当前账号没有班级")
		return
	}
	class := *auth.User.Class
	if !class.AIEnabled {
		jsonError(c, http.StatusForbidden, "教师暂时关闭了本班 AI 助手")
		return
	}
	if auth.User.AIBlocked {
		jsonError(c, http.StatusForbidden, "你的 AI 助手当前已暂停，请联系教师")
		return
	}
	if !a.AIConfigured() {
		jsonError(c, http.StatusServiceUnavailable, "模型服务未配置，你仍可手动编辑和发布作品")
		return
	}
	if a.studentAIRemaining(auth.User, class.ID) <= 0 {
		jsonError(c, http.StatusTooManyRequests, "本节 AI 次数已用完，你仍可手动修改作品")
		return
	}
	if !isRetry && class.AIRequestCooldownSeconds > 0 {
		var latest AIUsageLog
		if a.DB.Where("user_id = ?", auth.User.ID).Order("id DESC").First(&latest).Error == nil && time.Since(latest.CreatedAt) < time.Duration(class.AIRequestCooldownSeconds)*time.Second {
			jsonError(c, http.StatusTooManyRequests, "发送太快了，请稍等几秒")
			return
		}
	}
	releaseStudent, ok := a.acquireStudentAI(auth.User.ID, c.Request.Context())
	if !ok {
		jsonError(c, http.StatusConflict, "你已有一个 AI 请求正在进行")
		return
	}
	defer releaseStudent()

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeSSE(c, "error", gin.H{"message": "当前服务器不支持流式回答"})
		return
	}
	releaseClass, acquired, queued := a.acquireClassAIWait(c.Request.Context(), class.ID, class.AIConcurrency)
	if queued {
		writeSSE(c, "queue", gin.H{"position": 1, "message": "模型繁忙，已进入等待队列"})
		flusher.Flush()
	}
	if !acquired {
		return
	}
	defer releaseClass()

	writeSSE(c, "status", gin.H{"message": "正在准备模型请求"})
	flusher.Flush()
	conversation, requestMessage, err := a.prepareAIConversation(auth.User, request.ConversationID, request.RetryMessageID, request.Message)
	if err != nil {
		writeSSE(c, "error", gin.H{"message": err.Error()})
		return
	}
	work, _ := a.getOrCreateWork(auth.User)
	forceFull := request.ForceFull || isRetry
	messages := a.buildAIMessagesForRequest(conversation.ID, work.DraftCode, requestMessage, request.ConversationID == 0, forceFull)
	if forceFull {
		messages = append(messages, ChatMessage{Role: "system", Content: "本次是增量修改失败后的完整代码重试。禁止返回 patch JSON；必须根据用户最近一条需求返回完整、可直接运行的单文件 HTML，放在 html 代码块中。"})
	}
	var result AIResult
	var usage AIUsageLog
	var requestOptions AIRequestOptions
	var requestMode string
	var duration int64
	excluded := map[uint]bool{}
	lastFailureMessage := ""
	for {
		provider, release, acquireErr := a.acquireProvider(c.Request.Context(), excluded, func() {
			writeSSE(c, "queue", gin.H{"position": 1, "message": "模型服务繁忙，已进入等待队列"})
			flusher.Flush()
		})
		if acquireErr != nil {
			if c.Request.Context().Err() == nil {
				message := acquireErr.Error()
				if lastFailureMessage != "" {
					message = lastFailureMessage
				}
				writeSSE(c, "error", gin.H{"message": message})
				flusher.Flush()
			}
			return
		}
		excluded[provider.ID] = true
		writeSSE(c, "status", gin.H{"message": "正在连接 AI 服务"})
		flusher.Flush()
		requestOptions, requestMode = aiRequestOptionsForProvider(provider, work.DraftCode, requestMessage, request.ConversationID == 0, forceFull)
		usage = AIUsageLog{UserID: auth.User.ID, ClassID: class.ID, ConversationID: conversation.ID,
			ProviderID: &provider.ID, ProviderName: provider.Name, Model: provider.Model, Status: "pending",
			RequestMode: requestMode, ReasoningEffort: requestOptions.ReasoningEffort, MaxOutputTokens: requestOptions.MaxOutputTokens}
		if err := a.DB.Create(&usage).Error; err != nil {
			release()
			writeSSE(c, "error", gin.H{"message": "用量记录保存失败"})
			return
		}
		start := time.Now()
		emitted := false
		var streamErr error
		writeSSE(c, "status", gin.H{"message": "已连接模型，等待首个回答"})
		flusher.Flush()
		result, streamErr = a.aiClientForProvider(provider).StreamWithOptionsOneAttempt(c.Request.Context(), messages, requestOptions, func(delta string) error {
			emitted = true
			writeSSE(c, "delta", gin.H{"delta": delta})
			flusher.Flush()
			return nil
		})
		duration = time.Since(start).Milliseconds()
		release()
		if streamErr == nil {
			if result.Model == "" {
				result.Model = provider.Model
			}
			break
		}
		category := "upstream"
		message := "模型回答失败，请稍后重试"
		if upstream, ok := streamErr.(*AIUpstreamError); ok {
			category, message = upstream.Category, upstream.Message
		}
		lastFailureMessage = message
		failedModel := result.Model
		if failedModel == "" {
			failedModel = provider.Model
		}
		a.DB.Model(&usage).Updates(map[string]any{"status": "failed", "error_category": category,
			"duration_ms": duration, "model": failedModel, "prompt_tokens": result.PromptTokens,
			"completion_tokens": result.CompletionTokens, "prompt_cache_hit_tokens": result.PromptCacheHitTokens,
			"prompt_cache_miss_tokens": result.PromptCacheMissTokens, "finish_reason": result.FinishReason})
		a.markProviderFailure(provider, streamErr)
		if emitted || c.Request.Context().Err() != nil || !retryOnOtherProvider(streamErr) {
			if c.Request.Context().Err() == nil {
				writeSSE(c, "error", gin.H{"message": message})
				flusher.Flush()
			}
			return
		}
	}

	assistant := AIMessage{
		ConversationID: conversation.ID, Role: "assistant", Content: result.Content, Status: "success",
		Model: result.Model, LatencyMS: duration, PromptTokens: result.PromptTokens, CompletionTokens: result.CompletionTokens,
		PromptCacheHitTokens: result.PromptCacheHitTokens, PromptCacheMissTokens: result.PromptCacheMissTokens,
		FinishReason: result.FinishReason, RequestMode: requestMode, ReasoningEffort: requestOptions.ReasoningEffort,
		MaxOutputTokens: requestOptions.MaxOutputTokens,
	}
	if err := a.DB.Create(&assistant).Error; err != nil {
		writeSSE(c, "error", gin.H{"message": "回答已收到，但保存失败"})
		return
	}
	a.DB.Model(&usage).Updates(map[string]any{
		"status": "success", "duration_ms": duration, "prompt_tokens": result.PromptTokens,
		"completion_tokens": result.CompletionTokens, "prompt_cache_hit_tokens": result.PromptCacheHitTokens,
		"prompt_cache_miss_tokens": result.PromptCacheMissTokens, "model": result.Model, "finish_reason": result.FinishReason,
	})

	var proposal *AICodeProposal
	proposalWarning := ""
	if code, summary, format, warning := resolveAIProposal(result.Content, work.DraftCode); code != "" {
		safety := CheckHTMLSafety(code)
		created := AICodeProposal{
			MessageID: assistant.ID, ConversationID: conversation.ID, Code: code,
			Summary: summary, Format: format,
			SuggestedTitle: work.Title, SafetyOK: safety.OK, SafetyIssues: sortedSafetyText(safety.Issues),
		}
		if a.DB.Create(&created).Error == nil {
			proposal = &created
		}
	} else {
		proposalWarning = warning
	}
	if proposalWarning != "" {
		a.DB.Model(&assistant).Update("status", "proposal_invalid")
		a.DB.Model(&usage).Updates(map[string]any{"status": "failed", "error_category": "patch_invalid"})
		assistant.Status = "proposal_invalid"
	}
	writeSSE(c, "done", gin.H{
		"message_id": assistant.ID, "conversation_id": conversation.ID, "proposal": proposal,
		"proposal_warning": proposalWarning, "retry_available": proposalWarning != "", "request_mode": requestMode,
		"remaining": a.studentAIRemaining(auth.User, class.ID),
	})
	flusher.Flush()
}

func aiRequestOptionsForProvider(provider AIProvider, currentCode, message string, isNewConversation, forceFull bool) (AIRequestOptions, string) {
	fullOutputLimit := provider.MaxOutputTokens
	if fullOutputLimit <= 0 {
		fullOutputLimit = 393216
	}
	if strings.TrimSpace(currentCode) == "" {
		return AIRequestOptions{MaxOutputTokens: fullOutputLimit, ReasoningEffort: reasoningEffortForRequest(provider, currentCode, message, isNewConversation, forceFull)}, aiRequestModeGenerate
	}
	if forceFull {
		return AIRequestOptions{MaxOutputTokens: fullOutputLimit, ReasoningEffort: reasoningEffortForRequest(provider, currentCode, message, isNewConversation, forceFull)}, aiRequestModeRetryFull
	}
	if isNewConversation {
		return AIRequestOptions{MaxOutputTokens: fullOutputLimit, ReasoningEffort: reasoningEffortForRequest(provider, currentCode, message, isNewConversation, forceFull)}, aiRequestModeConversation
	}
	modificationLimit := provider.ModificationMaxTokens
	if modificationLimit <= 0 {
		modificationLimit = 393216
	}
	if modificationLimit > fullOutputLimit {
		modificationLimit = fullOutputLimit
	}
	lowerMessage := strings.ToLower(message)
	for _, keyword := range complexAIRequestKeywords {
		if strings.Contains(lowerMessage, keyword) {
			return AIRequestOptions{MaxOutputTokens: fullOutputLimit, ReasoningEffort: reasoningEffortForRequest(provider, currentCode, message, isNewConversation, forceFull)}, aiRequestModeModifyComplex
		}
	}
	return AIRequestOptions{MaxOutputTokens: modificationLimit, ReasoningEffort: reasoningEffortForRequest(provider, currentCode, message, isNewConversation, forceFull)}, aiRequestModeModifyPatch
}

// reasoningEffortForRequest resolves the provider's stored policy into the
// concrete value sent to the upstream API. Fixed modes remain unchanged;
// auto mode uses the request shape to balance latency and answer quality.
func reasoningEffortForRequest(provider AIProvider, currentCode, message string, isNewConversation, forceFull bool) string {
	configured := strings.ToLower(strings.TrimSpace(provider.ReasoningEffort))
	if configured != "auto" {
		return normalizeReasoningEffort(configured)
	}

	if forceFull || containsAIKeyword(message, complexAIRequestKeywords) {
		return "high"
	}
	if strings.TrimSpace(currentCode) == "" || isNewConversation {
		return "low"
	}
	if containsAIKeyword(message, simpleAIRequestKeywords) {
		return "none"
	}
	return "low"
}

func containsAIKeyword(message string, keywords []string) bool {
	lowerMessage := strings.ToLower(message)
	for _, keyword := range keywords {
		if strings.Contains(lowerMessage, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func (a *App) prepareAIConversation(user User, conversationID, retryMessageID uint, message string) (AIConversation, string, error) {
	if retryMessageID == 0 {
		conversation, err := a.prepareConversation(user, conversationID, message)
		return conversation, message, err
	}
	if conversationID == 0 {
		return AIConversation{}, "", fmt.Errorf("重试请求缺少对话信息")
	}
	var retryMessage AIMessage
	err := a.DB.Joins("JOIN ai_conversations ON ai_conversations.id = ai_messages.conversation_id").
		Where("ai_messages.id = ? AND ai_messages.conversation_id = ? AND ai_messages.role = ? AND ai_conversations.user_id = ?", retryMessageID, conversationID, "assistant", user.ID).
		First(&retryMessage).Error
	if err != nil {
		return AIConversation{}, "", fmt.Errorf("需要重试的 AI 消息不存在")
	}
	if retryMessage.Status != "proposal_invalid" {
		return AIConversation{}, "", fmt.Errorf("这条 AI 消息不需要全量重试")
	}
	var previousUser AIMessage
	if err := a.DB.Where("conversation_id = ? AND role = ? AND id < ?", conversationID, "user", retryMessage.ID).Order("id DESC").First(&previousUser).Error; err != nil {
		return AIConversation{}, "", fmt.Errorf("找不到原始修改需求")
	}
	var conversation AIConversation
	if err := a.DB.Where("id = ? AND user_id = ?", conversationID, user.ID).First(&conversation).Error; err != nil {
		return AIConversation{}, "", fmt.Errorf("对话不存在")
	}
	return conversation, previousUser.Content, nil
}

func (a *App) prepareConversation(user User, conversationID uint, message string) (AIConversation, error) {
	var conversation AIConversation
	if conversationID != 0 {
		if err := a.DB.Where("id = ? AND user_id = ?", conversationID, user.ID).First(&conversation).Error; err != nil {
			return AIConversation{}, err
		}
	} else {
		title := []rune(message)
		if len(title) > 30 {
			title = title[:30]
		}
		conversation = AIConversation{UserID: user.ID, ClassID: *user.ClassID, Title: string(title)}
		if err := a.DB.Create(&conversation).Error; err != nil {
			return AIConversation{}, err
		}
	}
	entry := AIMessage{ConversationID: conversation.ID, Role: "user", Content: message, Status: "success"}
	if err := a.DB.Create(&entry).Error; err != nil {
		return AIConversation{}, err
	}
	return conversation, nil
}

func (a *App) buildAIMessages(conversationID uint, currentCode string) []ChatMessage {
	settings := a.aiSettings()
	return a.buildAIMessagesWithLimits(conversationID, currentCode, settings.HistoryMessages, settings.HistoryChars)
}

// buildAIMessagesForRequest keeps the complete current work available while
// adapting historical context to the request type. Simple edits need very
// little history; debugging benefits from a wider recent window.
func (a *App) buildAIMessagesForRequest(conversationID uint, currentCode, request string, isNewConversation, forceFull bool) []ChatMessage {
	settings := a.aiSettings()
	messageLimit, charLimit := settings.HistoryMessages, settings.HistoryChars
	if messageLimit <= 0 {
		messageLimit = 8
	}
	if charLimit <= 0 {
		charLimit = 16000
	}
	switch {
	case strings.TrimSpace(currentCode) == "" || isNewConversation:
		messageLimit, charLimit = minPositive(messageLimit, 2), minPositive(charLimit, 4000)
	case forceFull:
		messageLimit, charLimit = minPositive(messageLimit, 4), minPositive(charLimit, 8000)
	default:
		complex := false
		lower := strings.ToLower(request)
		for _, keyword := range complexAIRequestKeywords {
			if strings.Contains(lower, keyword) {
				complex = true
				break
			}
		}
		if complex {
			messageLimit, charLimit = minPositive(messageLimit, 8), minPositive(charLimit, 12000)
		} else {
			messageLimit, charLimit = minPositive(messageLimit, 4), minPositive(charLimit, 8000)
		}
	}
	return a.buildAIMessagesWithLimits(conversationID, currentCode, messageLimit, charLimit)
}

func minPositive(value, maximum int) int {
	if value <= 0 {
		return maximum
	}
	if value < maximum {
		return value
	}
	return maximum
}

func (a *App) buildAIMessagesWithLimits(conversationID uint, currentCode string, messageLimit, charLimit int) []ChatMessage {
	if messageLimit <= 0 {
		messageLimit = 8
	}
	if charLimit <= 0 {
		charLimit = 16000
	}
	var history []AIMessage
	a.DB.Where("conversation_id = ?", conversationID).Order("id DESC").Limit(messageLimit).Find(&history)

	selected := make([]ChatMessage, 0, len(history))
	usedChars := 0
	for _, entry := range history {
		if entry.Role != "user" && entry.Role != "assistant" {
			continue
		}
		if entry.Role == "assistant" && entry.Status != "" && entry.Status != "success" {
			continue
		}
		content := entry.Content
		if entry.Role == "assistant" {
			content = compactAIHistoryContent(content)
		}
		contentRunes := []rune(content)
		if usedChars+len(contentRunes) > charLimit {
			remaining := charLimit - usedChars
			if len(selected) == 0 && remaining > 0 {
				content = string(contentRunes[len(contentRunes)-remaining:])
				selected = append(selected, ChatMessage{Role: entry.Role, Content: "[历史消息已截断]\n" + content})
			}
			break
		}
		usedChars += len(contentRunes)
		selected = append(selected, ChatMessage{Role: entry.Role, Content: content})
	}

	messages := []ChatMessage{{Role: "system", Content: studentSystemPrompt()}}
	if codeContext := currentCodeSystemPrompt(currentCode); codeContext != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: codeContext})
	}
	for index := len(selected) - 1; index >= 0; index-- {
		messages = append(messages, selected[index])
	}
	return messages
}

func writeSSE(c *gin.Context, event string, payload any) {
	data, _ := json.Marshal(payload)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
}

func (a *App) handleApplyProposal(c *gin.Context) {
	proposalID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	auth := authFrom(c)
	var proposal AICodeProposal
	err := a.DB.Joins("JOIN ai_conversations ON ai_conversations.id = ai_code_proposals.conversation_id").
		Where("ai_code_proposals.id = ? AND ai_conversations.user_id = ?", proposalID, auth.User.ID).First(&proposal).Error
	if err != nil {
		jsonError(c, http.StatusNotFound, "代码提案不存在")
		return
	}
	safety := CheckHTMLSafety(proposal.Code)
	if !safety.OK {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "代码提案未通过安全检查", "safety": safety})
		return
	}
	work, err := a.getOrCreateWork(auth.User)
	if err != nil || work.IsLocked {
		jsonError(c, http.StatusForbidden, "当前作品不能修改")
		return
	}
	now := time.Now()
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		if work.DraftCode != "" && work.DraftCode != proposal.Code {
			if err := tx.Create(&WorkRevision{WorkID: work.ID, Source: "ai", Summary: "应用 AI 提案前版本", Code: work.DraftCode}).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"draft_code": proposal.Code, "draft_cleared": false}
		if work.Title == "" && proposal.SuggestedTitle != "" {
			updates["title"] = proposal.SuggestedTitle
		}
		if err := tx.Model(&work).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Model(&proposal).Update("applied_at", &now).Error
	})
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "应用代码提案失败")
		return
	}
	a.pruneRevisions(work.ID)
	c.JSON(http.StatusOK, gin.H{"code": proposal.Code, "applied_at": now, "safety": safety})
}
