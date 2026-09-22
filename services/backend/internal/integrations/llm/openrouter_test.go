package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func completionResponse(content, finish string) string {
	body, _ := json.Marshal(map[string]any{
		"model": "provider/actual-free-model", "choices": []any{map[string]any{
			"finish_reason": finish, "message": map[string]string{"content": content},
		}}, "usage": map[string]int{"prompt_tokens": 42, "completion_tokens": 12},
	})
	return string(body)
}

func TestFreeOnlyRequestAndActualModel(t *testing.T) {
	c, err := New("secret-test-key", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.http.Timeout != 180*time.Second {
		t.Fatal("default timeout must be 180s")
	}
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != endpoint || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer secret-test-key" {
			t.Fatal("unexpected endpoint or authorization")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		provider := body["provider"].(map[string]any)
		if body["max_tokens"] != float64(8000) {
			t.Fatal("default max_tokens must be 8000")
		}
		prices := provider["max_price"].(map[string]any)
		if body["model"] != DefaultModel || provider["require_parameters"] != true || prices["prompt"] != float64(0) || prices["completion"] != float64(0) || prices["request"] != float64(0) {
			t.Fatal("free-only routing or structured output support not enforced")
		}
		if _, ok := body["models"]; ok {
			t.Fatal("no model fallbacks expected")
		}
		format := body["response_format"].(map[string]any)
		if format["type"] != "json_schema" {
			t.Fatal("missing structured output")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(completionResponse(`{"ok":true}`, "stop")))}, nil
	})
	result, err := c.CompleteJSON(context.Background(), "test", json.RawMessage(`{}`), json.RawMessage(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "provider/actual-free-model" || result.PromptTokens != 42 || string(result.Content) != `{"ok":true}` {
		t.Fatalf("wrong result: %+v", result)
	}
}

func TestConfiguredLimitsReachRequestAndEnforceTimeout(t *testing.T) {
	c, err := NewWithLimits("test-key", "", Limits{Timeout: 20 * time.Millisecond, MaxTokens: 12000})
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.MaxTokens != 12000 {
			t.Fatal("custom token limit not sent")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	// Parent deadline is deliberately longer than the configured HTTP timeout.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = c.CompleteJSON(ctx, "test", json.RawMessage(`{}`), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("slow provider must time out")
	}
	if ctx.Err() != nil {
		t.Fatal("request ignored its own shorter timeout")
	}
}

func TestRejectPaidModelsAndMissingKey(t *testing.T) {
	for _, model := range []string{"provider/paid", "openrouter/auto", "provider/free:free:online", "provider/model:free?plugin=web"} {
		if _, err := New("key", model); err == nil {
			t.Fatalf("accepted %q", model)
		}
	}
	c, err := New("", "provider/model:free")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("network called without key"); return nil, nil })
	_, err = c.CompleteJSON(context.Background(), "test", json.RawMessage(`{}`), json.RawMessage(`{}`))
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected disabled, got %v", err)
	}
}

func TestProviderErrorInsideHTTP200KeepsStatusWithoutLeakingBody(t *testing.T) {
	c, _ := New("key", "")
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"error":{"code":429,"message":"private document"}}`))}, nil
	})
	_, err := c.CompleteJSON(context.Background(), "test", json.RawMessage(`{}`), json.RawMessage(`{}`))
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Status != 429 || strings.Contains(err.Error(), "private") {
		t.Fatalf("provider error should preserve only status: %v", err)
	}
}

func TestUnusableResponsesAndRedirectAreRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate-limit", 429, "secret document text"},
		{"unauthorized", 401, "secret document text"},
		{"server-error", 503, "secret document text"},
		{"redirect", 302, "secret document text"},
		{"error-envelope", 200, `{"error":{"message":"secret document text"}}`},
		{"truncated", 200, completionResponse(`{"ok":true}`, "length")},
		{"not-json", 200, completionResponse("secret document text", "stop")},
		{"too-large", 200, strings.Repeat("x", (1<<20)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := New("key", "")
			calls := 0
			c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Fatal("unexpected retry or redirect")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://openrouter.ai/elsewhere"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			_, err := c.CompleteJSON(context.Background(), "test", json.RawMessage(`{}`), json.RawMessage(`{}`))
			if err == nil || strings.Contains(err.Error(), "secret document") {
				t.Fatalf("expected redacted failure, got %v", err)
			}
			if tc.status == 429 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Status != 429 {
					t.Fatal("lost rate-limit status")
				}
			}
		})
	}
}
