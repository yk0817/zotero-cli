package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// setBuildMetadata overrides the package-level build vars for a test and
// restores them afterward, so a test can assert on known values without being
// coupled to the local build's actual "dev"/"none"/"unknown" defaults.
func setBuildMetadata(t *testing.T, v, c, d string) {
	t.Helper()
	origV, origC, origD := version, commit, date
	t.Cleanup(func() { version, commit, date = origV, origC, origD })
	version, commit, date = v, c, d
}

// Contract: buildVersionInfo carries the three -ldflags build vars
// (version/commit/date) into the struct fields the JSON output exposes. A
// release binary that overrides these must report the real values, not the
// local "dev" defaults, or downstream tooling can't tell which build it drives.
func TestBuildVersionInfoCarriesBuildMetadata(t *testing.T) {
	setBuildMetadata(t, "1.2.3", "abc1234", "2026-07-18")

	got := buildVersionInfo()

	want := VersionInfo{Version: "1.2.3", Commit: "abc1234", Date: "2026-07-18"}
	if got != want {
		t.Errorf("buildVersionInfo() = %+v, want %+v", got, want)
	}
}

// Contract: `version --output json` must emit the standard success envelope
// {"ok":true,"data":{version,commit,date}} — this is the whole point of the
// subcommand (the built-in --version flag can't, #33). Agents parse this shape,
// so it is decoded and checked field-by-field, not string-matched.
func TestVersionJSONEnvelopeShape(t *testing.T) {
	setBuildMetadata(t, "1.2.3", "abc1234", "2026-07-18")

	// printJSON wraps data as JSONResponse{OK:true, Data:...}; marshal the same
	// value the subcommand hands it and assert the decoded shape.
	raw, err := json.Marshal(JSONResponse{OK: true, Data: buildVersionInfo()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
			Date    string `json:"date"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !env.OK {
		t.Errorf("ok = false, want true")
	}
	if env.Data.Version != "1.2.3" || env.Data.Commit != "abc1234" || env.Data.Date != "2026-07-18" {
		t.Errorf("data = %+v, want {1.2.3 abc1234 2026-07-18}", env.Data)
	}
}

// Contract: the version subcommand must be discoverable via `schema`, since
// that is how agents (and the auto-generated README table) enumerate commands.
// A --version flag alone is not discoverable there; the subcommand is.
func TestVersionCommandDiscoverableInSchema(t *testing.T) {
	root := &cobra.Command{Use: "zotero-cli"}
	root.AddCommand(newVersionCmd())

	schemas := buildSchemas(root)

	var found *CommandSchema
	for i := range schemas {
		if schemas[i].Name == "version" {
			found = &schemas[i]
			break
		}
	}
	if found == nil {
		t.Fatal("version command missing from schema output")
	}
	if found.Args != "none" {
		t.Errorf("version args = %q, want %q", found.Args, "none")
	}
}

// Contract: the plain-text version rendering is shared by the --version flag
// and the subcommand via a single versionString(), so it must include all three
// build fields. If a field were dropped here the two surfaces would silently
// diverge from what `version --output json` reports.
func TestVersionStringIncludesAllBuildFields(t *testing.T) {
	setBuildMetadata(t, "1.2.3", "abc1234", "2026-07-18")

	s := versionString()

	for _, want := range []string{"1.2.3", "abc1234", "2026-07-18"} {
		if !strings.Contains(s, want) {
			t.Errorf("versionString() = %q, missing %q", s, want)
		}
	}
}
