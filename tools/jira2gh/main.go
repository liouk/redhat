package main

// this program has been partially written with Claude Code

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"jira2gh/pkg/config"
	"net/http"
	"os"
	"os/exec"
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

type stringSet map[string]struct{}

func (s stringSet) add(e string) {
	s[e] = struct{}{}
}

var quiet bool
var dryRun bool

var rootCmd = &cobra.Command{
	Use:   "jira2gh <issue-id>...",
	Short: "Sync Jira issues to GitHub project",
	Long:  `A CLI tool to sync Jira issues to GitHub project by finding PRs in issues that aren't in the project.`,
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		quiet, _ = cmd.Flags().GetBool("quiet")
		dryRun, _ = cmd.Flags().GetBool("dry-run")
		ctx := context.Background()

		cfg := &config.NewConfig{
			Jira:   &config.JiraConfig{},
			GitHub: &config.GitHubConfig{},
		}

		cfg.Jira.Token = os.Getenv("JIRA_API_TOKEN")
		if len(cfg.Jira.Token) == 0 {
			if !quiet {
				fmt.Fprintf(os.Stderr, "Error: JIRA_API_TOKEN environment variable is not set\n")
			}
			os.Exit(StatusCodeError)
		}

		cfg.GitHub.Token = os.Getenv("GITHUB_TOKEN")
		if len(cfg.GitHub.Token) == 0 {
			if !quiet {
				fmt.Fprintf(os.Stderr, "Error: GITHUB_TOKEN environment variable is not set\n")
			}
			os.Exit(StatusCodeError)
		}

		var err error
		if cfgFile := cmd.Flag("config").Value.String(); len(cfgFile) > 0 {
			err = cfg.CompleteFromFile(cfgFile)
		} else {
			err = cfg.CompleteFromFlags(ctx, cmd, args)
		}

		if err != nil {
			if !quiet {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			os.Exit(StatusCodeError)
		}

		if err := run(ctx, cfg); err != nil {
			if !quiet {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
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

func printOut(format string, args ...any) {
	if !quiet {
		fmt.Printf(format, args...)
	}
}

func printOutln(args ...any) {
	if !quiet {
		fmt.Println(args...)
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

func runForProject(ctx context.Context, jira *config.JiraConfig, proj *config.ProjectConfig) error {
	githubPRs, err := fetchGitHubPRs(ctx, proj)
	if err != nil {
		return err
	}
	printOut("Found %d PRs in GitHub Project %s/%s\n", len(githubPRs), proj.GitHubOwner, proj.GitHubID)

	jiraPRs := stringSet{}
	for _, id := range proj.Jiras {
		printOutln("Checking jira issue", id)
		found, err := extractJiraPRs(ctx, jira, id)
		if err != nil {
			return err
		}

		printOut("  => found '%d' PRs", len(found))

		for pr := range found {
			jiraPRs.add(pr)
		}
	}

	if len(jiraPRs) == 0 {
		printOutln("\n\nNo PRs found in Jira issues specified, skipping sync.")
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
		printOutln("\n\nNo new PRs found to add to GitHub project, skipping sync.")
		return nil
	} else {
		sort.Strings(newPRs)
		printOut("\n\n%d PRs do not exist in project yet:\n", len(newPRs))
		for _, pr := range newPRs {
			printOutln("  *", pr)
		}
	}

	// In quiet mode, exit with code 1 to indicate new PRs found (don't sync)
	if quiet {
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
		printOutln("Aborting sync.")
		return nil
	}
	interactive := strings.EqualFold(response, "i")

	if err := addToProject(ctx, proj, newPRs, interactive); err != nil {
		return err
	}

	return nil
}

// extractJiraPRs assumes that epics automatically link to all the PRs of their respective linked issues
func extractJiraPRs(ctx context.Context, jira *config.JiraConfig, issueID string) (stringSet, error) {
	url := fmt.Sprintf("%s/rest/api/2/issue/%s/remotelink", jira.Host, issueID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", jira.Token))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var remoteLinks []struct {
		ID     int    `json:"id"`
		Self   string `json:"self"`
		Object struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"object"`
	}

	if err := json.Unmarshal(body, &remoteLinks); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	prs := make(stringSet)
	for _, link := range remoteLinks {
		prs.add(link.Object.URL)
	}

	return prs, nil
}

func fetchGitHubPRs(ctx context.Context, proj *config.ProjectConfig) (stringSet, error) {
	// Run gh CLI to fetch project items
	cmd := exec.CommandContext(ctx, "gh", "project", "item-list", proj.GitHubID, "--owner", proj.GitHubOwner, "--format", "json", "--limit", "1000")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("failed to fetch GitHub project items: %w\nstderr: %s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to fetch GitHub project items: %w", err)
	}

	var response struct {
		Items []struct {
			Content struct {
				URL string `json:"url"`
			} `json:"content"`
		} `json:"items"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub project items: %w", err)
	}

	// Extract URLs
	prs := make(stringSet)
	for _, item := range response.Items {
		if item.Content.URL != "" {
			prs.add(item.Content.URL)
		}
	}

	return prs, nil
}

func addToProject(ctx context.Context, proj *config.ProjectConfig, prs []string, interactive bool) error {
	printOut("\nAdding %d PRs to GitHub project %s/%s...\n", len(prs), proj.GitHubOwner, proj.GitHubID)

	if interactive {
		for _, prURL := range prs {
			printOut("  * %s (Y/n) ", prURL)
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read confirmation: %w", err)
			}

			response = strings.TrimSpace(strings.ToLower(response))
			switch response {
			case "q":
				printOutln("Abort.")
				return nil

			case "y", "":
				output, err := ghItemAdd(ctx, proj, prURL)
				if err != nil {
					printOutln()
					return fmt.Errorf("failed to add PR %s to project: %w\noutput: %s", prURL, err, string(output))
				}
				printOutln("  => added")

			default:
				printOutln("  => skipped")
			}

		}
		return nil
	}

	for _, prURL := range prs {
		printOut("  * %s ... ", prURL)
		output, err := ghItemAdd(ctx, proj, prURL)
		if err != nil {
			printOutln()
			return fmt.Errorf("failed to add PR %s to project: %w\noutput: %s", prURL, err, string(output))
		}
		printOutln("ok")
	}

	printOut("\nSuccessfully added all %d PRs to the project!\n", len(prs))
	return nil
}

func ghItemAdd(ctx context.Context, proj *config.ProjectConfig, prURL string) ([]byte, error) {
	if dryRun {
		return nil, nil
	}

	return exec.
		CommandContext(ctx, "gh", "project", "item-add", proj.GitHubID, "--owner", proj.GitHubOwner, "--url", prURL).
		CombinedOutput()
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
