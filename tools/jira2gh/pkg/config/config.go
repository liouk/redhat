package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type NewConfig struct {
	Jira     *JiraConfig      `yaml:"jira"`
	GitHub   *GitHubConfig    `yaml:"github"`
	Projects []*ProjectConfig `yaml:"projects"`
}

type JiraConfig struct {
	Host  string `yaml:"host"`
	Token string `yaml:"-"`
}

type GitHubConfig struct {
	Token string `yaml:"-"`
}

type ProjectConfig struct {
	GitHubID    string   `yaml:"github_id"`
	GitHubOwner string   `yaml:"github_owner"`
	Jiras       []string `yaml:"jiras"`
	IgnoreRepos []string `yaml:"ignore_repos"`
	IgnorePRs   []string `yaml:"ignore_prs"`
}

func (cfg *NewConfig) CompleteFromFlags(ctx context.Context, cmd *cobra.Command, jiras []string) error {
	if len(jiras) == 0 {
		return fmt.Errorf("no jiras provided")
	}

	// Get GitHub owner from flag or GitHub CLI
	githubOwner := cmd.Flag("github-owner").Value.String()
	if len(githubOwner) == 0 {
		// Try to get owner from GitHub CLI
		var err error
		githubOwner, err = getGitHubOwner(ctx)
		if err != nil {
			return fmt.Errorf("Failed to determine GitHub owner. Please provide --github-owner flag or ensure GitHub CLI is configured: %v", err)
		}
	}

	cfg.Jira.Host = cmd.Flag("jira-host").Value.String()
	proj := &ProjectConfig{
		GitHubID: cmd.Flag("github-project-id").Value.String(),
		Jiras:    jiras,
	}

	ignoreReposStr := cmd.Flag("ignore-repos").Value.String()
	if ignoreReposStr != "" {
		for repo := range strings.SplitSeq(ignoreReposStr, ",") {
			proj.IgnoreRepos = append(proj.IgnoreRepos, strings.TrimSpace(repo))
		}
	}

	ignorePRsStr := cmd.Flag("ignore-prs").Value.String()
	if ignorePRsStr != "" {
		for pr := range strings.SplitSeq(ignorePRsStr, ",") {
			proj.IgnorePRs = append(proj.IgnorePRs, strings.TrimSpace(pr))
		}
	}

	cfg.Projects = []*ProjectConfig{proj}
	return nil
}

func (cfg *NewConfig) CompleteFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse YAML from %s: %w", filename, err)
	}

	return nil
}

// getGitHubOwner retrieves the GitHub owner from the GitHub CLI
func getGitHubOwner(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".login")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub user from gh CLI: %w", err)
	}

	owner := strings.TrimSpace(string(output))
	if owner == "" {
		return "", fmt.Errorf("GitHub CLI returned empty owner")
	}

	return owner, nil
}
