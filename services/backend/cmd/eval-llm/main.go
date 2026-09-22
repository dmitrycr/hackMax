// eval-llm runs synthetic quality checks directly through the production semantic
// module. It does not require River, PostgreSQL, MAX or user documents.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"hackmax/backend/internal/evaluation"
	"hackmax/backend/internal/integrations/llm"
	"hackmax/backend/internal/modules/semantic"
)

type report struct {
	StartedAt      time.Time                      `json:"started_at"`
	UpdatedAt      time.Time                      `json:"updated_at"`
	SuiteVersion   string                         `json:"suite_version"`
	SuiteSHA256    string                         `json:"suite_sha256"`
	PromptVersion  string                         `json:"prompt_version"`
	RequestTimeout string                         `json:"request_timeout"`
	MaxTokens      int                            `json:"max_tokens"`
	PlannedCalls   int                            `json:"planned_calls"`
	CompletedCalls int                            `json:"completed_calls"`
	StoppedEarly   string                         `json:"stopped_early,omitempty"`
	Limitations    string                         `json:"limitations"`
	Runs           []evaluation.Run               `json:"runs"`
	Summary        map[string]*evaluation.Summary `json:"summary_by_requested_model"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	casePath := flag.String("cases", "../../tests/fixtures/llm/cases.json", "synthetic suite JSON")
	out := flag.String("out", "../../data/evaluations/latest.json", "JSON report, including synthetic findings")
	env := flag.String("env-file", "", "optional .env path; existing environment takes precedence")
	modelsFlag := flag.String("models", "", "comma-separated free model IDs; defaults to OPENROUTER_MODEL")
	repeat := flag.Int("repeat", 1, "repetitions of every case (1–10)")
	interval := flag.Duration("interval", 4*time.Second, "pause between calls; does not replace provider quotas")
	flag.Parse()
	if *repeat < 1 || *repeat > 10 || *interval < 0 {
		return errors.New("invalid repeat or interval")
	}
	if *env != "" {
		if err := godotenv.Load(*env); err != nil {
			return fmt.Errorf("load env file: %w", err)
		}
	}
	data, err := os.ReadFile(*casePath)
	if err != nil {
		return err
	}
	suite, digest, err := evaluation.Load(data)
	if err != nil {
		return fmt.Errorf("invalid evaluation suite: %w", err)
	}
	limits, err := llm.LimitsFromEnv()
	if err != nil {
		return err
	}
	modelList := *modelsFlag
	if modelList == "" {
		modelList = os.Getenv("OPENROUTER_MODEL")
	}
	if modelList == "" {
		modelList = llm.DefaultModel
	}
	models := strings.Split(modelList, ",")
	clients, seen := []*llm.Client{}, map[string]bool{}
	key := os.Getenv("OPENROUTER_API_KEY")
	if strings.TrimSpace(key) == "" {
		return llm.ErrDisabled
	}
	for i, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			return errors.New("empty or duplicate model ID")
		}
		models[i], seen[model] = model, true
		client, err := llm.NewWithLimits(key, model, limits)
		if err != nil {
			return err
		}
		clients = append(clients, client)
	}
	planned := len(models) * len(suite.Cases) * (*repeat)
	if planned > 100 {
		return errors.New("at most 100 calls per evaluation; split the suite or reduce repetitions")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := report{StartedAt: time.Now().UTC(), SuiteVersion: suite.Version, SuiteSHA256: digest,
		PromptVersion: semantic.PromptVersion, RequestTimeout: limits.Timeout.String(), MaxTokens: limits.MaxTokens,
		PlannedCalls: planned, Runs: []evaluation.Run{},
		Limitations: "Synthetic development set, not an independent benchmark. Automatic checks compare statuses, source IDs and quote presence, not semantic entailment or factual correctness of suggestions. No raw rejected output is saved."}
	if err := save(*out, &r); err != nil {
		return err
	} // Validate destination before spending quota.
	fmt.Printf("Evaluation: %d synthetic cases, %d model(s), %d planned calls; prompt %s\n", len(suite.Cases), len(models), planned, semantic.PromptVersion)
	failed := false
loop:
	for m, client := range clients {
		for n := 1; n <= *repeat; n++ {
			for _, c := range suite.Cases {
				if len(r.Runs) > 0 && *interval > 0 {
					timer := time.NewTimer(*interval)
					select {
					case <-timer.C:
					case <-ctx.Done():
						timer.Stop()
						r.StoppedEarly = "cancelled"
						break loop
					}
				}
				if ctx.Err() != nil {
					r.StoppedEarly = "cancelled"
					break loop
				}
				result := evaluation.Evaluate(ctx, client, models[m], n, c)
				r.Runs = append(r.Runs, result)
				if result.Outcome != "pass" {
					failed = true
				}
				fmt.Printf("[%d/%d] %s / %s #%d: %s (%d ms, actual=%s, HTTP=%d)\n", len(r.Runs), planned, models[m], c.ID, n, result.Outcome, result.ElapsedMS, result.Result.Model, result.HTTPStatus)
				if err := save(*out, &r); err != nil {
					return err
				}
				if result.HTTPStatus == 401 || result.HTTPStatus == 402 || result.HTTPStatus == 429 {
					r.StoppedEarly = fmt.Sprintf("HTTP %d: credentials or quota; no automatic retries", result.HTTPStatus)
					break loop
				}
				if result.Outcome == "api_error" && result.HTTPStatus == 0 {
					r.StoppedEarly = "network or response failure; no automatic retries"
					break loop
				}
			}
		}
	}
	if err := save(*out, &r); err != nil {
		return err
	}
	for _, model := range models {
		if s := r.Summary[model]; s != nil {
			fmt.Printf("%s: %d/%d passed; mismatches=%d, API errors=%d, model-output errors=%d\n", model, s.Passed, s.Attempts, s.Mismatches, s.APIErrors, s.ModelOutputErrors)
		}
	}
	fmt.Printf("Report: %s\n", *out)
	if failed || r.CompletedCalls != planned {
		return errors.New("evaluation contains failures or is incomplete; inspect the JSON report")
	}
	return nil
}

func save(path string, r *report) error {
	r.UpdatedAt = time.Now().UTC()
	r.CompletedCalls = len(r.Runs)
	r.Summary = evaluation.Summarize(r.Runs)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".eval-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
