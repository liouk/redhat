package github

import (
	"context"
	"os"
	"os/exec"
)

// projectCommand selects credentials only for project operations. PR reads keep
// the caller's normal gh authentication.
func projectCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "gh", append([]string{"project"}, args...)...)
	if token := os.Getenv("JIRA2GH_GH_TOKEN"); token != "" {
		cmd.Env = append(cmd.Environ(), "GH_TOKEN="+token)
	}
	return cmd
}
