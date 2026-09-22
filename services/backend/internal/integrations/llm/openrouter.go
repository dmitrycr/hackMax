// Package llm contains the server-side OpenRouter boundary. Only free models
// are accepted; no SDK, tools, plugins or paid model fallback are enabled.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const endpoint = "https://openrouter.ai/api/v1/chat/completions"
const DefaultModel = "openrouter/free"

var ErrDisabled = errors.New("OPENROUTER_API_KEY is not configured")
var freeModel = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+:free$`)

type Client struct {
	apiKey    string
	model     string
	maxTokens int
	http      *http.Client
}

type Completion struct {
	Content          json.RawMessage
	Model            string
	PromptTokens     int
	CompletionTokens int
}

// APIError deliberately excludes provider response bodies: they can echo input.
type APIError struct{ Status int }

// OutputError indicates an unusable generation rather than an applicant error.
type OutputError struct{ Code string }

func (e *OutputError) Error() string { return "LLM output error: " + e.Code }

func (e *APIError) Error() string {
	return fmt.Sprintf("OpenRouter request failed (HTTP %d); check credentials, model support or free quota", e.Status)
}

func New(apiKey, model string) (*Client, error) {
	return NewWithLimits(apiKey, model, DefaultLimits())
}

func NewWithLimits(apiKey, model string, limits Limits) (*Client, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if model == "" {
		model = DefaultModel
	}
	if model != DefaultModel && !freeModel.MatchString(model) {
		return nil, errors.New("OPENROUTER_MODEL must be openrouter/free or a provider/model:free ID")
	}
	return &Client{apiKey: strings.TrimSpace(apiKey), model: model, maxTokens: limits.MaxTokens, http: &http.Client{
		Timeout:       limits.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// CompleteJSON makes one bounded request. Callers validate the returned JSON
// against their domain contract. Retrying belongs to the job layer, not here.
func (c *Client) CompleteJSON(ctx context.Context, instruction string, input, schema json.RawMessage) (Completion, error) {
	if c.apiKey == "" {
		return Completion{}, ErrDisabled
	}
	if len(input) > 64*1024 || !json.Valid(input) || !json.Valid(schema) {
		return Completion{}, errors.New("invalid or oversized LLM input/schema")
	}
	payload := map[string]any{
		"model": c.model, "stream": false, "max_tokens": c.maxTokens,
		"messages": []map[string]string{
			{"role": "system", "content": instruction},
			{"role": "user", "content": string(input)},
		},
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "document_review", "strict": true, "schema": schema,
		}},
		"provider": map[string]any{
			"require_parameters": true,
			"max_price":          map[string]int{"prompt": 0, "completion": 0, "request": 0},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Completion{}, errors.New("cannot encode LLM request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Completion{}, errors.New("cannot create LLM request")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Completion{}, ctx.Err()
		}
		return Completion{}, errors.New("OpenRouter transport error or timeout")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Completion{}, &APIError{Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return Completion{}, &OutputError{Code: "invalid_or_oversized_response"}
	}
	var response struct {
		Model   string          `json:"model"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &response) != nil {
		return Completion{}, &OutputError{Code: "invalid_response_envelope"}
	}
	completion := Completion{Model: response.Model,
		PromptTokens: response.Usage.PromptTokens, CompletionTokens: response.Usage.CompletionTokens}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		// Some providers report failures in a successful HTTP envelope.
		// Keep the numeric status, never echo their potentially sensitive message.
		var providerError struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(response.Error, &providerError) == nil && providerError.Code >= 400 && providerError.Code <= 599 {
			return completion, &APIError{Status: providerError.Code}
		}
		return completion, &OutputError{Code: "provider_error_envelope"}
	}
	if len(response.Choices) != 1 || response.Model == "" {
		return completion, &OutputError{Code: "missing_completion"}
	}
	choice := response.Choices[0]
	if choice.FinishReason != "stop" || choice.Message.Refusal != "" || !json.Valid([]byte(choice.Message.Content)) {
		return completion, &OutputError{Code: "refused_incomplete_or_invalid_json"}
	}
	completion.Content = json.RawMessage(choice.Message.Content)
	return completion, nil
}
