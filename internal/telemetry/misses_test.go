package telemetry_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mathomhaus/guild/internal/config"
	"github.com/mathomhaus/guild/internal/telemetry"
)

// ---- happy path -------------------------------------------------------------

// TestRecordMiss_HappyPath verifies that RecordMiss writes one parseable TSV
// line with the expected project and query fields.
func TestRecordMiss_HappyPath(t *testing.T) {
	home := tempHome(t)
	ctx := context.Background()
	cfg := enabledCfg()

	const project = "myproject"
	const query = "how do I configure the scoring weights"

	before := time.Now().UTC().Truncate(time.Second)
	if err := telemetry.RecordMiss(ctx, cfg, project, query); err != nil {
		t.Fatalf("RecordMiss: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	logPath := filepath.Join(home, ".guild", "misses.log")
	lines := readLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	fields := parseTSVFields(t, lines[0], 3)

	// Field 0: RFC3339 UTC timestamp.
	ts, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		t.Fatalf("field[0] not RFC3339: %v", err)
	}
	if ts.Location() != time.UTC {
		t.Errorf("timestamp not UTC: %v", ts.Location())
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("timestamp %v outside window", ts)
	}

	// Field 1: project.
	if fields[1] != project {
		t.Errorf("project: got %q, want %q", fields[1], project)
	}

	// Field 2: query (logged intentionally — it IS the retrieval system input).
	if fields[2] != query {
		t.Errorf("query: got %q, want %q", fields[2], query)
	}
}

// ---- opt-out ----------------------------------------------------------------

// TestRecordMiss_OptOut_EnvVar verifies GUILD_NO_USAGE_LOG=1 suppresses misses.log.
func TestRecordMiss_OptOut_EnvVar(t *testing.T) {
	home := tempHome(t)
	t.Setenv("GUILD_NO_USAGE_LOG", "1")

	cfg := &config.Config{
		Telemetry:  config.TelemetryConfig{UsageLog: false},
		NoUsageLog: true,
	}

	if err := telemetry.RecordMiss(context.Background(), cfg, "proj", "some query"); err != nil {
		t.Fatalf("RecordMiss: %v", err)
	}

	logPath := filepath.Join(home, ".guild", "misses.log")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("misses.log should not exist when opt-out is set")
	}
}

// TestRecordMiss_OptOut_ConfigField verifies config-field opt-out independently.
func TestRecordMiss_OptOut_ConfigField(t *testing.T) {
	home := tempHome(t)
	t.Setenv("GUILD_NO_USAGE_LOG", "")

	if err := telemetry.RecordMiss(context.Background(), disabledViaCfgField(), "proj", "query"); err != nil {
		t.Fatalf("RecordMiss: %v", err)
	}

	logPath := filepath.Join(home, ".guild", "misses.log")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("misses.log should not exist when config disables telemetry")
	}
}

// ---- best-effort ------------------------------------------------------------

// TestRecordMiss_BestEffort_ReadOnlyLog verifies that a non-writable misses.log
// causes RecordMiss to return nil (not propagate the error).
func TestRecordMiss_BestEffort_ReadOnlyLog(t *testing.T) {
	home := tempHome(t)
	guildDir := filepath.Join(home, ".guild")
	if err := os.MkdirAll(guildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(guildDir, "misses.log")
	if err := os.WriteFile(logPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	// Make it read-only so the append will fail; intentional for best-effort test.
	if err := os.Chmod(logPath, 0o444); err != nil {
		t.Fatalf("chmod read-only: %v", err)
	}

	err := telemetry.RecordMiss(context.Background(), enabledCfg(), "proj", "query")
	if err != nil {
		t.Errorf("RecordMiss returned error on write failure: %v", err)
	}
}

// ---- concurrent writers -----------------------------------------------------

// TestRecordMiss_Concurrent verifies that 4 goroutines × 100 misses each
// produces exactly 400 parseable lines.
func TestRecordMiss_Concurrent(t *testing.T) {
	home := tempHome(t)
	ctx := context.Background()
	cfg := enabledCfg()

	const goroutines = 4
	const recordsEach = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < recordsEach; i++ {
				if err := telemetry.RecordMiss(ctx, cfg, "proj", "what is a half life"); err != nil {
					t.Errorf("goroutine %d record %d: %v", g, i, err)
				}
			}
		}()
	}
	wg.Wait()

	logPath := filepath.Join(home, ".guild", "misses.log")
	lines := readLines(t, logPath)

	total := goroutines * recordsEach
	if len(lines) != total {
		t.Errorf("concurrent misses: got %d lines, want %d", len(lines), total)
	}

	for i, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Errorf("line %d has %d fields (expected 3): %q", i, len(fields), line)
			continue
		}
		if _, err := time.Parse(time.RFC3339, fields[0]); err != nil {
			t.Errorf("line %d: invalid timestamp: %v", i, err)
		}
	}

	t.Logf("concurrent miss test: %d goroutines × %d records = %d lines, all parseable",
		goroutines, recordsEach, len(lines))
}

// ---- query truncation -------------------------------------------------------

// TestRecordMiss_QueryTruncation verifies that a very long query is truncated
// before writing (keeps records under POSIX PIPE_BUF).
func TestRecordMiss_QueryTruncation(t *testing.T) {
	home := tempHome(t)

	// Build a query longer than maxQueryLen (256 bytes).
	longQuery := strings.Repeat("x", 512)

	if err := telemetry.RecordMiss(context.Background(), enabledCfg(), "proj", longQuery); err != nil {
		t.Fatalf("RecordMiss: %v", err)
	}

	logPath := filepath.Join(home, ".guild", "misses.log")
	lines := readLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	fields := parseTSVFields(t, lines[0], 3)
	if len(fields[2]) > 256 {
		t.Errorf("query field not truncated: len=%d > 256", len(fields[2]))
	}
}

// ---- nil cfg ----------------------------------------------------------------

// TestRecordMiss_NilConfig verifies nil cfg is treated as opt-out (no panic).
func TestRecordMiss_NilConfig(t *testing.T) {
	home := tempHome(t)

	err := telemetry.RecordMiss(context.Background(), nil, "proj", "query")
	if err != nil {
		t.Fatalf("RecordMiss(nil cfg): %v", err)
	}

	logPath := filepath.Join(home, ".guild", "misses.log")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("misses.log should not exist with nil cfg")
	}
}
