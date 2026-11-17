package github

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jira2gh/pkg/config"
	"jira2gh/pkg/jira"
)

func TestProjectTokenRouting(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
printf '%s %s\n' "$1" "${GH_TOKEN-unset}" >> "$TOKEN_LOG"
case "$1 $2" in
  project*) printf '%s\n' '{"id":"PROJECT","items":[],"fields":[{"name":"Priority","id":"FIELD"}]}' ;;
  'pr view') printf '%s\n' '{"author":{"login":"example"},"state":"OPEN"}' ;;
  'pr checks') : ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct{ name, override, normal, want string }{
		{"override", "project-token", "read-token", "project-token"},
		{"empty override", "", "read-token", "read-token"},
		{"normal auth fallback", "", "", "unset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "tokens")
			t.Setenv("TOKEN_LOG", log)
			t.Setenv("JIRA2GH_GH_TOKEN", tc.override)
			t.Setenv("GH_TOKEN", tc.normal)
			if tc.normal == "" {
				if err := os.Unsetenv("GH_TOKEN"); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("GITHUB_TOKEN", "github-fallback")
			ctx := context.Background()
			proj := &config.ProjectConfig{GitHubOwner: "example", GitHubProject: "7"}
			if _, err := FetchGitHubPRs(ctx, proj); err != nil {
				t.Fatal(err)
			}
			if _, err := ghGetProjectID(ctx, proj); err != nil {
				t.Fatal(err)
			}
			if _, err := ghGetFieldID(ctx, proj, "Priority"); err != nil {
				t.Fatal(err)
			}
			if err := ghItemAdd(ctx, proj, "https://github.com/example/repo/pull/1", nil); err != nil {
				t.Fatal(err)
			}
			if err := ghItemEdit(ctx, "PROJECT", "ITEM", "text", "FIELD", "value"); err != nil {
				t.Fatal(err)
			}
			if err := RemoveFromProject(ctx, proj, []jira.PR{{URL: "https://github.com/example/repo/pull/1", ItemID: "ITEM"}}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := FetchPRDetails(ctx, "https://github.com/example/repo/pull/1"); err != nil {
				t.Fatal(err)
			}
			if _, err := FetchJobSummary(ctx, "https://github.com/example/repo/pull/1", "OPEN"); err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
			normal := tc.normal
			if normal == "" {
				normal = "unset"
			}
			if len(lines) != 9 {
				t.Fatalf("got %d calls, want 9", len(lines))
			}
			for i, line := range lines {
				want := "project " + tc.want
				if i >= 6 {
					want = "pr " + normal
				}
				if line != want {
					t.Fatalf("call %d = %q, want %q", i, line, want)
				}
			}
			if got := os.Getenv("GH_TOKEN"); got != tc.normal {
				t.Fatal("override changed parent environment")
			}
		})
	}
}
