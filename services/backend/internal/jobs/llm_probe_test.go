package jobs

import (
	"testing"
	"time"
)

func TestLLMJobDeadlineAllowsRequestToFinish(t *testing.T) {
	for _, tc := range []struct{ request, want time.Duration }{
		{0, 200 * time.Second},
		{180 * time.Second, 200 * time.Second},
		{5 * time.Minute, 320 * time.Second},
	} {
		worker := &LLMProbeWorker{RequestTimeout: tc.request}
		if got := worker.Timeout(nil); got != tc.want {
			t.Fatalf("request=%s: got %s, want %s", tc.request, got, tc.want)
		}
	}
}
