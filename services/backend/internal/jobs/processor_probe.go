package jobs

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

type ProcessorProbeArgs struct{}

func (ProcessorProbeArgs) Kind() string { return "system.processor_probe" }

type ProcessorReadiness interface{ Ready(context.Context) error }

// ProcessorProbeWorker verifies the real River -> Go -> Python connection.
// It does not claim to check a user's document.
type ProcessorProbeWorker struct {
	river.WorkerDefaults[ProcessorProbeArgs]
	Processor ProcessorReadiness
}

func (w *ProcessorProbeWorker) Work(ctx context.Context, job *river.Job[ProcessorProbeArgs]) error {
	if err := w.Processor.Ready(ctx); err != nil {
		return err
	}
	slog.Info("processor probe completed", "job_id", job.ID)
	return nil
}
