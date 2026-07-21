package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func runnable(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, RunE: func(*cobra.Command, []string) error { return nil }}
}

// Contract: buildSchemas is what agents and the auto-generated README read to
// discover commands. It must include leaf subcommands by their full path
// (e.g. "index build"), so nested commands are individually discoverable and
// not hidden behind a parent group. A pure parent group (no RunE) must not be
// emitted as a runnable command.
func TestBuildSchemasIncludesNestedSubcommands(t *testing.T) {
	root := &cobra.Command{Use: "zotero-cli"}
	parent := &cobra.Command{Use: "index"} // no RunE — a pure group
	parent.AddCommand(runnable("build", "build the index"))
	root.AddCommand(parent)
	root.AddCommand(runnable("search", "keyword search"))

	schemas := buildSchemas(root)

	names := make(map[string]bool, len(schemas))
	for _, s := range schemas {
		names[s.Name] = true
	}
	if !names["index build"] {
		t.Errorf("schema missing nested 'index build'; got %v", names)
	}
	if !names["search"] {
		t.Errorf("schema missing 'search'; got %v", names)
	}
	if names["index"] {
		t.Errorf("pure parent 'index' (no RunE) must not be emitted as a command")
	}
}
