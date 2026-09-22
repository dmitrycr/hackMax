package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"hackmax/backend/internal/integrations/llm"
	"hackmax/backend/internal/modules/semantic"
)

type stub struct {
	content string
	err     error
}

func (s stub) CompleteJSON(context.Context, string, json.RawMessage, json.RawMessage) (llm.Completion, error) {
	return llm.Completion{Content: json.RawMessage(s.content), Model: "model:free", PromptTokens: 10, CompletionTokens: 20}, s.err
}
func suiteForTest(t *testing.T) Suite {
	t.Helper()
	data, err := os.ReadFile("../../../../tests/fixtures/llm/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	suite, digest, err := Load(data)
	if err != nil || len(digest) != 64 {
		t.Fatalf("invalid suite: %v", err)
	}
	return suite
}
func answer(status, source, quote string) string {
	text := "Проверьте данные"
	data, _ := json.Marshal(map[string]any{"findings": []semantic.Finding{{RequirementID: "r1", Status: status, Evidence: []semantic.Evidence{{SourceID: source, Quote: quote}}, Message: "Результат", Suggestion: &text}}})
	return string(data)
}

func TestEvaluationSeparatesQualityAndTechnicalFailures(t *testing.T) {
	cases := map[string]Case{}
	for _, c := range suiteForTest(t).Cases {
		cases[c.ID] = c
	}
	for _, tc := range []struct {
		name, id, status, source, quote, outcome        string
		apiError                                        bool
		missed, falseIssue, unsupported, sourceMismatch int
	}{
		{"correct", "audience_present", "met", "p1", "для школьников 10–14 лет", "pass", false, 0, 0, 0, 0},
		{"missed", "negation", "met", "p1", "Школьники в проекте не участвуют.", "mismatch", false, 1, 0, 0, 0},
		{"false-warning", "audience_present", "needs_review", "p1", "для школьников 10–14 лет", "mismatch", false, 0, 1, 0, 0},
		{"unsupported-approval", "audience_missing_partial", "met", "budget", "Общая стоимость оборудования: 100 000 рублей.", "mismatch", false, 0, 0, 1, 0},
		{"irrelevant-source", "irrelevant_fragment", "met", "p2", "Команда использует яркие плакаты", "mismatch", false, 0, 0, 0, 1},
		{"invented-quote", "audience_present", "met", "p1", "Выдуманная цитата", "model_output_error", false, 0, 0, 0, 0},
		{"quota", "audience_present", "met", "p1", "для школьников 10–14 лет", "api_error", true, 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := stub{content: answer(tc.status, tc.source, tc.quote)}
			if tc.apiError {
				client.err = &llm.APIError{Status: 429}
			}
			r := Evaluate(context.Background(), client, "requested:free", 1, cases[tc.id])
			if r.Outcome != tc.outcome {
				t.Fatalf("got %+v", r)
			}
			s := Summarize([]Run{r})["requested:free"]
			if s.MissedIssues != tc.missed || s.FalseIssues != tc.falseIssue || s.UnsupportedApprovals != tc.unsupported || s.SourceMismatches != tc.sourceMismatch {
				t.Fatalf("wrong metrics: %+v", s)
			}
			if (tc.outcome == "api_error" || tc.outcome == "model_output_error") && s.ComparedRequirements != 0 {
				t.Fatal("technical failure must not count as a semantic comparison")
			}
		})
	}
}

func TestSuiteRejectsBrokenExpectationsBeforeCalls(t *testing.T) {
	s := suiteForTest(t)
	s.Cases[0].Expected[0].RequiredSources = []string{"unknown"}
	data, _ := json.Marshal(s)
	if _, _, err := Load(data); err == nil {
		t.Fatal("bad expected source accepted")
	}
	s = suiteForTest(t)
	s.Synthetic = false
	data, _ = json.Marshal(s)
	if _, _, err := Load(data); err == nil {
		t.Fatal("non-synthetic suite accepted")
	}
}

func TestRepeatedAnswersDetectChangedStatus(t *testing.T) {
	c := suiteForTest(t).Cases[0]
	first := Evaluate(context.Background(), stub{content: answer("met", "p1", "для школьников 10–14 лет")}, "requested:free", 1, c)
	second := Evaluate(context.Background(), stub{content: answer("needs_review", "p1", "для школьников 10–14 лет")}, "requested:free", 2, c)
	s := Summarize([]Run{first, second})["requested:free"]
	if s.RepeatedCases != 1 || s.UnstableCases != 1 {
		t.Fatalf("wrong stability metrics: %+v", s)
	}
}
