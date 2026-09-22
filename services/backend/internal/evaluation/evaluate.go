package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"hackmax/backend/internal/integrations/llm"
	"hackmax/backend/internal/modules/semantic"
)

type Comparison struct {
	RequirementID  string `json:"requirement_id"`
	ExpectedStatus string `json:"expected_status"`
	ActualStatus   string `json:"actual_status"`
	StatusMatch    bool   `json:"status_match"`
	SourcesMatch   bool   `json:"sources_match"`
}
type Run struct {
	CaseID         string          `json:"case_id"`
	RequestedModel string          `json:"requested_model"`
	Repeat         int             `json:"repeat"`
	Outcome        string          `json:"outcome"` // pass, mismatch, api_error, model_output_error, input_error
	ErrorCode      string          `json:"error_code,omitempty"`
	HTTPStatus     int             `json:"http_status,omitempty"`
	ElapsedMS      int64           `json:"elapsed_ms"`
	Expected       []Expected      `json:"expected"`
	Result         semantic.Result `json:"result"`
	Comparisons    []Comparison    `json:"comparisons,omitempty"`
}

func Evaluate(ctx context.Context, client semantic.Completer, model string, repetition int, c Case) Run {
	started := time.Now()
	result, err := semantic.Check(ctx, client, c.Input)
	run := Run{CaseID: c.ID, RequestedModel: model, Repeat: repetition, Expected: c.Expected,
		Result: result, ElapsedMS: time.Since(started).Milliseconds(), Outcome: "pass"}
	if err != nil {
		var validation *semantic.CheckError
		var output *llm.OutputError
		var api *llm.APIError
		switch {
		case errors.As(err, &validation):
			run.Outcome, run.ErrorCode = validation.Kind, validation.Code
		case errors.As(err, &output):
			run.Outcome, run.ErrorCode = "model_output_error", output.Code
		case errors.As(err, &api):
			run.Outcome, run.HTTPStatus = "api_error", api.Status
		default:
			run.Outcome, run.ErrorCode = "api_error", "request_failed"
		}
		return run
	}
	actual := map[string]semantic.Finding{}
	for _, f := range result.Findings {
		actual[f.RequirementID] = f
	}
	for _, e := range c.Expected {
		f := actual[e.RequirementID]
		cmp := Comparison{RequirementID: e.RequirementID, ExpectedStatus: e.Status, ActualStatus: f.Status, StatusMatch: f.Status == e.Status, SourcesMatch: true}
		present, allowed := map[string]bool{}, map[string]bool{}
		for _, id := range e.AllowedSources {
			allowed[id] = true
		}
		for _, evidence := range f.Evidence {
			present[evidence.SourceID] = true
			if !allowed[evidence.SourceID] {
				cmp.SourcesMatch = false
			}
		}
		for _, id := range e.RequiredSources {
			if !present[id] {
				cmp.SourcesMatch = false
			}
		}
		if !cmp.StatusMatch || !cmp.SourcesMatch {
			run.Outcome = "mismatch"
		}
		run.Comparisons = append(run.Comparisons, cmp)
	}
	return run
}

type Summary struct {
	RepeatedCases        int            `json:"repeated_cases_with_valid_answers"`
	UnstableCases        int            `json:"cases_with_changed_status_or_sources"`
	Attempts             int            `json:"attempts"`
	Passed               int            `json:"passed_cases"`
	Mismatches           int            `json:"mismatched_cases"`
	APIErrors            int            `json:"api_errors"`
	ModelOutputErrors    int            `json:"model_output_errors"`
	InputErrors          int            `json:"input_errors"`
	ComparedRequirements int            `json:"compared_requirements"`
	StatusMatches        int            `json:"status_matches"`
	SourceMismatches     int            `json:"source_mismatches"`
	MissedIssues         int            `json:"missed_issues"`         // expected needs_review, got met
	FalseIssues          int            `json:"false_issues"`          // expected met, got needs_review
	UnsupportedApprovals int            `json:"unsupported_approvals"` // insufficient_data -> met
	ExpectedIssues       int            `json:"expected_issues_evaluated"`
	ExpectedMet          int            `json:"expected_met_evaluated"`
	PromptTokens         int            `json:"prompt_tokens"`
	CompletionTokens     int            `json:"completion_tokens"`
	ElapsedMS            int64          `json:"elapsed_ms"`
	ActualModels         map[string]int `json:"actual_models"`
}

func Summarize(runs []Run) map[string]*Summary {
	groups := map[string]*Summary{}
	signatures := map[string]map[string]map[string]bool{}
	counts := map[string]map[string]int{}
	for _, run := range runs {
		s := groups[run.RequestedModel]
		if s == nil {
			s = &Summary{ActualModels: map[string]int{}}
			groups[run.RequestedModel] = s
		}
		s.Attempts++
		s.ElapsedMS += run.ElapsedMS
		s.PromptTokens += run.Result.PromptTokens
		s.CompletionTokens += run.Result.CompletionTokens
		if run.Result.Model != "" {
			s.ActualModels[run.Result.Model]++
		}
		switch run.Outcome {
		case "pass":
			s.Passed++
		case "mismatch":
			s.Mismatches++
		case "api_error":
			s.APIErrors++
		case "model_output_error":
			s.ModelOutputErrors++
		case "input_error":
			s.InputErrors++
		}
		for _, c := range run.Comparisons {
			s.ComparedRequirements++
			if c.StatusMatch {
				s.StatusMatches++
			}
			if !c.SourcesMatch {
				s.SourceMismatches++
			}
			if c.ExpectedStatus == "needs_review" {
				s.ExpectedIssues++
				if c.ActualStatus == "met" {
					s.MissedIssues++
				}
			}
			if c.ExpectedStatus == "met" {
				s.ExpectedMet++
				if c.ActualStatus == "needs_review" {
					s.FalseIssues++
				}
			}
			if c.ExpectedStatus == "insufficient_data" && c.ActualStatus == "met" {
				s.UnsupportedApprovals++
			}
		}
		if run.Outcome == "pass" || run.Outcome == "mismatch" {
			if signatures[run.RequestedModel] == nil {
				signatures[run.RequestedModel] = map[string]map[string]bool{}
				counts[run.RequestedModel] = map[string]int{}
			}
			if signatures[run.RequestedModel][run.CaseID] == nil {
				signatures[run.RequestedModel][run.CaseID] = map[string]bool{}
			}
			// Ignore wording and quote length; compare statuses and source sets.
			fingerprint := map[string]any{}
			for _, f := range run.Result.Findings {
				sourceSet := map[string]bool{}
				for _, e := range f.Evidence {
					sourceSet[e.SourceID] = true
				}
				ids := []string{}
				for id := range sourceSet {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				fingerprint[f.RequirementID] = []any{f.Status, ids}
			}
			encoded, _ := json.Marshal(fingerprint)
			signatures[run.RequestedModel][run.CaseID][string(encoded)] = true
			counts[run.RequestedModel][run.CaseID]++
		}
	}
	for model, cases := range signatures {
		for id, unique := range cases {
			if counts[model][id] > 1 {
				groups[model].RepeatedCases++
			}
			if len(unique) > 1 {
				groups[model].UnstableCases++
			}
		}
	}
	return groups
}
