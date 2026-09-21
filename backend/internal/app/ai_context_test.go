package app

import (
	"strings"
	"testing"
)

func TestBuildAIMessagesSeparatesCurrentCodeAndCompactsHistoricalHTML(t *testing.T) {
	a := newTestApp(t)
	a.Config.AIHistoryMessages = 8
	a.Config.AIHistoryChars = 16000
	conversation := AIConversation{UserID: 1, ClassID: 1, Title: "context"}
	a.DB.Create(&conversation)
	a.DB.Create(&AIMessage{ConversationID: conversation.ID, Role: "user", Content: "帮我做一个游戏"})
	a.DB.Create(&AIMessage{ConversationID: conversation.ID, Role: "assistant", Content: "已完成\n```html\n<html><body>very-large-code</body></html>\n```\n可以继续修改。"})
	a.DB.Create(&AIMessage{ConversationID: conversation.ID, Role: "user", Content: "把背景改成蓝色"})

	messages := a.buildAIMessages(conversation.ID, "<html><body>current-code</body></html>")
	if len(messages) != 5 {
		t.Fatalf("expected two system messages and three history messages, got %#v", messages)
	}
	if strings.Contains(messages[0].Content, "current-code") || !strings.Contains(messages[1].Content, "current-code") {
		t.Fatalf("current code should be isolated from the stable system prompt: %#v", messages[:2])
	}
	if messages[3].Role != "assistant" || strings.Contains(messages[3].Content, "very-large-code") || !strings.Contains(messages[3].Content, "不重复携带") {
		t.Fatalf("historical HTML was not compacted: %#v", messages[3])
	}
}

func TestBuildAIMessagesUsesNewestHistoryWithinCharacterBudget(t *testing.T) {
	a := newTestApp(t)
	a.Config.AIHistoryMessages = 8
	a.Config.AIHistoryChars = 8
	conversation := AIConversation{UserID: 1, ClassID: 1, Title: "budget"}
	a.DB.Create(&conversation)
	for _, content := range []string{"1111", "2222", "3333", "4444"} {
		a.DB.Create(&AIMessage{ConversationID: conversation.ID, Role: "user", Content: content})
	}

	messages := a.buildAIMessages(conversation.ID, "")
	if len(messages) != 3 || messages[1].Content != "3333" || messages[2].Content != "4444" {
		t.Fatalf("expected newest messages within budget, got %#v", messages)
	}
}

func TestStudentAIRequestOptionsSelectsModeAndBudget(t *testing.T) {
	a := newTestApp(t)
	a.Config.AIMaxOutputTokens = 393216
	a.Config.AIModificationTokens = 393216
	a.Config.AIReasoningEffort = "low"

	options, mode := a.studentAIRequestOptions("", "做一个游戏", true, false)
	if mode != aiRequestModeGenerate || options.MaxOutputTokens != 393216 || options.ReasoningEffort != "low" {
		t.Fatalf("unexpected generation options: mode=%q options=%#v", mode, options)
	}
	options, mode = a.studentAIRequestOptions("<html></html>", "换一个新作品", true, false)
	if mode != aiRequestModeConversation || options.MaxOutputTokens != 393216 || options.ReasoningEffort != "low" {
		t.Fatalf("unexpected new conversation options: mode=%q options=%#v", mode, options)
	}
	options, mode = a.studentAIRequestOptions("<html></html>", "把背景改成蓝色", false, false)
	if mode != aiRequestModeModifyPatch || options.MaxOutputTokens != 393216 || options.ReasoningEffort != "low" {
		t.Fatalf("unexpected patch options: mode=%q options=%#v", mode, options)
	}
	options, mode = a.studentAIRequestOptions("<html></html>", "修复碰撞逻辑错误", false, false)
	if mode != aiRequestModeModifyComplex || options.MaxOutputTokens != 393216 || options.ReasoningEffort != "low" {
		t.Fatalf("unexpected complex options: mode=%q options=%#v", mode, options)
	}
	options, mode = a.studentAIRequestOptions("<html></html>", "把背景改成蓝色", false, true)
	if mode != aiRequestModeRetryFull || options.MaxOutputTokens != 393216 || options.ReasoningEffort != "low" {
		t.Fatalf("unexpected full retry options: mode=%q options=%#v", mode, options)
	}
}

func TestPrepareAIConversationRetriesWithoutDuplicatingUserMessage(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "重试班", Active: true}
	a.DB.Create(&class)
	user := User{ClassID: &class.ID, Role: RoleStudent, Name: "同学", LoginName: "retry-student", PasswordHash: "x"}
	a.DB.Create(&user)
	conversation := AIConversation{UserID: user.ID, ClassID: class.ID, Title: "重试"}
	a.DB.Create(&conversation)
	userMessage := AIMessage{ConversationID: conversation.ID, Role: "user", Content: "把背景改成蓝色", Status: "success"}
	a.DB.Create(&userMessage)
	assistant := AIMessage{ConversationID: conversation.ID, Role: "assistant", Content: `{"type":"patch"}`, Status: "proposal_invalid"}
	a.DB.Create(&assistant)

	prepared, message, err := a.prepareAIConversation(user, conversation.ID, assistant.ID, "")
	if err != nil || prepared.ID != conversation.ID || message != userMessage.Content {
		t.Fatalf("unexpected retry conversation: conversation=%#v message=%q err=%v", prepared, message, err)
	}
	var userMessageCount int64
	a.DB.Model(&AIMessage{}).Where("conversation_id = ? AND role = ?", conversation.ID, "user").Count(&userMessageCount)
	if userMessageCount != 1 {
		t.Fatalf("retry duplicated user messages: %d", userMessageCount)
	}
}

func TestBuildAIMessagesSkipsInvalidPatchAssistant(t *testing.T) {
	a := newTestApp(t)
	conversation := AIConversation{UserID: 1, ClassID: 1, Title: "invalid patch"}
	a.DB.Create(&conversation)
	a.DB.Create(&AIMessage{ConversationID: conversation.ID, Role: "user", Content: "原需求", Status: "success"})
	a.DB.Create(&AIMessage{ConversationID: conversation.ID, Role: "assistant", Content: "invalid-patch-content", Status: "proposal_invalid"})
	messages := a.buildAIMessages(conversation.ID, "<html></html>")
	for _, message := range messages {
		if strings.Contains(message.Content, "invalid-patch-content") {
			t.Fatalf("invalid patch assistant should not be sent back to the model: %#v", messages)
		}
	}
}
