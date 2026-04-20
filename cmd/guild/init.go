// Package main — init command registration shim.
//
// The cobra command tree for `guild init` lives in internal/cli/init_cmd.go;
// this file is intentionally minimal. It exists so the deliverable boundary
// (cmd/guild/init.go) is honoured and future cmd-layer additions have a home.
//
// The internal/cli package's init() function wires initCmd into rootCmd
// automatically when the cli package is imported by main.go. No additional
// wiring is needed here.
package main
