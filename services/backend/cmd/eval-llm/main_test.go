package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hackmax/backend/internal/evaluation"
)

func TestSavePreservesPartialReportAndReplacesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "report.json")
	r := report{PlannedCalls: 8, Runs: []evaluation.Run{{CaseID: "c1", RequestedModel: "test:free", Outcome: "api_error", HTTPStatus: 429}}}
	if err := save(path, &r); err != nil {
		t.Fatal(err)
	}
	r.StoppedEarly = "quota"
	if err := save(path, &r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.CompletedCalls != 1 || got.PlannedCalls != 8 || got.StoppedEarly != "quota" || got.Summary["test:free"].APIErrors != 1 {
		t.Fatalf("partial report was lost: %+v", got)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatal("temporary report files were left behind")
	}
}
