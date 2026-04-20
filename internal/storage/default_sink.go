package storage

import (
	"io"
	"os"
)

// defaultSink returns the io.Writer Migrate uses when callers don't
// supply one. It's os.Stderr today. Isolated into its own file so tests
// can build-tag-override it if needed without touching migrate.go.
//
// Stdout is off-limits for upgrade notices because stdout is reserved
// for --json payloads that downstream tools parse; any extra line there
// breaks their JSON decode.
func defaultSink() io.Writer { return os.Stderr }
