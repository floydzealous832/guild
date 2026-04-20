package quest

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathomhaus/guild/internal/command"
	"github.com/mathomhaus/guild/internal/lore"
	"github.com/mathomhaus/guild/internal/storage"
)

// setupLoreDB creates a file-backed lore DB under t.TempDir, applies
// migrations, and registers the given project. Returns the file path so
// callers can open fresh connections without sharing the handle the
// handler will close via defer.
func setupLoreDB(t *testing.T, projectID string) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lore.db")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatalf("open lore db: %v", err)
	}
	if err := storage.Migrate(ctx, db, "lore"); err != nil {
		_ = db.Close()
		t.Fatalf("migrate lore db: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO projects (id, path) VALUES (?, ?)`,
		projectID, t.TempDir(),
	); err != nil {
		_ = db.Close()
		t.Fatalf("register lore project %q: %v", projectID, err)
	}
	_ = db.Close()
	return path
}

// openLoreVerifyDB opens a fresh connection to the lore DB at path for
// test verification queries. The handler owns and closes its own
// connection; this handle is independent for post-handler assertions.
func openLoreVerifyDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatalf("open verify lore db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newPostDeps builds a command.Deps that uses the given questDB handle
// and opens a fresh lore DB connection per call (so the handler's
// defer-Close doesn't affect test verification handles).
func newPostDeps(questDB *sql.DB, loreDBPath, pid string) command.Deps {
	return command.Deps{
		OpenDB: func(_ context.Context) (*sql.DB, error) {
			return questDB, nil
		},
		ResolveProj: func(_ context.Context, _ string) (string, error) {
			return pid, nil
		},
		OpenLoreDB: func(ctx context.Context) (*sql.DB, error) {
			if loreDBPath == "" {
				return nil, fmt.Errorf("lore db not configured")
			}
			return storage.Open(ctx, loreDBPath)
		},
	}
}

// callPostHandler invokes the PostCommand handler directly.
func callPostHandler(t *testing.T, d command.Deps, in PostInput) (PostOutput, error) {
	t.Helper()
	return PostCommand.Handler(context.Background(), d, in)
}

// findLoreEntry looks up a lore entry by ID in the given DB.
func findLoreEntry(t *testing.T, loreDB *sql.DB, entryID int64) *lore.Entry {
	t.Helper()
	ctx := context.Background()
	var (
		id         int64
		promptedBy sql.NullString
		summary    string
		kind       string
		topic      string
	)
	err := loreDB.QueryRowContext(ctx,
		`SELECT id, prompted_by, summary, kind, topic FROM entries WHERE id = ?`,
		entryID,
	).Scan(&id, &promptedBy, &summary, &kind, &topic)
	if err != nil {
		t.Fatalf("find lore entry %d: %v", entryID, err)
	}
	return &lore.Entry{
		ID:         id,
		PromptedBy: promptedBy.String,
		Summary:    summary,
		Kind:       lore.Kind(kind),
		Topic:      topic,
	}
}

// countLoreEntries returns the count of entries in loreDB for the project.
func countLoreEntries(t *testing.T, loreDB *sql.DB, pid string) int {
	t.Helper()
	var n int
	err := loreDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM entries WHERE project_id = ?`, pid,
	).Scan(&n)
	if err != nil {
		t.Fatalf("count entries: %v", err)
	}
	return n
}

// postCmdSink satisfies the lineListSink interface for narration tests.
type postCmdSink struct{}

func (postCmdSink) Line(glyph, _ /*ascii*/, text string) string {
	return glyph + " " + text + "\n"
}

func (postCmdSink) List(label string, items []string) string {
	return label + ": " + strings.Join(items, ", ") + "\n"
}

// TestPostCmd_SpecPresent verifies the atomic spec dance:
//   - lore entry is created with prompted_by=QUEST-X and summary=spec text
//   - quest acceptance contains "spec: LORE-N" bullet
//   - output narration says "posted QUEST-X with spec LORE-N"
func TestPostCmd_SpecPresent(t *testing.T) {
	qdb, pid := newTestDB(t)
	lorePath := setupLoreDB(t, pid)
	deps := newPostDeps(qdb, lorePath, pid)

	specText := "This is the rationale for the design approach."
	out, err := callPostHandler(t, deps, PostInput{
		Subject: "spec-present quest",
		Spec:    specText,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.SpecEntry == nil {
		t.Fatal("SpecEntry is nil; expected an entry ID")
	}
	entryID := *out.SpecEntry

	// Verify lore entry exists with correct metadata.
	ldb := openLoreVerifyDB(t, lorePath)
	entry := findLoreEntry(t, ldb, entryID)
	if entry.PromptedBy != out.Quest.ID {
		t.Errorf("PromptedBy=%q; want %q", entry.PromptedBy, out.Quest.ID)
	}
	if entry.Summary != specText {
		t.Errorf("Summary=%q; want %q", entry.Summary, specText)
	}
	if entry.Kind != lore.KindDecision {
		t.Errorf("Kind=%q; want decision", entry.Kind)
	}

	// Verify "spec: LORE-N" bullet is in quest acceptance.
	specBullet := fmt.Sprintf("spec: LORE-%d", entryID)
	found := false
	for _, a := range out.Quest.Acceptance {
		if a == specBullet {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("acceptance does not contain %q; got %v", specBullet, out.Quest.Acceptance)
	}

	// Verify narration line.
	sink := postCmdSink{}
	narration := formatPosted(sink, out)
	wantNarr := fmt.Sprintf("posted %s with spec LORE-%d", out.Quest.ID, entryID)
	if !strings.Contains(narration, wantNarr) {
		t.Errorf("narration=%q; want substring %q", narration, wantNarr)
	}
}

// TestPostCmd_SpecAbsent verifies that omitting spec leaves lore untouched
// and narration shows the standard single-line form.
func TestPostCmd_SpecAbsent(t *testing.T) {
	qdb, pid := newTestDB(t)
	lorePath := setupLoreDB(t, pid)
	deps := newPostDeps(qdb, lorePath, pid)

	out, err := callPostHandler(t, deps, PostInput{
		Subject:    "no-spec quest",
		Acceptance: []string{"basic criterion"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.SpecEntry != nil {
		t.Errorf("SpecEntry = %d; want nil when spec absent", *out.SpecEntry)
	}
	ldb := openLoreVerifyDB(t, lorePath)
	if countLoreEntries(t, ldb, pid) != 0 {
		t.Error("lore entries created when spec was absent; want zero")
	}
	// acceptance should be exactly as provided (no extra bullet)
	if len(out.Quest.Acceptance) != 1 || out.Quest.Acceptance[0] != "basic criterion" {
		t.Errorf("acceptance = %v; want [basic criterion]", out.Quest.Acceptance)
	}

	// Standard narration: no "with spec" part.
	sink := postCmdSink{}
	narration := formatPosted(sink, out)
	if strings.Contains(narration, "with spec") {
		t.Errorf("narration=%q; should not contain 'with spec' when spec absent", narration)
	}
	if !strings.Contains(narration, out.Quest.ID) {
		t.Errorf("narration=%q; should contain quest ID %q", narration, out.Quest.ID)
	}
}

// TestPostCmd_SpecEmptyString verifies that spec="" is treated as absent:
// no entry created, acceptance unchanged.
func TestPostCmd_SpecEmptyString(t *testing.T) {
	qdb, pid := newTestDB(t)
	lorePath := setupLoreDB(t, pid)
	deps := newPostDeps(qdb, lorePath, pid)

	out, err := callPostHandler(t, deps, PostInput{
		Subject: "empty-spec quest",
		Spec:    "",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.SpecEntry != nil {
		t.Errorf("SpecEntry = %d; want nil for empty spec", *out.SpecEntry)
	}
	ldb := openLoreVerifyDB(t, lorePath)
	if countLoreEntries(t, ldb, pid) != 0 {
		t.Error("lore entries created for empty spec; want zero")
	}
}

// TestPostCmd_HintFiresOnRichAcceptance verifies hint fires when acceptance
// exceeds 150 words (strictly greater — >150).
func TestPostCmd_HintFiresOnRichAcceptance(t *testing.T) {
	qdb, pid := newTestDB(t)
	lorePath := setupLoreDB(t, pid)
	deps := newPostDeps(qdb, lorePath, pid)

	// Build an acceptance with >150 words across 4 bullets of 40 words each.
	// 4 × 40 = 160 words > 150.
	words := make([]string, 40)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", i+1)
	}
	bullet40 := strings.Join(words, " ")
	acceptance := []string{bullet40, bullet40, bullet40, bullet40}

	out, err := callPostHandler(t, deps, PostInput{
		Subject:    "rich-acceptance quest",
		Acceptance: acceptance,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.HintLine == "" {
		t.Error("HintLine is empty; expected hint to fire for >150-word acceptance")
	}
	if !strings.Contains(out.HintLine, "160 words") {
		t.Errorf("HintLine=%q; expected to mention '160 words'", out.HintLine)
	}
	if !strings.Contains(out.HintLine, "4 bullets") {
		t.Errorf("HintLine=%q; expected to mention '4 bullets'", out.HintLine)
	}
}

// TestPostCmd_HintSuppressedWithSpec verifies hint does NOT fire when spec
// is provided even if acceptance is rich (>150 words).
func TestPostCmd_HintSuppressedWithSpec(t *testing.T) {
	qdb, pid := newTestDB(t)
	lorePath := setupLoreDB(t, pid)
	deps := newPostDeps(qdb, lorePath, pid)

	words := make([]string, 40)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", i+1)
	}
	bullet40 := strings.Join(words, " ")
	acceptance := []string{bullet40, bullet40, bullet40, bullet40}

	out, err := callPostHandler(t, deps, PostInput{
		Subject:    "rich-with-spec quest",
		Acceptance: acceptance,
		Spec:       "Here is the design rationale.",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.HintLine != "" {
		t.Errorf("HintLine=%q; expected empty when spec is provided", out.HintLine)
	}
}

// TestPostCmd_HintSuppressedOnLightAcceptance verifies hint does NOT fire
// when acceptance is small (<=150 words, <5 bullets, no keywords).
func TestPostCmd_HintSuppressedOnLightAcceptance(t *testing.T) {
	qdb, pid := newTestDB(t)
	lorePath := setupLoreDB(t, pid)
	deps := newPostDeps(qdb, lorePath, pid)

	out, err := callPostHandler(t, deps, PostInput{
		Subject:    "light-acceptance quest",
		Acceptance: []string{"simple pass criteria", "another short one"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.HintLine != "" {
		t.Errorf("HintLine=%q; expected empty for light acceptance", out.HintLine)
	}
}

// TestPostCmd_BoundaryCases tests boundary semantics:
//   - exactly 150 words: hint does NOT fire (threshold is strictly >150)
//   - exactly 5 bullets: hint fires (threshold is >=5)
func TestPostCmd_BoundaryCases(t *testing.T) {
	t.Run("exactly_150_words_no_hint", func(t *testing.T) {
		qdb, pid := newTestDB(t)
		lorePath := setupLoreDB(t, pid)
		deps := newPostDeps(qdb, lorePath, pid)

		// Build exactly 150 words: 3 bullets × 50 words each.
		words50 := make([]string, 50)
		for i := range words50 {
			words50[i] = fmt.Sprintf("w%d", i+1)
		}
		bullet50 := strings.Join(words50, " ")
		acceptance := []string{bullet50, bullet50, bullet50}

		// Sanity-check: must be exactly 150.
		totalWords := 0
		for _, a := range acceptance {
			totalWords += len(strings.Fields(a))
		}
		if totalWords != 150 {
			t.Fatalf("test setup: totalWords=%d, want 150", totalWords)
		}

		out, err := callPostHandler(t, deps, PostInput{
			Subject:    "exactly-150-words",
			Acceptance: acceptance,
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		// Exactly 150 words is NOT >150, so no hint.
		if out.HintLine != "" {
			t.Errorf("HintLine=%q; expected empty at exactly 150 words (threshold is >150)", out.HintLine)
		}
	})

	t.Run("exactly_5_bullets_fires_hint", func(t *testing.T) {
		qdb, pid := newTestDB(t)
		lorePath := setupLoreDB(t, pid)
		deps := newPostDeps(qdb, lorePath, pid)

		// 5 short bullets — well under 150 words total, no keywords.
		acceptance := []string{"a", "b", "c", "d", "e"}

		out, err := callPostHandler(t, deps, PostInput{
			Subject:    "exactly-5-bullets",
			Acceptance: acceptance,
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		// Exactly 5 bullets = >=5, so hint fires.
		if out.HintLine == "" {
			t.Error("HintLine is empty; expected hint to fire at exactly 5 bullets (threshold is >=5)")
		}
	})
}

// TestPostCmd_HintFiresOnKeyword verifies hint fires when a design-language
// keyword is present regardless of word count or bullet count.
func TestPostCmd_HintFiresOnKeyword(t *testing.T) {
	keywords := []string{"approach", "propose", "schema", "protocol", "algorithm"}
	for _, kw := range keywords {
		kw := kw
		t.Run(kw, func(t *testing.T) {
			qdb, pid := newTestDB(t)
			lorePath := setupLoreDB(t, pid)
			deps := newPostDeps(qdb, lorePath, pid)

			out, err := callPostHandler(t, deps, PostInput{
				Subject:    "keyword-" + kw,
				Acceptance: []string{"Use the " + kw + " here."},
			})
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if out.HintLine == "" {
				t.Errorf("HintLine is empty; expected hint to fire for keyword %q", kw)
			}
		})
	}
}

// TestPostCmd_SpecTopicFallback verifies that when epic is empty, topic
// defaults to "quest-spec".
func TestPostCmd_SpecTopicFallback(t *testing.T) {
	qdb, pid := newTestDB(t)
	lorePath := setupLoreDB(t, pid)
	deps := newPostDeps(qdb, lorePath, pid)

	out, err := callPostHandler(t, deps, PostInput{
		Subject: "no-epic quest",
		Spec:    "rationale here",
		// no Epic set
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.SpecEntry == nil {
		t.Fatal("SpecEntry is nil")
	}
	ldb := openLoreVerifyDB(t, lorePath)
	entry := findLoreEntry(t, ldb, *out.SpecEntry)
	if entry.Topic != "quest-spec" {
		t.Errorf("topic=%q; want quest-spec when epic is empty", entry.Topic)
	}
}

// TestPostCmd_SpecUsesEpicAsTopic verifies that when epic is set, it is
// used as the lore entry topic.
func TestPostCmd_SpecUsesEpicAsTopic(t *testing.T) {
	qdb, pid := newTestDB(t)
	lorePath := setupLoreDB(t, pid)
	deps := newPostDeps(qdb, lorePath, pid)

	out, err := callPostHandler(t, deps, PostInput{
		Subject: "with-epic quest",
		Epic:    "my-epic",
		Spec:    "rationale here",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.SpecEntry == nil {
		t.Fatal("SpecEntry is nil")
	}
	ldb := openLoreVerifyDB(t, lorePath)
	entry := findLoreEntry(t, ldb, *out.SpecEntry)
	if entry.Topic != "my-epic" {
		t.Errorf("topic=%q; want my-epic", entry.Topic)
	}
}

// TestPostCmd_NarrationFormat locks the formatPosted output for
// spec-present, spec-absent, and hint-present cases.
func TestPostCmd_NarrationFormat(t *testing.T) {
	entryID := int64(42)
	q := &Quest{ID: "QUEST-7", Subject: "test subject"}
	sink := postCmdSink{}

	t.Run("spec_present_narration", func(t *testing.T) {
		out := PostOutput{Quest: q, SpecEntry: &entryID}
		narration := formatPosted(sink, out)
		want := "posted QUEST-7 with spec LORE-42: test subject"
		if !strings.Contains(narration, want) {
			t.Errorf("narration=%q; want substring %q", narration, want)
		}
	})

	t.Run("spec_absent_narration", func(t *testing.T) {
		out := PostOutput{Quest: q}
		narration := formatPosted(sink, out)
		if strings.Contains(narration, "with spec") {
			t.Errorf("narration=%q; should not contain 'with spec'", narration)
		}
		if !strings.Contains(narration, "posted QUEST-7") {
			t.Errorf("narration=%q; want 'posted QUEST-7'", narration)
		}
	})

	t.Run("hint_line_appears_below_narration", func(t *testing.T) {
		out := PostOutput{Quest: q, HintLine: "some hint text"}
		narration := formatPosted(sink, out)
		if !strings.Contains(narration, "some hint text") {
			t.Errorf("narration=%q; hint line missing", narration)
		}
		// Hint should appear after the narration line (after a newline).
		parts := strings.SplitN(narration, "\n", 2)
		if len(parts) < 2 {
			t.Errorf("narration=%q; expected newline separating narration and hint", narration)
		}
		if !strings.Contains(parts[1], "some hint text") {
			t.Errorf("hint not on second line: %q", parts[1])
		}
	})
}
