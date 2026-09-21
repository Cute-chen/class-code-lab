package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func aiTestConfig(serverURL string) Config {
	return Config{AIBaseURL: serverURL + "/v1", AIAPIKey: "test-key", AIModel: "test-model", AITimeout: 2 * time.Second, AIMaxOutputTokens: 512}
}

func TestAIClientStreamsAndCollectsUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != true || body["model"] != "test-model" || body["max_completion_tokens"] != float64(512) {
			t.Fatalf("unexpected request body: %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"model\":\"test-model\",\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"model\":\"test-model\",\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":2,\"prompt_cache_hit_tokens\":7,\"prompt_cache_miss_tokens\":2}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client := NewAIClient(aiTestConfig(server.URL))
	var streamed strings.Builder
	result, err := client.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "test"}}, func(delta string) error { streamed.WriteString(delta); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "你好" || streamed.String() != "你好" || result.PromptTokens == nil || *result.PromptTokens != 9 ||
		result.PromptCacheHitTokens == nil || *result.PromptCacheHitTokens != 7 || result.PromptCacheMissTokens == nil || *result.PromptCacheMissTokens != 2 || result.FinishReason != "stop" {
		t.Fatalf("unexpected result: %#v streamed=%q", result, streamed.String())
	}
}

func TestAIClientUsesDeepSeekTokenAndReasoningParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["max_tokens"] != float64(1234) || body["reasoning_effort"] != "low" {
			t.Fatalf("deepseek controls missing from request: %#v", body)
		}
		if _, exists := body["max_completion_tokens"]; exists {
			t.Fatalf("deepseek request must not use max_completion_tokens: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"deepseek-flash","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":20,"completion_tokens":3,"prompt_cache_hit_tokens":16,"prompt_cache_miss_tokens":4}}`)
	}))
	defer server.Close()
	cfg := aiTestConfig(server.URL)
	cfg.AIModel = "deepseek-flash"
	cfg.AIMaxOutputTokens = 2048
	cfg.AIReasoningEffort = "none"
	result, err := NewAIClient(cfg).StreamWithOptions(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "test"}},
		AIRequestOptions{MaxOutputTokens: 1234, ReasoningEffort: "low"},
		func(string) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.PromptCacheHitTokens == nil || *result.PromptCacheHitTokens != 16 || result.PromptCacheMissTokens == nil || *result.PromptCacheMissTokens != 4 {
		t.Fatalf("unexpected deepseek usage: %#v", result)
	}
}

func TestAIClientRejectsLengthTruncatedResponseButPreservesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"deepseek-flash","choices":[{"message":{"content":"<html>partial"},"finish_reason":"length"}],"usage":{"prompt_tokens":120,"completion_tokens":64,"prompt_cache_hit_tokens":100,"prompt_cache_miss_tokens":20}}`)
	}))
	defer server.Close()
	cfg := aiTestConfig(server.URL)
	cfg.AIModel = "deepseek-flash"
	result, err := NewAIClient(cfg).Stream(context.Background(), []ChatMessage{{Role: "user", Content: "test"}}, func(string) error { return nil })
	var upstream *AIUpstreamError
	if !errors.As(err, &upstream) || upstream.Category != "output_truncated" {
		t.Fatalf("expected output_truncated error, result=%#v err=%v", result, err)
	}
	if result.FinishReason != "length" || result.CompletionTokens == nil || *result.CompletionTokens != 64 || result.PromptCacheHitTokens == nil || *result.PromptCacheHitTokens != 100 {
		t.Fatalf("truncated result lost usage details: %#v", result)
	}
}

func TestAIClientAcceptsNonStreamingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"compat-model","choices":[{"message":{"content":"普通响应"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	}))
	defer server.Close()
	client := NewAIClient(aiTestConfig(server.URL))
	var delta string
	result, err := client.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "test"}}, func(value string) error { delta += value; return nil })
	if err != nil || result.Content != "普通响应" || delta != "普通响应" {
		t.Fatalf("unexpected result: %#v, delta=%q, err=%v", result, delta, err)
	}
}

func TestAIClientRetries429Once(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"busy"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"compat-model","choices":[{"message":{"content":"重试成功"}}]}`)
	}))
	defer server.Close()
	client := NewAIClient(aiTestConfig(server.URL))
	result, err := client.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "test"}}, func(string) error { return nil })
	if err != nil || result.Content != "重试成功" || calls.Load() != 2 {
		t.Fatalf("result=%#v calls=%d err=%v", result, calls.Load(), err)
	}
}

func TestAIClientDoesNotRetryAuthenticationFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid key"}}`)
	}))
	defer server.Close()
	client := NewAIClient(aiTestConfig(server.URL))
	_, err := client.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "test"}}, func(string) error { return nil })
	if err == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}
