package cli

// Emoji prefix table. The --no-emoji flag / GUILD_NO_EMOJI=1 env
// substitutes plain ASCII equivalents for accessibility and terminals
// that mangle multi-byte sequences.
//
// Keeping this table inside internal/cli (as opposed to a standalone
// internal/emoji package) avoids introducing a new package just to host
// constants. If a second package ever needs emoji prefixing, we extract
// then.

// Emoji glyph constants — one per narration line item.
const (
	emojiInscribed = "📜"
	emojiAppraised = "🔮"
	emojiAccepted  = "⚔️"
	emojiStale     = "👻"
	emojiCleared   = "🏆"
	emojiPosted    = "➕"
	emojiReforged  = "🔨"
	emojiLinked    = "🔗"
	emojiSealed    = "🔒"
	emojiWhispers  = "💭"
	emojiBriefing  = "📋"
	emojiParallel  = "⚡"
	emojiCommune   = "🌀"
	emojiInquest   = "⚖️"
	emojiMeld      = "🔮" // same glyph as appraised; different context
	emojiPulse     = "💓"
	emojiMigration = "🔧"
	emojiWarn      = "⚠️"
	emojiSuccess   = "✅"
	emojiErr       = "❌"
	emojiForfeited = "↩️"
	emojiUnblocked = "🔓"
	emojiCampfire  = "🏕️"
	emojiUpdated   = "✏️"
)

// ASCII fallbacks — each is a bracketed short label so downstream
// log-scrapers need only one regex regardless of --no-emoji.
const (
	asciiInscribed = "[inscribed]"
	asciiAppraised = "[appraised]"
	asciiAccepted  = "[accepted]"
	asciiStale     = "[stale]"
	asciiCleared   = "[cleared]"
	asciiPosted    = "[posted]"
	asciiReforged  = "[reforged]"
	asciiLinked    = "[linked]"
	asciiSealed    = "[sealed]"
	asciiWhispers  = "[whispers]"
	asciiBriefing  = "[briefing]"
	asciiParallel  = "[parallel]"
	asciiCommune   = "[commune]"
	asciiInquest   = "[inquest]"
	asciiMeld      = "[meld]"
	asciiPulse     = "[pulse]"
	asciiMigration = "[migration]"
	asciiWarn      = "[warn]"
	asciiSuccess   = "[ok]"
	asciiErr       = "[err]"
	asciiForfeited = "[forfeited]"
	asciiUnblocked = "[unblocked]"
	asciiCampfire  = "[campfire]"
	asciiUpdated   = "[updated]"
)

// prefix returns the emoji glyph when noEmoji is false, else the plain
// ASCII label. Callers form their full line with strings.Builder or
// fmt.Fprintf using the returned string as a prefix + space separator.
func prefix(glyph, ascii string, noEmoji bool) string {
	if noEmoji {
		return ascii
	}
	return glyph
}
