package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestProjectOwnerFromFile(t *testing.T) {
	for _, tc := range []struct{ name, owner, want string }{
		{"omitted", "", "@me"},
		{"empty", "  github_owner: ''\n", "@me"},
		{"organization", "  github_owner: example-org\n", "example-org"},
		{"explicit self", "  github_owner: '@me'\n", "@me"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(file, []byte("projects:\n- github_project: '8'\n"+tc.owner), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := &NewConfig{}
			if err := cfg.CompleteFromFile(file, "8"); err != nil {
				t.Fatal(err)
			}
			if got := cfg.Projects[0].Owner(); got != tc.want {
				t.Fatalf("owner = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProjectOwnerFromFlags(t *testing.T) {
	// Owner selection must not need gh or a network lookup.
	t.Setenv("PATH", t.TempDir())
	for _, owner := range []string{"@me", "example-org", ""} {
		t.Run(owner, func(t *testing.T) {
			cmd := &cobra.Command{}
			for name, value := range map[string]string{
				"github-owner": owner, "github-project-id": "8", "jira-host": "https://jira.example.com",
				"ignore-repos": "", "ignore-prs": "", "ignore-jiras": "",
			} {
				cmd.Flags().String(name, value, "")
			}
			cfg := &NewConfig{Jira: &JiraConfig{}}
			if err := cfg.CompleteFromFlags(context.Background(), cmd, []string{"EXAMPLE-123"}); err != nil {
				t.Fatal(err)
			}
			want := owner
			if want == "" {
				want = "@me"
			}
			if got := cfg.Projects[0].Owner(); got != want {
				t.Fatalf("owner = %q, want %q", got, want)
			}
		})
	}
}
