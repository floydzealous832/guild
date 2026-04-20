package lore

import (
	"fmt"
	"strconv"
	"strings"
)

// DisplayPrefix is the canonical display prefix for lore entry IDs.
// Render sites use this so the next rename is a one-liner.
const DisplayPrefix = "LORE-"

// formatEntryID returns the canonical "LORE-N" form used in CLI output,
// lore_study cross-references, and inter-quest citations.
func formatEntryID(id int64) string {
	return fmt.Sprintf("%s%d", DisplayPrefix, id)
}

// ParseEntryID accepts "LORE-23", "ENTRY-23" (legacy, for backward-compat
// with existing lore summary cross-references), or a bare integer "23".
// Matching is case-insensitive. Returns an error on unrecognised shapes.
func ParseEntryID(s string) (int64, error) {
	s = strings.TrimSpace(s)
	upper := strings.ToUpper(s)
	for _, pfx := range []string{"LORE-", "ENTRY-"} {
		if strings.HasPrefix(upper, pfx) {
			s = s[len(pfx):]
			break
		}
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("lore: invalid entry id %q: %w", s, err)
	}
	if id <= 0 {
		return 0, fmt.Errorf("lore: entry id must be positive, got %d", id)
	}
	return id, nil
}
