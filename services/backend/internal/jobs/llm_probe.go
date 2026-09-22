package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"hackmax/backend/internal/integrations/llm"
	"hackmax/backend/internal/modules/semantic"
)

type LLMProbeArgs struct{}

func (LLMProbeArgs) Kind() string { return "system.llm_probe" }
func (LLMProbeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "llm", MaxAttempts: 1}
}

type LLMProbeWorker struct {
	river.WorkerDefaults[LLMProbeArgs]
	Client         semantic.Completer
	RequestTimeout time.Duration
}

func (w *LLMProbeWorker) Timeout(*river.Job[LLMProbeArgs]) time.Duration {
	timeout := w.RequestTimeout
	if timeout == 0 {
		timeout = llm.DefaultTimeout
	}
	return timeout + 20*time.Second
}

// Work sends only this synthetic example, never a user's uploaded document.
// Probes are not automatically retried, to conserve free-model quota.
func (w *LLMProbeWorker) Work(ctx context.Context, job *river.Job[LLMProbeArgs]) error {
	result, err := semantic.Check(ctx, w.Client, semantic.Input{
		Requirements: []semantic.Requirement{{ID: "audience", Text: "В описании проекта должна быть указана целевая аудитория."}},
		Fragments:    []semantic.Fragment{{ID: "paragraph-1", Text: "Проект: бесплатные занятия рисованием для школьников 10–14 лет."}},
	})
	if err != nil {
		return river.JobCancel(err)
	}
	slog.Info("LLM probe completed (schema and references validated; quality not evaluated)",
		"job_id", job.ID, "model", result.Model, "prompt_version", result.PromptVersion,
		"prompt_tokens", result.PromptTokens, "completion_tokens", result.CompletionTokens)
	return nil
}
