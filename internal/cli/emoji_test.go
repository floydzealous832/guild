package cli

import (
	"strings"
	"testing"

	"github.com/mathomhaus/guild/internal/config"
)

// TestPrefix_Table covers every emoji entry: with noEmoji=true it must
// return the bracketed ASCII label; with noEmoji=false it must contain the
// emoji glyph.
func TestPrefix_Table(t *testing.T) {
	cases := []struct {
		name  string
		glyph string
		ascii string
	}{
		{"inscribed", emojiInscribed, asciiInscribed},
		{"appraised", emojiAppraised, asciiAppraised},
		{"accepted", emojiAccepted, asciiAccepted},
		{"stale", emojiStale, asciiStale},
		{"cleared", emojiCleared, asciiCleared},
		{"posted", emojiPosted, asciiPosted},
		{"reforged", emojiReforged, asciiReforged},
		{"linked", emojiLinked, asciiLinked},
		{"sealed", emojiSealed, asciiSealed},
		{"whispers", emojiWhispers, asciiWhispers},
		{"briefing", emojiBriefing, asciiBriefing},
		{"parallel", emojiParallel, asciiParallel},
		{"commune", emojiCommune, asciiCommune},
		{"inquest", emojiInquest, asciiInquest},
		{"meld", emojiMeld, asciiMeld},
		{"pulse", emojiPulse, asciiPulse},
		{"migration", emojiMigration, asciiMigration},
		{"warn", emojiWarn, asciiWarn},
		{"ok", emojiSuccess, asciiSuccess},
		{"err", emojiErr, asciiErr},
		{"forfeited", emojiForfeited, asciiForfeited},
		{"unblocked", emojiUnblocked, asciiUnblocked},
		{"campfire", emojiCampfire, asciiCampfire},
		{"updated", emojiUpdated, asciiUpdated},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"/noEmoji=true", func(t *testing.T) {
			got := prefix(tc.glyph, tc.ascii, true)
			if got != tc.ascii {
				t.Errorf("prefix(%q, %q, true) = %q; want %q", tc.glyph, tc.ascii, got, tc.ascii)
			}
		})
		t.Run(tc.name+"/noEmoji=false", func(t *testing.T) {
			got := prefix(tc.glyph, tc.ascii, false)
			if got != tc.glyph {
				t.Errorf("prefix(%q, %q, false) = %q; want %q", tc.glyph, tc.ascii, got, tc.glyph)
			}
		})
	}
}

// TestPickEmoji covers the lore_read.go helper used by appraise/oath/echoes/whispers.
func TestPickEmoji(t *testing.T) {
	cfgOn := &config.Config{NoEmoji: true}
	cfgOff := &config.Config{NoEmoji: false}

	if got := pickEmoji(cfgOn, "🔮", "[appraised]"); got != "[appraised]" {
		t.Errorf("pickEmoji(noEmoji=true) = %q; want [appraised]", got)
	}
	if got := pickEmoji(cfgOff, "🔮", "[appraised]"); got != "🔮" {
		t.Errorf("pickEmoji(noEmoji=false) = %q; want 🔮", got)
	}
	if got := pickEmoji(nil, "🔮", "[appraised]"); got != "🔮" {
		t.Errorf("pickEmoji(nil cfg) = %q; want 🔮", got)
	}
}

// TestNoEmojiFlag_InHelp verifies --no-emoji appears in guild --help output.
func TestNoEmojiFlag_InHelp(t *testing.T) {
	buf := new(strings.Builder)
	rootCmd.SetOut(writerAdapter{buf})
	rootCmd.SetErr(writerAdapter{buf})
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	_ = rootCmd.Execute()

	out := buf.String()
	if !strings.Contains(out, "no-emoji") {
		t.Errorf("--help output does not mention 'no-emoji':\n%s", out)
	}
}

// writerAdapter bridges strings.Builder to io.Writer for SetOut/SetErr.
type writerAdapter struct{ b *strings.Builder }

func (w writerAdapter) Write(p []byte) (int, error) { return w.b.Write(p) }

// TestEnv_NoEmoji verifies GUILD_NO_EMOJI=1 sets Config.NoEmoji via config.Load.
func TestEnv_NoEmoji(t *testing.T) {
	t.Setenv("GUILD_NO_EMOJI", "1")
	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !cfg.NoEmoji {
		t.Error("GUILD_NO_EMOJI=1 did not set Config.NoEmoji=true")
	}
}

// TestEnv_NoEmoji_Off verifies that without the env var, NoEmoji defaults to false.
func TestEnv_NoEmoji_Off(t *testing.T) {
	t.Setenv("GUILD_NO_EMOJI", "")
	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.NoEmoji {
		t.Error("GUILD_NO_EMOJI unset should leave Config.NoEmoji=false")
	}
}
