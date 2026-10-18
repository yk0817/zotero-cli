package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// VersionInfo is the structured build metadata surfaced by
// `version --output json`. Its fields mirror the -ldflags overrides applied to
// release binaries (see .goreleaser.yaml).
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// buildVersionInfo snapshots the package-level build metadata into a struct for
// structured output. Release binaries override the underlying vars via
// -ldflags; local builds report the "dev"/"none"/"unknown" defaults.
func buildVersionInfo() VersionInfo {
	return VersionInfo{Version: version, Commit: commit, Date: date}
}

// versionString renders the one-line human-readable version. It is shared by
// the cobra --version flag and the plain-text output of the version subcommand
// so the two can never drift.
func versionString() string {
	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

// newVersionCmd adds a discoverable `version` subcommand that honors
// --output json. The built-in --version flag is handled by cobra before RunE
// runs, so it always prints plain text and cannot emit the JSON envelope; this
// subcommand fills that gap. Because it is a real command it also appears in
// `zotero-cli schema`, so agents discover it the same way as every other
// command. The --version flag keeps its standard plain-text behavior for humans.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version, commit, and build date",
		Annotations: map[string]string{
			"args": "none",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if isJSON() {
				return printJSON(buildVersionInfo())
			}
			fmt.Println(versionString())
			return nil
		},
	}
}
