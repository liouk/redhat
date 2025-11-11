package main

// this program has been partially written with Claude Code

import (
	"bufio"
	"context"
	"fmt"
	"jira2gh/pkg/config"
	"jira2gh/pkg/github"
	"jira2gh/pkg/jira"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const (
	StatusCodeNoNewPRs = iota
	StatusCodeNewPRsFound
	StatusCodeError
)

var rootCmd = &cobra.Command{
	Use:   "jira2gh <issue-id>...",
	Short: "Sync Jira issues to GitHub project",
	Long:  `A CLI tool to sync Jira issues to GitHub project by finding PRs in issues that aren't in the project.`,
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		config.Quiet, _ = cmd.Flags().GetBool("quiet")
		github.DryRun, _ = cmd.Flags().GetBool("dry-run")
		ctx := context.Background()

		cfg := &config.NewConfig{
			Jira:   &config.JiraConfig{},
			GitHub: &config.GitHubConfig{},
		}

		cfg.Jira.Token = os.Getenv("JIRA_API_TOKEN")
		if len(cfg.Jira.Token) == 0 {
			config.Stderr("Error: JIRA_API_TOKEN environment variable is not set\n")
			os.Exit(StatusCodeError)
		}

		cfg.GitHub.Token = os.Getenv("GITHUB_TOKEN")
		if len(cfg.GitHub.Token) == 0 {
			config.Stderr("Error: GITHUB_TOKEN environment variable is not set\n")
			os.Exit(StatusCodeError)
		}

		var err error
		if cfgFile := cmd.Flag("config").Value.String(); len(cfgFile) > 0 {
			err = cfg.CompleteFromFile(cfgFile)
		} else {
			err = cfg.CompleteFromFlags(ctx, cmd, args)
		}

		if err != nil {
			config.Stderr("Error: %v\n", err)
			os.Exit(StatusCodeError)
		}

		if err := run(ctx, cfg); err != nil {
			config.Stderr("Error: %v\n", err)
			os.Exit(StatusCodeError)
		}
	},
}

func init() {
	rootCmd.Flags().String("config", "", "yaml config file to use (has priority over CLI flags)")
	rootCmd.Flags().String("jira-host", "", "Jira host URL (e.g., https://issues.redhat.com)")
	rootCmd.Flags().String("github-project-id", "", "Self-explanatory")
	rootCmd.Flags().String("github-owner", "", "GitHub owner (user or org). If not provided, will be fetched from GitHub CLI")
	rootCmd.Flags().String("ignore-repos", "", "Comma-separated list of repositories to ignore (e.g., owner/repo1,owner/repo2)")
	rootCmd.Flags().String("ignore-prs", "", "Comma-separated list of PRs to ignore (e.g., owner/repo#123,owner/repo#456)")
	rootCmd.Flags().BoolP("quiet", "q", false, "Quiet mode: suppress all output, exit with 0=no new PRs, 1=new PRs found, 2=error")
	rootCmd.Flags().BoolP("dry-run", "", false, "Dry-run mode: do not make any changes to the github project")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(StatusCodeError)
	}
}

func run(ctx context.Context, cfg *config.NewConfig) error {
	for _, proj := range cfg.Projects {
		if err := runForProject(ctx, cfg.Jira, proj); err != nil {
			return nil
		}
	}

	return nil
}

func runForProject(ctx context.Context, jiraCfg *config.JiraConfig, proj *config.ProjectConfig) error {
	githubPRs, err := github.FetchGitHubPRs(ctx, proj)
	if err != nil {
		return err
	}
	config.Printf("Found %d PRs in GitHub Project %s/%s\n", len(githubPRs), proj.GitHubOwner, proj.GitHubProject)

	jiraPRs := map[string]struct{}{}
	for _, id := range proj.Jiras {
		config.Println("Checking jira issue", id)
		found, err := jira.ExtractJiraPRs(ctx, jiraCfg, id)
		if err != nil {
			return err
		}

		config.Printf("  => found '%d' PRs", len(found))

		for pr := range found {
			jiraPRs[pr] = struct{}{}
		}
	}

	if len(jiraPRs) == 0 {
		config.Println("\n\nNo PRs found in Jira issues specified, skipping sync.")
		return nil
	}

	newPRs := []string{}
	for jiraPR, _ := range jiraPRs {
		if _, exists := githubPRs[jiraPR]; !exists {
			if !shouldIgnorePR(jiraPR, proj) {
				newPRs = append(newPRs, jiraPR)
			}
		}
	}

	if len(newPRs) == 0 {
		config.Println("\n\nNo new PRs found to add to GitHub project, skipping sync.")
		return nil
	} else {
		sort.Strings(newPRs)
		config.Printf("\n\n%d PRs do not exist in project yet:\n", len(newPRs))
		for _, pr := range newPRs {
			config.Println("  *", pr)
		}
	}

	if config.Quiet {
		os.Exit(StatusCodeNewPRsFound)
	}

	// Prompt for confirmation
	fmt.Print("\nSync PRs to GitHub Project? [Y/n/i(nteractive)] ")
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}

	// valid responses: y (yes), i (interactive), "" (yes)
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "i" && response != "" {
		config.Println("Aborting sync.")
		return nil
	}
	interactive := strings.EqualFold(response, "i")

	if err := github.AddToProject(ctx, proj, newPRs, interactive); err != nil {
		return err
	}

	return nil
}

// shouldIgnorePR checks if a PR URL belongs to an ignored repository or is a specific ignored PR
func shouldIgnorePR(prURL string, proj *config.ProjectConfig) bool {
	// Extract owner/repo and PR number from URL (e.g., https://github.com/owner/repo/pull/123)
	parts := strings.Split(prURL, "/")
	if len(parts) < 5 {
		return false
	}

	ownerRepo := parts[3] + "/" + parts[4] // owner/repo format

	if slices.Contains(proj.IgnoreRepos, ownerRepo) {
		return true
	}

	if len(proj.IgnorePRs) > 0 && len(parts) >= 7 {
		prIdentifier := ownerRepo + "#" + parts[6]
		if slices.Contains(proj.IgnorePRs, prIdentifier) {
			return true
		}
	}

	return false
}
