package hints

import (
	"regexp"
	"strings"
)

// This file holds the pure-function triggers + follow-through detectors
// for the 9 launch-set rules. Each is `func(ctx *Context, ev CallEvent) bool`
// so rules.go can bind them by value without a lookup table.
//
// Conventions:
//   - Triggers assume ev.Tool already matches Rule.TriggerTool.
//     (The engine checks TriggerTool before calling Trigger.)
//   - Both detectors must be side-effect free.
//   - All string compares are case-insensitive unless the semantics
//     (e.g. slug syntax) demand otherwise.

// -----------------------------------------------------------------------
// Regexes used by multiple detectors. Compiled once per package init.
// -----------------------------------------------------------------------

// todoPhrasesRE matches calibration-set phrases that mark quest-like
// content rather than durable lore. The set is intentionally small —
// ENTRY-29's calibration showed the 5 below catch the bulk of fires
// without much false positive rate.
//
// Quoted so phrases like "need to" survive a strings.ToLower before
// regex match.
var todoPhrasesRE = regexp.MustCompile(
	`\btodo\b|\bneed to\b|\bshould fix\b|\bmust fix\b|\bwe should\b`,
)

// slugRE matches hyphenated-lowercase strings, e.g. "cross-project-dedup".
var slugRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)+$`)

// questIDRE matches the QUEST-NNN pattern used in queries that should
// have gone to quest_scroll, not lore_appraise.
var questIDRE = regexp.MustCompile(`^QUEST-\d+$`)

// -----------------------------------------------------------------------
// 💡 hint — inscribe-looks-like-quest
// -----------------------------------------------------------------------

// triggerInscribeLooksLikeQuest fires on lore_inscribe whose title or
// summary carries a TODO-like phrase. Indicates the agent filed a task
// as lore (durable knowledge) rather than a quest (actionable work).
func triggerInscribeLooksLikeQuest(_ *Context, ev CallEvent) bool {
	title := strings.ToLower(ev.StringArg("title"))
	summary := strings.ToLower(ev.StringArg("summary"))
	if title == "" && summary == "" {
		return false
	}
	combined := title + "\n" + summary
	return todoPhrasesRE.MatchString(combined)
}

// followQuestPostAfterInscribe hits when a later event is quest_post.
// Caveat (per ENTRY-29): any quest_post counts regardless of topic
// relevance — accept the loose pairing as the cost of a cheap detector.
func followQuestPostAfterInscribe(_ *Context, ev CallEvent) bool {
	return ev.Tool == "quest_post"
}

// -----------------------------------------------------------------------
// 💡 hint — no-session-start
// -----------------------------------------------------------------------

// triggerNoSessionStart fires when a non-bootstrap tool runs before
// guild_session_start / quest_bounties has been observed in this
// session's Context. The engine clamps TriggerTool="*" so this runs
// against every event — we bail early for the bootstrap tools themselves.
func triggerNoSessionStart(ctx *Context, ev CallEvent) bool {
	if ev.Tool == "guild_session_start" || ev.Tool == "quest_bounties" {
		return false
	}
	return !ctx.SeenSessionStart()
}

// followSessionBootstrap hits on guild_session_start or quest_bounties.
func followSessionBootstrap(_ *Context, ev CallEvent) bool {
	return ev.Tool == "guild_session_start" || ev.Tool == "quest_bounties"
}

// -----------------------------------------------------------------------
// 💡 hint — session-end-without-brief
// -----------------------------------------------------------------------

// sessionEndCallThreshold is the call-count threshold past which a
// session without a brief starts emitting the gentle reminder. Matches
// the 30-call calibration point from ENTRY-29.
const sessionEndCallThreshold = 30

// triggerSessionEndWithoutBrief fires after sessionEndCallThreshold+
// guild events with no quest_brief in the Context. Excludes the
// triggering call from the "no brief" check so the rule does not
// suppress itself when quest_brief IS the current call.
func triggerSessionEndWithoutBrief(ctx *Context, ev CallEvent) bool {
	if ev.Tool == "quest_brief" {
		return false
	}
	if ctx.CallCount() < sessionEndCallThreshold {
		return false
	}
	// Look back over the entire session history; if any quest_brief is
	// present, suppress.
	for _, e := range ctx.Events(0) {
		if e.Tool == "quest_brief" {
			return false
		}
	}
	return true
}

// followQuestBrief hits on a later quest_brief. Shared by three rules.
func followQuestBrief(_ *Context, ev CallEvent) bool {
	return ev.Tool == "quest_brief"
}

// -----------------------------------------------------------------------
// 💡 hint — slug-query (migrated from lore.MissHint)
// -----------------------------------------------------------------------

// triggerSlugQuery fires on lore_appraise whose query looks like a slug
// or QUEST-NNN id AND returned zero results — the agent probably meant
// quest_list or quest_scroll. The pre-engine lore.slugHint only set
// MissHint on len(rows)==0; QUEST-73 restores that gate via the
// __hints_zero_result signal stuffed by appraise_cmd.go's handler.
// Firing regardless of result count is noise when the search succeeded.
func triggerSlugQuery(_ *Context, ev CallEvent) bool {
	// Gate on the zero-result signal first — cheaper than regex, and the
	// rule should never fire when the search returned hits.
	v, ok := ev.Args[zeroResultKey]
	if !ok {
		return false
	}
	zero, ok := v.(bool)
	if !ok || !zero {
		return false
	}

	q := strings.TrimSpace(ev.StringArg("query"))
	if q == "" {
		return false
	}
	// Whitespace means the query is multi-token, not a slug.
	if strings.ContainsAny(q, " \t\n") {
		return false
	}
	if questIDRE.MatchString(q) {
		return true
	}
	return slugRE.MatchString(strings.ToLower(q))
}

// followQuestListOrScroll hits on a later quest_list or quest_scroll.
func followQuestListOrScroll(_ *Context, ev CallEvent) bool {
	return ev.Tool == "quest_list" || ev.Tool == "quest_scroll"
}

// -----------------------------------------------------------------------
// 💡 hint — journal-outside-accepted
// -----------------------------------------------------------------------

// triggerJournalOutsideAccepted fires on quest_journal whose quest_id
// has not been accepted in this session's Context. "Accepted" means a
// quest_accept with the same quest_id earlier in events.
//
// Caveat: continuation sessions (quest accepted in a prior process) will
// over-fire — cross-session accept state is out of scope for v1.
func triggerJournalOutsideAccepted(ctx *Context, ev CallEvent) bool {
	qid := strings.ToUpper(strings.TrimSpace(ev.StringArg("quest_id")))
	if qid == "" {
		return false
	}
	for _, e := range ctx.Events(0) {
		if e.Tool != "quest_accept" {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(e.StringArg("quest_id"))) == qid {
			return false
		}
	}
	return true
}

// followQuestAccept hits on a later quest_accept.
func followQuestAccept(_ *Context, ev CallEvent) bool {
	return ev.Tool == "quest_accept"
}

// -----------------------------------------------------------------------
// 💡 hint — no-brief-24h (migrated from quest.BriefHint)
// -----------------------------------------------------------------------

// briefHintSessionKey is the Args key the command wrapper stuffs a
// bool into when the handler's in-DB brief check concluded "no recent
// brief". The hint engine reads that signal rather than redoing the DB
// hit — keeps the engine IO-free and the rule pure.
const briefHintSessionKey = "__hints_brief_stale"

// zeroResultKey is the Args key set by lore_appraise's handler to signal
// the search returned no rows. slug-query only fires on that signal so
// agents aren't told "did you mean quest_scroll?" when their search
// already returned useful hits (QUEST-73 — regression from the pre-engine
// lore.slugHint behavior which only set MissHint on empty result sets).
const zeroResultKey = "__hints_zero_result"

// triggerNoBrief24h fires on quest_clear when the handler's late-bound
// brief check flagged the session as stale. The pre-hint logic in
// clear_cmd.go now stuffs that flag into ev.Args under the sentinel key
// above (see the migration in command/mcp.go + cli/quest.go).
func triggerNoBrief24h(_ *Context, ev CallEvent) bool {
	v, ok := ev.Args[briefHintSessionKey]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// -----------------------------------------------------------------------
// ℹ️ fyi — inscribe-without-appraise
// -----------------------------------------------------------------------

// appraiseWindow is how far back the rule looks for a lore_appraise.
// ENTRY-29 calibrated at 5 calls, tracking "did the agent check for
// duplicates recently".
const appraiseWindow = 5

// triggerInscribeWithoutAppraise fires on lore_inscribe when no
// lore_appraise has happened in the last `appraiseWindow` events.
func triggerInscribeWithoutAppraise(ctx *Context, _ CallEvent) bool {
	// The +1 accounts for the triggering inscribe itself in the window.
	return !ctx.RecentlyCalled(appraiseWindow+1, "lore_appraise")
}

// followLoreAppraise hits on a later lore_appraise.
func followLoreAppraise(_ *Context, ev CallEvent) bool {
	return ev.Tool == "lore_appraise"
}

// -----------------------------------------------------------------------
// ℹ️ fyi — clear-without-report-detail
// -----------------------------------------------------------------------

// clearReportMinWords is the ENTRY-29-calibrated threshold below which
// a quest_clear report is considered "thin".
const clearReportMinWords = 20

// triggerClearWithoutReportDetail fires on quest_clear whose report
// body is shorter than clearReportMinWords words.
func triggerClearWithoutReportDetail(_ *Context, ev CallEvent) bool {
	report := strings.TrimSpace(ev.StringArg("report"))
	if report == "" {
		return true // empty report is trivially "thin"
	}
	return wordCount(report) < clearReportMinWords
}

// followQuestUpdateOrJournal hits on a later quest_update or quest_journal.
func followQuestUpdateOrJournal(_ *Context, ev CallEvent) bool {
	return ev.Tool == "quest_update" || ev.Tool == "quest_journal"
}

// -----------------------------------------------------------------------
// ℹ️ fyi — principle-too-long
// -----------------------------------------------------------------------

// principleMaxWords is the oath-hygiene target from ENTRY-21 / the ≤60-word
// oath principle. Mirrors lore.PrincipleMaxWordsDefault.
const principleMaxWords = 60

// triggerPrincipleTooLong fires on lore_inscribe(kind=principle) whose
// title+summary word count exceeds the principle bound.
func triggerPrincipleTooLong(_ *Context, ev CallEvent) bool {
	kind := strings.ToLower(strings.TrimSpace(ev.StringArg("kind")))
	if kind != "principle" {
		return false
	}
	title := strings.TrimSpace(ev.StringArg("title"))
	summary := strings.TrimSpace(ev.StringArg("summary"))
	if title == "" && summary == "" {
		return false
	}
	return wordCount(title)+wordCount(summary) > principleMaxWords
}

// followPrincipleShortened hits on a later lore_update or lore_reforge
// targeting a lore entry — the agent has done SOMETHING to tighten up.
func followPrincipleShortened(_ *Context, ev CallEvent) bool {
	return ev.Tool == "lore_update" || ev.Tool == "lore_reforge"
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

// wordCount counts whitespace-delimited words. Mirrors the counter lore
// uses for its principle-hygiene warning so the two rules agree on
// what "60 words" means.
func wordCount(s string) int {
	return len(strings.Fields(s))
}
