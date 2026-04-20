package cli

import (
	"bytes"
	"strings"
	"testing"
)

// runInit invokes the init subcommand via rootCmd with the given extra args.
func runInit(t *testing.T, args ...string) error {
	t.Helper()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(append([]string{"init"}, args...))
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	return rootCmd.Execute()
}

// TestInitCmd_RetiredFlags verifies that --write, --merge, and --force return
// a non-nil error containing "removed" with a migration hint.
func TestInitCmd_RetiredFlags(t *testing.T) {
	cases := []struct {
		flag string
	}{
		{"--write"},
		{"--merge"},
		{"--force"},
	}

	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			err := runInit(t, tc.flag)
			if err == nil {
				t.Fatalf("%s: expected error for retired flag, got nil", tc.flag)
			}
			if !strings.Contains(err.Error(), "removed") {
				t.Errorf("%s: error missing 'removed'; got: %v", tc.flag, err)
			}
		})
	}
}
