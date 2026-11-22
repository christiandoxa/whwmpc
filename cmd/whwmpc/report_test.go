package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func TestReadOnlyDemoCommandsAndReport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := state.Store{ConfigDir: filepath.Join(dir, "config"), StateDir: filepath.Join(dir, "state")}
	scanner := inspection.New(true)
	var out, errs bytes.Buffer
	code, handled := reportCommand(ctx, []string{"check", "--json"}, scanner, store, true, "en", &out, &errs)
	if !handled || code != 0 {
		t.Fatalf("check: %d %s", code, errs.String())
	}
	var doc struct {
		Source   string `json:"source"`
		Version  int    `json:"schema_version"`
		Findings []any  `json:"findings"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.Source != "demo" || doc.Version != 1 || len(doc.Findings) < 4 {
		t.Fatalf("demo output projection = %+v, %v", doc, err)
	}
	path := filepath.Join(dir, "report.md")
	out.Reset()
	code, _ = reportCommand(ctx, []string{"report", "--format", "markdown", "--output", path}, scanner, store, true, "id", &out, &errs)
	if code != 0 {
		t.Fatalf("export: %d %s", code, errs.String())
	}
	first, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(first, []byte("DEMO")) {
		t.Fatalf("file did not contain demo report: %v", err)
	}
	code, _ = reportCommand(ctx, []string{"report", "--output", path}, scanner, store, true, "id", &out, &errs)
	again, _ := os.ReadFile(path)
	if code != 1 || !bytes.Equal(first, again) {
		t.Fatal("report can overwrite existing file")
	}
	if _, err := os.Stat(store.StateDir); !os.IsNotExist(err) {
		t.Fatal("demo created live state")
	}
}

func TestDemoRejectsSystemActionWithoutReadingPlan(t *testing.T) {
	var stderr bytes.Buffer
	m := workflow.NewMaintenance(state.Store{}, true)
	code, handled := actionCommand(context.Background(), []string{"apply", "--file", "/no/such/path", "--confirm", "id"},
		m, nil, nil, &stderr)
	if code != 1 || !handled || !bytes.Contains(stderr.Bytes(), []byte("DEMO")) {
		t.Fatalf("apply demo = (%d, %t, %q)", code, handled, stderr.String())
	}
}
