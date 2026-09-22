// Package semantic validates model output and its evidence, not the truth of an interpretation.
package semantic

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"hackmax/backend/internal/integrations/llm"
)

const PromptVersion = "semantic-v2"

//go:embed prompt_v2.txt
var instruction string

//go:embed result.schema.json
var schema json.RawMessage

type Requirement struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Set by the caller only when all relevant readable context was supplied.
	ContextComplete bool `json:"context_complete"`
}
type Fragment struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type Input struct {
	Requirements []Requirement `json:"requirements"`
	Fragments    []Fragment    `json:"fragments"`
}
type Evidence struct {
	SourceID string `json:"source_id"`
	Quote    string `json:"quote"`
}
type Finding struct {
	RequirementID string     `json:"requirement_id"`
	Status        string     `json:"status"`
	Evidence      []Evidence `json:"evidence"`
	Message       string     `json:"message"`
	Suggestion    *string    `json:"suggestion"`
}
type Result struct {
	Findings         []Finding `json:"findings"`
	Model            string    `json:"model"`
	PromptVersion    string    `json:"prompt_version"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
}
type Completer interface {
	CompleteJSON(context.Context, string, json.RawMessage, json.RawMessage) (llm.Completion, error)
}

// CheckError describes a processing failure, never a defect in an applicant's document.
// Invalid output is discarded as a whole; no unverified findings are returned.
type CheckError struct {
	Kind string `json:"kind"` // input_error or model_output_error
	Code string `json:"code"`
}

func (e *CheckError) Error() string   { return fmt.Sprintf("%s: %s", e.Kind, e.Code) }
func invalidOutput(code string) error { return &CheckError{Kind: "model_output_error", Code: code} }

func ValidateInput(input Input) error {
	invalid := func(code string) error { return &CheckError{Kind: "input_error", Code: code} }
	if len(input.Requirements) == 0 || len(input.Requirements) > 10 || len(input.Fragments) == 0 || len(input.Fragments) > 50 {
		return invalid("invalid_item_count")
	}
	requirements, sources := map[string]bool{}, map[string]bool{}
	for _, r := range input.Requirements {
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Text) == "" || requirements[r.ID] {
			return invalid("invalid_requirement")
		}
		requirements[r.ID] = true
	}
	for _, f := range input.Fragments {
		if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Text) == "" || sources[f.ID] {
			return invalid("invalid_fragment")
		}
		sources[f.ID] = true
	}
	payload, err := json.Marshal(input)
	if err != nil || len(payload) > 64*1024 {
		return invalid("input_too_large")
	}
	return nil
}

// Only whitespace is normalized: case, punctuation, digits and alphabets stay intact.
// Quote presence is not proof of entailment or evidence of absence in the whole file.
func ValidateQuote(source, quote string) bool {
	normalized := strings.Join(strings.Fields(quote), " ")
	return normalized != "" && strings.Contains(strings.Join(strings.Fields(source), " "), normalized)
}

func Check(ctx context.Context, client Completer, input Input) (Result, error) {
	if err := ValidateInput(input); err != nil {
		return Result{}, err
	}
	payload, _ := json.Marshal(input)
	completion, err := client.CompleteJSON(ctx, instruction, payload, schema)
	result := Result{Model: completion.Model, PromptVersion: PromptVersion,
		PromptTokens: completion.PromptTokens, CompletionTokens: completion.CompletionTokens}
	if err != nil {
		return result, err
	}
	var output struct {
		Findings []Finding `json:"findings"`
	}
	decoder := json.NewDecoder(bytes.NewReader(completion.Content))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&output) != nil {
		return result, invalidOutput("invalid_schema")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return result, invalidOutput("trailing_data")
	}
	if len(output.Findings) != len(input.Requirements) {
		return result, invalidOutput("incomplete_coverage")
	}
	requirements, sources := map[string]bool{}, map[string]string{}
	for _, r := range input.Requirements {
		requirements[r.ID] = true
	}
	for _, f := range input.Fragments {
		sources[f.ID] = f.Text
	}
	seen := map[string]bool{}
	for _, f := range output.Findings {
		if !requirements[f.RequirementID] || seen[f.RequirementID] {
			return result, invalidOutput("invalid_requirement_reference")
		}
		seen[f.RequirementID] = true
		if strings.TrimSpace(f.Message) == "" || f.Evidence == nil || f.Suggestion == nil {
			return result, invalidOutput("missing_field")
		}
		if f.Status != "met" && f.Status != "needs_review" && f.Status != "insufficient_data" {
			return result, invalidOutput("invalid_status")
		}
		if f.Status != "insufficient_data" && len(f.Evidence) == 0 {
			return result, invalidOutput("missing_evidence")
		}
		for _, evidence := range f.Evidence {
			source, exists := sources[evidence.SourceID]
			if !exists {
				return result, invalidOutput("unknown_source")
			}
			if !ValidateQuote(source, evidence.Quote) {
				return result, invalidOutput("unverified_quote")
			}
		}
	}
	result.Findings = output.Findings
	return result, nil
}
