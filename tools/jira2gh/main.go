package main

// this program has been partially written with Claude Code

import (
	"bufio"
	"context"
	"fmt"
	"jira2gh/pkg/config"
	"jira2gh/pkg/github"
	"jira2gh/pkg/jira"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

const (
	StatusCodeNoNewPRs = iota
	StatusCodeNewPRsFound
	StatusCodeError
)

type prInfo struct {
	repo   string // owner/repo
	number string // PR number
	title  string // PR title
	author string // PR author
	state  string // PR state
	issue  string
	epic   string
	url    string // PR URL
}

func parsePRURL(url string) (owner, repo, number string) {
	// URL format: https://github.com/owner/repo/pull/number
	parts := strings.Split(url, "/")
	if len(parts) >= 7 && parts[2] == "github.com" && parts[5] == "pull" {
		return parts[3], parts[4], parts[6]
	}
	return "", "", ""
}

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
			projectFilter := cmd.Flag("project").Value.String()
			err = cfg.CompleteFromFile(cfgFile, projectFilter)
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
	rootCmd.Flags().String("project", "", "when using --config, filter to only run for the project with this github_project value")
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
			return err
		}
	}

	return nil
}

func runForProject(ctx context.Context, jiraCfg *config.JiraConfig, proj *config.ProjectConfig) error {
	config.Printf("Fetching PRs from GitHub Project %s/%s...\n", proj.GitHubOwner, proj.GitHubProject)
	githubPRs, err := github.FetchGitHubPRs(ctx, proj)
	if err != nil {
		return err
	}
	config.Printf("✓ Found %d PRs in project\n", len(githubPRs))

	config.Println("\nChecking Jira issues for linked PRs...")
	jiraPRs := map[string]jira.PR{}
	for _, id := range proj.Jiras {
		prs, err := jira.ExtractJiraPRs(ctx, jiraCfg, id)
		if err != nil {
			return err
		}
		prCount := len(prs)
		maps.Copy(jiraPRs, prs)
		totalCount := len(jiraPRs)

		prCountWord := "PRs"
		if prCount == 1 {
			prCountWord = "PR"
		}
		config.Printf("  %-20s →  %d %s found (%d total)\n", id, prCount, prCountWord, totalCount)
	}

	// Enrich PRs with GitHub details (author, state)
	config.Println("\nFetching PR details from GitHub...")
	for url, pr := range jiraPRs {
		author, state, err := github.FetchPRDetails(ctx, url)
		if err != nil {
			config.Printf("  Warning: could not fetch details for %s: %v\n", url, err)
			continue
		}
		pr.Author = author
		pr.State = state
		jiraPRs[url] = pr
	}
	for url, pr := range githubPRs {
		author, state, err := github.FetchPRDetails(ctx, url)
		if err != nil {
			config.Printf("  Warning: could not fetch details for %s: %v\n", url, err)
			continue
		}
		pr.Author = author
		pr.State = state
		githubPRs[url] = pr
	}

	prWord := "PRs"
	if len(jiraPRs) == 1 {
		prWord = "PR"
	}
	config.Printf("✓ Found %d unique %s across all Jira issues\n", len(jiraPRs), prWord)

	if len(jiraPRs) == 0 {
		config.Println("\nNo PRs found in Jira issues specified, skipping sync.")
		return nil
	}

	newPRs := []jira.PR{}
	for jiraPRUrl, jiraPR := range jiraPRs {
		if _, exists := githubPRs[jiraPRUrl]; !exists {
			if !shouldIgnorePR(jiraPRUrl, proj) {
				newPRs = append(newPRs, jiraPR)
			}
		}
	}

	// Find PRs to remove: in GitHub with tracked epic, but no longer in Jira
	removedPRs := []jira.PR{}
	for url, ghPR := range githubPRs {
		if ghPR.JiraEpic == "" {
			continue
		}
		if !slices.Contains(proj.Jiras, ghPR.JiraEpic) {
			continue
		}
		if _, stillInJira := jiraPRs[url]; !stillInJira {
			if !shouldIgnorePR(url, proj) {
				removedPRs = append(removedPRs, ghPR)
			}
		}
	}

	if len(newPRs) == 0 && len(removedPRs) == 0 {
		config.Println("\nNo changes to sync.")
		return nil
	}

	// Display new PRs to add
	if len(newPRs) > 0 {
		prWord = "PRs"
		if len(newPRs) == 1 {
			prWord = "PR"
		}
		config.Printf("\n%d new %s to add to project:\n", len(newPRs), prWord)
		displayGroupedPRs(groupPRsByRepo(newPRs))
	}

	// Display PRs to remove
	if len(removedPRs) > 0 {
		prWord = "PRs"
		if len(removedPRs) == 1 {
			prWord = "PR"
		}
		config.Printf("\n%d %s no longer linked to tracked epics:\n", len(removedPRs), prWord)
		displayGroupedPRs(groupPRsByRepo(removedPRs))
	}

	if config.Quiet {
		os.Exit(StatusCodeNewPRsFound)
	}

	// Prompt for additions
	if len(newPRs) > 0 {
		prWord = "PRs"
		if len(newPRs) == 1 {
			prWord = "PR"
		}
		fmt.Printf("\nAdd %d %s to GitHub Project? [Y/n] ", len(newPRs), prWord)
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response == "y" || response == "" {
			if err := github.AddToProject(ctx, proj, newPRs); err != nil {
				return err
			}
		}
	}

	// Prompt for removals
	if len(removedPRs) > 0 {
		prWord = "PRs"
		if len(removedPRs) == 1 {
			prWord = "PR"
		}
		fmt.Printf("\nRemove %d %s from GitHub Project? [Y/n] ", len(removedPRs), prWord)
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response == "y" || response == "" {
			if err := github.RemoveFromProject(ctx, proj, removedPRs); err != nil {
				return err
			}
		}
	}

	return nil
}

func groupPRsByRepo(prs []jira.PR) map[string][]prInfo {
	prsByRepo := make(map[string][]prInfo)
	for _, pr := range prs {
		owner, repo, number := parsePRURL(pr.URL)
		if owner == "" {
			continue
		}
		repoKey := owner + "/" + repo
		prsByRepo[repoKey] = append(prsByRepo[repoKey], prInfo{
			repo:   repoKey,
			number: number,
			title:  pr.Title,
			author: pr.Author,
			state:  pr.State,
			issue:  pr.JiraIssue,
			epic:   pr.JiraEpic,
			url:    pr.URL,
		})
	}
	return prsByRepo
}

func displayGroupedPRs(prsByRepo map[string][]prInfo) {
	repos := make([]string, 0, len(prsByRepo))
	for repo := range prsByRepo {
		repos = append(repos, repo)
	}
	slices.Sort(repos)

	for _, repo := range repos {
		prs := prsByRepo[repo]
		slices.SortFunc(prs, func(a, b prInfo) int {
			return strings.Compare(a.number, b.number)
		})

		config.Printf("\n  %s\n", repo)
		for _, pr := range prs {
			config.Printf("    • #%s  %s\n", pr.number, pr.title)
			config.Printf("      Author: %-20s State: %s\n", pr.author, pr.state)
			config.Printf("      Jira: %-20s Epic: %s\n", pr.issue, pr.epic)
			config.Printf("      Link: %s\n", pr.url)
		}
	}
}

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
