package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"hackmax/backend/internal/integrations/llm"
)

type fakeCompleter struct {
	content string
	calls   int
}

func (f *fakeCompleter) CompleteJSON(_ context.Context, _ string, _, _ json.RawMessage) (llm.Completion, error) {
	f.calls++
	return llm.Completion{Content: json.RawMessage(f.content), Model: "actual-model", PromptTokens: 10}, nil
}

func sampleInput() Input {
	return Input{Requirements: []Requirement{{ID: "r1", Text: "Указать аудиторию"}}, Fragments: []Fragment{{ID: "p1", Text: "Для школьников"}}}
}

func TestCheckValidatesCoverageEvidenceAndSchema(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		valid         bool
	}{
		{"valid", `{"findings":[{"requirement_id":"r1","status":"met","evidence":[{"source_id":"p1","quote":"Для школьников"}],"message":"Аудитория указана","suggestion":""}]}`, true},
		{"invented-quote", `{"findings":[{"requirement_id":"r1","status":"met","evidence":[{"source_id":"p1","quote":"Для пенсионеров"}],"message":"OK","suggestion":""}]}`, false},
		{"empty-quote", `{"findings":[{"requirement_id":"r1","status":"needs_review","evidence":[{"source_id":"p1","quote":" "}],"message":"OK","suggestion":""}]}`, false},
		{"unknown-source", `{"findings":[{"requirement_id":"r1","status":"met","evidence":[{"source_id":"invented","quote":"Для школьников"}],"message":"OK","suggestion":""}]}`, false},
		{"unknown-requirement", `{"findings":[{"requirement_id":"invented","status":"met","evidence":[{"source_id":"p1","quote":"Для школьников"}],"message":"OK","suggestion":""}]}`, false},
		{"missing-evidence", `{"findings":[{"requirement_id":"r1","status":"met","evidence":[],"message":"OK","suggestion":""}]}`, false},
		{"insufficient", `{"findings":[{"requirement_id":"r1","status":"insufficient_data","evidence":[],"message":"Недостаточно данных","suggestion":"Добавьте раздел"}]}`, true},
		{"missing-field", `{"findings":[{"requirement_id":"r1","status":"met","evidence":[{"source_id":"p1","quote":"Для школьников"}],"message":"OK"}]}`, false},
		{"extra-field", `{"findings":[{"requirement_id":"r1","status":"met","evidence":[{"source_id":"p1","quote":"Для школьников"}],"message":"OK","suggestion":"","approved":true}]}`, false},
		{"skipped-requirement", `{"findings":[]}`, false},
		{"unknown-status", `{"findings":[{"requirement_id":"r1","status":"approved","evidence":[{"source_id":"p1","quote":"Для школьников"}],"message":"OK","suggestion":""}]}`, false},
		{"trailing-json", `{"findings":[]} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeCompleter{content: tc.content}
			result, err := Check(context.Background(), client, sampleInput())
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, got %v", tc.valid, err)
			}
			if !tc.valid {
				var checkErr *CheckError
				if !errors.As(err, &checkErr) || checkErr.Kind != "model_output_error" {
					t.Fatalf("must be a technical model error: %v", err)
				}
				if len(result.Findings) != 0 {
					t.Fatal("invalid findings must not become applicant issues")
				}
				if result.Model != "actual-model" || result.PromptTokens != 10 {
					t.Fatal("lost metadata of rejected output")
				}
			}
			if tc.valid && (result.Model != "actual-model" || result.PromptVersion != PromptVersion) {
				t.Fatal("missing provenance")
			}
		})
	}
}

func TestInvalidInputDoesNotSpendQuota(t *testing.T) {
	input := sampleInput()
	input.Requirements = append(input.Requirements, input.Requirements[0])
	client := &fakeCompleter{}
	if _, err := Check(context.Background(), client, input); err == nil || client.calls != 0 {
		t.Fatal("duplicate requirement must fail before API call")
	}
}

func TestQuoteNormalizationPreservesMeaningfulCharacters(t *testing.T) {
	for _, tc := range []struct {
		source, quote string
		valid         bool
	}{
		{"Для\nшкольников\t10–14 лет.", "Для школьников 10–14 лет.", true},
		{"Не участвуют школьники.", "Участвуют школьники.", false},
		{"Компания ABC", "Компания АВС", false},
		{"Бюджет 100,00", "Бюджет 10000", false},
		{"Источник", " ", false},
		{"Для школьников", "Для\u00a0школьников", true},
	} {
		if got := ValidateQuote(tc.source, tc.quote); got != tc.valid {
			t.Fatalf("source=%q quote=%q: got %v", tc.source, tc.quote, got)
		}
	}
}

func TestMultipleEvidenceAndDuplicateRequirements(t *testing.T) {
	input := sampleInput()
	input.Fragments = append(input.Fragments, Fragment{ID: "p2", Text: "Для взрослых"})
	client := &fakeCompleter{content: `{"findings":[{"requirement_id":"r1","status":"needs_review","evidence":[{"source_id":"p1","quote":"Для школьников"},{"source_id":"p2","quote":"Для взрослых"}],"message":"Разные аудитории","suggestion":"Уточните аудиторию"}]}`}
	result, err := Check(context.Background(), client, input)
	if err != nil || len(result.Findings[0].Evidence) != 2 {
		t.Fatalf("multiple evidence failed: %v", err)
	}
	input.Requirements = append(input.Requirements, Requirement{ID: "r2", Text: "Второе требование"})
	client.content = `{"findings":[{"requirement_id":"r1","status":"insufficient_data","evidence":[],"message":"Нет данных","suggestion":"Уточните"},{"requirement_id":"r1","status":"insufficient_data","evidence":[],"message":"Нет данных","suggestion":"Уточните"}]}`
	if _, err := Check(context.Background(), client, input); err == nil {
		t.Fatal("duplicate findings must not replace a missing requirement")
	}
}
