package github

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"jira2gh/pkg/config"
	"os"
	"os/exec"
	"strings"
)

var DryRun bool

func FetchGitHubPRs(ctx context.Context, proj *config.ProjectConfig) (map[string]struct{}, error) {
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
	prs := map[string]struct{}{}
	for _, item := range response.Items {
		if item.Content.URL != "" {
			prs[item.Content.URL] = struct{}{}
		}
	}

	return prs, nil
}

func AddToProject(ctx context.Context, proj *config.ProjectConfig, prs []string, interactive bool) error {
	config.Printf("\nAdding %d PRs to GitHub project %s/%s...\n", len(prs), proj.GitHubOwner, proj.GitHubID)

	if interactive {
		for _, prURL := range prs {
			config.Printf("  * %s (Y/n) ", prURL)
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read confirmation: %w", err)
			}

			response = strings.TrimSpace(strings.ToLower(response))
			switch response {
			case "q":
				config.Println("Abort.")
				return nil

			case "y", "":
				output, err := ghItemAdd(ctx, proj, prURL)
				if err != nil {
					config.Println()
					return fmt.Errorf("failed to add PR %s to project: %w\noutput: %s", prURL, err, string(output))
				}
				config.Println("  => added")

			default:
				config.Println("  => skipped")
			}

		}
		return nil
	}

	for _, prURL := range prs {
		config.Printf("  * %s ... ", prURL)
		output, err := ghItemAdd(ctx, proj, prURL)
		if err != nil {
			config.Println()
			return fmt.Errorf("failed to add PR %s to project: %w\noutput: %s", prURL, err, string(output))
		}
		config.Println("ok")
	}

	config.Printf("\nSuccessfully added all %d PRs to the project!\n", len(prs))
	return nil
}

func ghItemAdd(ctx context.Context, proj *config.ProjectConfig, prURL string) ([]byte, error) {
	if DryRun {
		return nil, nil
	}

	return exec.
		CommandContext(ctx, "gh", "project", "item-add", proj.GitHubID, "--owner", proj.GitHubOwner, "--url", prURL).
		CombinedOutput()
}
