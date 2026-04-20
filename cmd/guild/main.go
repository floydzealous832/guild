// Package main is the entry point for the guild binary.
//
// guild bundles the lore CLI, quest CLI, and MCP stdio server in one
// static binary. See https://github.com/mathomhaus/guild for docs.
package main

import (
	"fmt"
	"os"

	"github.com/mathomhaus/guild/internal/cli"
)

// version, commit, and date are stamped at build time via -ldflags.
// goreleaser sets them to the release tag, git SHA, and build date
// respectively. The defaults ("dev", "", "") apply to `go build` and
// `go install` invocations that don't pass ldflags.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	// Wire the build-time stamp values into the CLI before executing.
	cli.SetVersion(version, commit, date)

	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
