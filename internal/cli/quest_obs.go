// quest_obs.go — observability subcommands: list, guild, active, pulse,
// archive, restore. Registered via init() into the existing questCmd.
// The list --files / --deps / --json flags close the agent-autonomy gap
// by letting agents compute task parallelism without extra round-trips.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/mathomhaus/guild/internal/quest"
)

// --- flag vars (obs subcommands only) ---

var (
	// archive / restore flags
	qArchivePath string
	qRestorePath string
)

// --- command definitions ---

var (
	questArchiveCmd = &cobra.Command{
		Use:     "archive",
		Aliases: []string{"export"},
		Short:   "snapshot quest state to .guild/snapshot.json (opt-in)",
		Args:    cobra.NoArgs,
		RunE:    runQuestArchive,
	}

	questRestoreCmd = &cobra.Command{
		Use:     "restore",
		Aliases: []string{"import"},
		Short:   "restore quest state from .guild/snapshot.json",
		Args:    cobra.NoArgs,
		RunE:    runQuestRestore,
	}
)

func init() {
	// archive flags.
	questArchiveCmd.Flags().StringVar(&qArchivePath, "path", "", "snapshot path (default: .guild/snapshot.json in project root)")

	// restore flags.
	questRestoreCmd.Flags().StringVar(&qRestorePath, "path", "", "snapshot path (default: .guild/snapshot.json in project root)")

	questCmd.AddCommand(
		questArchiveCmd,
		questRestoreCmd,
	)
	// list/guild/pulse/active registered via the command registry in quest.go's init.
}

// --- runners ---

// list/guild/pulse migrated to internal/quest registry (QUEST-45).

func runQuestArchive(cmd *cobra.Command, _ []string) (rerr error) {
	ctx := ctxFromCmd(cmd)
	start := time.Now()
	cfg, err := loadCfg(cmd)
	if err != nil {
		return err
	}
	defer recordTelemetry(ctx, cfg, cfg.Project, "quest archive", start, &rerr)

	db, err := openQuestDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	p, err := resolveProject(ctx, db, cfg)
	if err != nil {
		return err
	}

	snapshotPath := qArchivePath
	if snapshotPath == "" {
		snapshotPath = filepath.Join(p.Path, ".guild", "snapshot.json")
	}

	if err := quest.Archive(ctx, db, p.ID, snapshotPath); err != nil {
		return err
	}

	sink := newEmojiSink(cfg)
	fmt.Fprintln(cmd.OutOrStdout(), sink.line("📸", "archived:", "quest state archived → "+snapshotPath))
	return nil
}

func runQuestRestore(cmd *cobra.Command, _ []string) (rerr error) {
	ctx := ctxFromCmd(cmd)
	start := time.Now()
	cfg, err := loadCfg(cmd)
	if err != nil {
		return err
	}
	defer recordTelemetry(ctx, cfg, cfg.Project, "quest restore", start, &rerr)

	db, err := openQuestDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	p, err := resolveProject(ctx, db, cfg)
	if err != nil {
		return err
	}

	snapshotPath := qRestorePath
	if snapshotPath == "" {
		snapshotPath = filepath.Join(p.Path, ".guild", "snapshot.json")
	}

	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		return fmt.Errorf("snapshot not found at %s", snapshotPath)
	}

	result, err := quest.Restore(ctx, db, p.ID, snapshotPath)
	if err != nil {
		return err
	}

	sink := newEmojiSink(cfg)
	fmt.Fprintln(cmd.OutOrStdout(), sink.line("📥", "restored:",
		fmt.Sprintf("restored %d tasks, %d notes from %s",
			result.TasksImported, result.NotesImported, snapshotPath)))
	return nil
}

// quest table renderer moved to internal/quest/list_cmd.go (QUEST-45).
