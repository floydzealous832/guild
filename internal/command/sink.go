package command

import (
	"fmt"
	"strings"
)

// CLISink is the concrete renderer for cobra-backed commands. It exposes
// the full CLI vocabulary — tables, sections, separators — so verbs with
// rich terminal output can use native primitives without pretending they
// share an interface with the MCP side.
//
// NoEmoji is sourced from --no-emoji / GUILD_NO_EMOJI at adapter
// construction. Every method consults it to decide between the emoji
// glyph and the bracketed ASCII fallback.
type CLISink struct {
	NoEmoji bool
}

// Emoji returns glyph unless NoEmoji is set, in which case ascii is
// returned. Used by callers that want the prefix on its own (without
// the Line helper's trailing newline).
func (s CLISink) Emoji(glyph, ascii string) string {
	if s.NoEmoji {
		return ascii
	}
	return glyph
}

// Line renders one narrated line ending in a newline. The most common
// primitive — used by every migrated verb.
func (s CLISink) Line(glyph, ascii, text string) string {
	prefix := s.Emoji(glyph, ascii)
	if prefix == "" {
		return text + "\n"
	}
	return prefix + " " + text + "\n"
}

// List renders an indented label-with-bullets block:
//
//	"  label:\n    - item\n    - item\n"
//
// Empty items returns "". Used identically across surfaces.
func (s CLISink) List(label string, items []string) string {
	return renderList(label, items)
}

// Section renders a titled section header with an underline — the
// `📝 NOTES\n----\n` pattern in scroll-style output. Emoji-prefixed
// when NoEmoji is false.
func (s CLISink) Section(glyph, ascii, title string) string {
	prefix := s.Emoji(glyph, ascii)
	var b strings.Builder
	if prefix != "" {
		b.WriteString("  " + prefix + " " + strings.ToUpper(title) + "\n")
	} else {
		b.WriteString("  " + strings.ToUpper(title) + "\n")
	}
	b.WriteString("  " + strings.Repeat("-", 40) + "\n")
	return b.String()
}

// Separator renders a full-width visual separator (used at the top of
// the scroll output, between major sections).
func (CLISink) Separator() string {
	return strings.Repeat("=", 60) + "\n"
}

// Row renders a fixed-width data row — the CLI's table-ish output.
// Format/args follow fmt.Sprintf semantics; caller controls alignment
// via %-Ns verbs. Appends a newline.
func (CLISink) Row(format string, args ...any) string {
	return fmt.Sprintf("  "+format+"\n", args...)
}

// MCPSink is the concrete renderer for MCP tool output. Always emoji
// (chat is assumed UTF-safe) and optimized for compact token-efficient
// blocks — no tables, no separators, no section headers.
type MCPSink struct{}

// Emoji always returns the emoji glyph.
func (MCPSink) Emoji(glyph, _ string) string { return glyph }

// Line matches CLISink.Line but ignores the ASCII fallback.
func (MCPSink) Line(glyph, _, text string) string {
	if glyph == "" {
		return text + "\n"
	}
	return glyph + " " + text + "\n"
}

// List matches CLISink.List — indented bullets.
func (MCPSink) List(label string, items []string) string {
	return renderList(label, items)
}

// Meta renders a dot-separated key=value meta row — the MCP compact
// form of what the CLI might render as a table.
//
//	Meta("status=next", "priority=P0", "owner=agent")
//	→ "  status=next · priority=P0 · owner=agent\n"
func (MCPSink) Meta(parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return "  " + strings.Join(kept, " · ") + "\n"
}

// Indented renders a single indented "key: value" line — MCP's compact
// alternative to CLISink.Row's table row.
func (MCPSink) Indented(label, value string) string {
	return "  " + label + ": " + value + "\n"
}

// renderList is shared between both sinks — the indented-bullet shape
// looks the same on both surfaces.
func renderList(label string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(label)
	b.WriteString(":\n")
	for _, item := range items {
		b.WriteString("    - ")
		b.WriteString(item)
		b.WriteString("\n")
	}
	return b.String()
}
