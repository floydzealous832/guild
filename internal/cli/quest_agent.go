// This file registers the agent-facing quest subcommands:
//   quest journal QUEST-X TEXT
//   quest campfire QUEST-X --hypothesis --tried --next --token-warning
//   quest scroll QUEST-X
//   quest summon QUEST-X --to AGENT
//   quest orders [--agent N]
//   quest bounties [--brief]
//   quest brief TEXT
//
// All commands follow the pattern established in quest.go: open the quest
// DB, resolve the project, delegate to the internal/quest package, format
// output via emojiSink, record telemetry. The lore DB is opened only by
// bounties (for oath/echoes) and closed before returning.
//
// Campfire emoji choice: 🏕️ — the classic camping scene. It's evocative
// and widely supported (Unicode 1.0-era). The ASCII fallback is "[campfire]".

package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mathomhaus/guild/internal/lore"
	"github.com/mathomhaus/guild/internal/quest"
)

// --------------------------------------------------------------------------
// Flag state for agent subcommands. Separate from quest.go's flag vars to
// avoid polluting the shared reset function — resetQuestFlagState clears
// only its own vars. We zero these in resetAgentFlagState called from the
// integration-test helper (registered below alongside the commands).
// --------------------------------------------------------------------------

var (
	qbBrief bool // bounties --brief
)

// resetAgentFlagState zeros the package-level flag vars between
// cobra.Execute calls in tests. Mirrors resetQuestFlagState.
func resetAgentFlagState() {
	qbBrief = false
}

// --------------------------------------------------------------------------
// Cobra command declarations
// --------------------------------------------------------------------------

var (
	questBountiesCmd = &cobra.Command{
		Use:   "bounties",
		Short: "session-start: oath + echoes + last brief + top task + parallelism",
		Args:  cobra.NoArgs,
		RunE:  runQuestBounties,
	}
)

func init() {
	questBountiesCmd.Flags().BoolVar(&qbBrief, "brief", false, "show only the last briefing")

	questCmd.AddCommand(
		questBountiesCmd,
	)
	// journal + brief + summon + orders + campfire + scroll come from
	// internal/quest registry specs; attached in quest.go's init.
}

// --------------------------------------------------------------------------
// Runners
// --------------------------------------------------------------------------

func runQuestBounties(cmd *cobra.Command, _ []string) (rerr error) {
	ctx := ctxFromCmd(cmd)
	start := time.Now()
	cfg, err := loadCfg(cmd)
	if err != nil {
		return err
	}
	defer recordTelemetry(ctx, cfg, cfg.Project, "quest bounties", start, &rerr)

	db, err := openQuestDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	p, err := resolveProject(ctx, db, cfg)
	if err != nil {
		return err
	}

	sink := newEmojiSink(cfg)
	out := cmd.OutOrStdout()

	// Wire up lore loaders only in full mode (briefOnly skips them).
	var oathLoader quest.OathLoader
	var echoLoader quest.EchoLoader
	if !qbBrief {
		loreDB, loreErr := openLoreDB(ctx)
		if loreErr == nil {
			// Close after Bounties returns.
			defer func() { _ = loreDB.Close() }()

			oathLoader = func(ctx context.Context, project string) ([]quest.OathEntry, error) {
				entries, err := lore.Oath(ctx, loreDB, project)
				if err != nil {
					return nil, err
				}
				out := make([]quest.OathEntry, len(entries))
				for i, e := range entries {
					out[i] = quest.OathEntry{Title: e.Title, Summary: e.Summary}
				}
				return out, nil
			}

			echoLoader = func(ctx context.Context, project string) ([]quest.EchoEntry, error) {
				echoes, err := lore.Echoes(ctx, loreDB, project, false)
				if err != nil {
					return nil, err
				}
				result := make([]quest.EchoEntry, len(echoes))
				for i, e := range echoes {
					result[i] = quest.EchoEntry{Title: e.Entry.Title, Reason: e.Reason}
				}
				return result, nil
			}
		}
		// If lore DB fails to open, loaders remain nil — bounties degrades gracefully.
	}

	res, err := quest.Bounties(ctx, db, p.ID, qbBrief, oathLoader, echoLoader)
	if err != nil {
		return err
	}

	// -----------------------------------------------------------------------
	// Render output.
	// -----------------------------------------------------------------------

	// Last briefing.
	if res.LastBriefText != "" {
		fmt.Fprintf(out, "%s\n",
			sink.line("📋", "Last briefing", fmt.Sprintf(
				"Last briefing [%s] by %s:", res.LastBriefAt, res.LastBriefAgent)))
		fmt.Fprintf(out, "  %s\n", res.LastBriefText)
		fmt.Fprintln(out)
	}

	if qbBrief {
		return nil
	}

	// Oath (principles).
	if len(res.Oath) > 0 {
		fmt.Fprintf(out, "%s\n", sink.line("⚔️", "Oath:", fmt.Sprintf("%d oath(s) sworn:", len(res.Oath))))
		for _, o := range res.Oath {
			fmt.Fprintf(out, "  %s — %s\n", o.Title, o.Summary)
		}
		fmt.Fprintln(out)
	}

	// Echoes (fading lore).
	if len(res.Echoes) > 0 {
		fmt.Fprintf(out, "%s\n", sink.line("👻", "Echoes:", fmt.Sprintf("%d fading echo(es):", len(res.Echoes))))
		for _, e := range res.Echoes {
			fmt.Fprintf(out, "  %s [%s]\n", e.Title, e.Reason)
		}
		fmt.Fprintln(out)
	}

	// No unclaimed tasks.
	if res.NoUnclaimed {
		fmt.Fprintln(out, sink.line("✅", "OK:", "no unclaimed tasks — all done, blocked, or in progress"))
		return nil
	}

	// Top quest.
	if res.TopQuest != nil {
		q := res.TopQuest
		prio := ""
		if q.Priority != "" {
			prio = " [" + string(q.Priority) + "]"
		}
		epic := ""
		if q.Epic != "" {
			epic = " · " + q.Epic
		}
		fmt.Fprintf(out, "%s%s%s\n", q.ID, prio, epic)
		fmt.Fprintf(out, "  %s\n", q.Subject)
		if len(q.Files) > 0 {
			fmt.Fprintf(out, "  Files: %s\n", strings.Join(q.Files, ", "))
		}
		if len(q.Acceptance) > 0 {
			for _, a := range q.Acceptance {
				fmt.Fprintf(out, "  ✓ %s\n", a)
			}
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "→  quest accept %s\n", q.ID)
	}

	// Parallelism hint.
	if res.ParallelCount > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n",
			sink.line("⚡", "[parallel]", fmt.Sprintf("%d task(s) can run in parallel:", res.ParallelCount)))
		for _, pair := range res.ParallelPairs {
			// Find the quest details for B.
			var b *quest.Quest
			for _, q := range res.AllNext {
				if q.ID == pair.B {
					b = q
					break
				}
			}
			if b != nil {
				prio := ""
				if b.Priority != "" {
					prio = "[" + string(b.Priority) + "]"
				}
				subj := b.Subject
				if len(subj) > 55 {
					subj = subj[:55]
				}
				fmt.Fprintf(out, "   %s  %s  %s\n", b.ID, prio, subj)
			}
		}
		remaining := res.ParallelCount - len(res.ParallelPairs)
		if remaining > 0 {
			fmt.Fprintf(out, "   … and %d more (quest list to see all)\n", remaining)
		}
		if res.TopQuest != nil {
			fmt.Fprintf(out, "   → spawn agents for these while you work on %s\n", res.TopQuest.ID)
		}
	}

	return nil
}
