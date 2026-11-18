package config

import (
	"context"
	"fmt"
	"os"
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
	Email string `yaml:"email"`
	Token string `yaml:"-"`
}

type GitHubConfig struct{}

type ProjectConfig struct {
	GitHubProject   string   `yaml:"github_project"`
	GitHubProjectID string   `yaml:"-"`
	GitHubOwner     string   `yaml:"github_owner,omitempty"`
	Jiras           []string `yaml:"jiras"`
	IgnoreRepos     []string `yaml:"ignore_repos"`
	IgnorePRs       []string `yaml:"ignore_prs"`
	IgnoreJiras     []string `yaml:"ignore_jiras"`
	Authors         []string `yaml:"authors"`
	SkipJira        bool     `yaml:"-"`
}

// Owner defaults to the user authenticated by the project token.
func (p *ProjectConfig) Owner() string {
	if owner := strings.TrimSpace(p.GitHubOwner); owner != "" {
		return owner
	}
	return "@me"
}

type GitHubFieldValue struct {
	Field   string `yaml:"field"`
	FieldID string `yaml:"-"`
	Value   string `yaml:"value"`
	ValueID string `yaml:"-"`
}

func (cfg *NewConfig) CompleteFromFlags(_ context.Context, cmd *cobra.Command, jiras []string) error {
	if len(jiras) == 0 {
		return fmt.Errorf("no jiras provided")
	}

	cfg.Jira.Host = cmd.Flag("jira-host").Value.String()
	proj := &ProjectConfig{
		GitHubProject: cmd.Flag("github-project-id").Value.String(),
		GitHubOwner:   cmd.Flag("github-owner").Value.String(),
		Jiras:         jiras,
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

	ignoreJirasStr := cmd.Flag("ignore-jiras").Value.String()
	if ignoreJirasStr != "" {
		for j := range strings.SplitSeq(ignoreJirasStr, ",") {
			proj.IgnoreJiras = append(proj.IgnoreJiras, strings.TrimSpace(j))
		}
	}

	cfg.Projects = []*ProjectConfig{proj}
	return nil
}

func (cfg *NewConfig) CompleteFromFile(filename string, projectFilter string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse YAML from %s: %w", filename, err)
	}

	// Filter projects if a project filter is specified
	if projectFilter != "" {
		filterSet := make(map[string]bool)
		for p := range strings.SplitSeq(projectFilter, ",") {
			filterSet[strings.TrimSpace(p)] = true
		}
		filtered := []*ProjectConfig{}
		for _, proj := range cfg.Projects {
			if filterSet[proj.GitHubProject] {
				filtered = append(filtered, proj)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no project found matching --project=%s", projectFilter)
		}
		cfg.Projects = filtered
	}

	return nil
}
