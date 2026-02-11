package github

import (
	"context"
	"encoding/json"
	"fmt"
	"jira2gh/pkg/config"
	"jira2gh/pkg/jira"
	"os/exec"
	"strings"
)

var (
	DryRun   bool
	fieldIDs = map[string]string{}
)

func FetchGitHubPRs(ctx context.Context, proj *config.ProjectConfig) (map[string]jira.PR, error) {
	// Run gh CLI to fetch project items
	cmd := exec.CommandContext(ctx, "gh", "project", "item-list", proj.GitHubProject, "--owner", proj.GitHubOwner, "--format", "json", "--limit", "1000")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("failed to fetch GitHub project items: %w\nstderr: %s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to fetch GitHub project items: %w", err)
	}

	var response struct {
		Items []struct {
			ID      string `json:"id"`
			Content struct {
				URL string `json:"url"`
			} `json:"content"`
			FieldValues struct {
				Nodes []struct {
					Field struct {
						Name string `json:"name"`
					} `json:"field"`
					Text string `json:"text"`
				} `json:"nodes"`
			} `json:"fieldValues"`
		} `json:"items"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub project items: %w", err)
	}

	// Extract PRs with metadata
	prs := map[string]jira.PR{}
	for _, item := range response.Items {
		if item.Content.URL == "" {
			continue
		}

		pr := jira.PR{
			URL:    item.Content.URL,
			ItemID: item.ID,
		}

		// Extract custom field values
		for _, fieldValue := range item.FieldValues.Nodes {
			switch fieldValue.Field.Name {
			case "Jira Feature":
				pr.JiraFeature = fieldValue.Text
			case "Jira Epic":
				pr.JiraEpic = fieldValue.Text
			case "Jira Issue":
				pr.JiraIssue = fieldValue.Text
			}
		}

		prs[pr.URL] = pr
	}

	return prs, nil
}

func AddToProject(ctx context.Context, proj *config.ProjectConfig, prs []jira.PR) error {
	prWord := "PRs"
	if len(prs) == 1 {
		prWord = "PR"
	}
	config.Printf("\nSyncing %s to project %s/%s...\n", prWord, proj.GitHubOwner, proj.GitHubProject)

	projID, err := ghGetProjectID(ctx, proj)
	if err != nil {
		return fmt.Errorf("could not get project ID: %v", err)
	}
	proj.GitHubProjectID = projID

	for _, pr := range prs {
		// Parse URL to get short format: owner/repo#number
		shortPR := formatPRShort(pr.URL)
		err := ghItemAdd(ctx, proj, pr.URL, pr.Metadata())
		if err != nil {
			return err
		}
		config.Printf("  ✓ %s\n", shortPR)
	}

	config.Printf("\n✓ Successfully added %d %s to the project!\n", len(prs), prWord)
	return nil
}

func RemoveFromProject(ctx context.Context, proj *config.ProjectConfig, prs []jira.PR) error {
	prWord := "PRs"
	if len(prs) == 1 {
		prWord = "PR"
	}
	config.Printf("\nRemoving %s from project %s/%s...\n", prWord, proj.GitHubOwner, proj.GitHubProject)

	for _, pr := range prs {
		shortPR := formatPRShort(pr.URL)
		if DryRun {
			config.Printf("  ✓ %s (dry-run)\n", shortPR)
			continue
		}

		_, err := exec.CommandContext(ctx, "gh", "project",
			"item-delete", proj.GitHubProject,
			"--owner", proj.GitHubOwner,
			"--id", pr.ItemID,
		).CombinedOutput()

		if err != nil {
			return fmt.Errorf("failed to remove %s: %v", shortPR, err)
		}
		config.Printf("  ✓ %s\n", shortPR)
	}

	config.Printf("\n✓ Successfully removed %d %s from the project!\n", len(prs), prWord)
	return nil
}

func formatPRShort(url string) string {
	// URL format: https://github.com/owner/repo/pull/number
	parts := strings.Split(url, "/")
	if len(parts) >= 7 && parts[2] == "github.com" && parts[5] == "pull" {
		return parts[3] + "/" + parts[4] + "#" + parts[6]
	}
	return url
}

// FetchPRDetails fetches PR details (author, state) from GitHub
func FetchPRDetails(ctx context.Context, prURL string) (author, state string, err error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prURL, "--json", "author,state")
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch PR details: %w", err)
	}

	var response struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		State string `json:"state"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return "", "", fmt.Errorf("failed to parse PR details: %w", err)
	}

	return response.Author.Login, response.State, nil
}

func ghItemAdd(ctx context.Context, proj *config.ProjectConfig, prURL string, metadata map[string]string) error {
	if DryRun {
		return nil
	}

	out, err := exec.CommandContext(ctx, "gh", "project",
		"item-add", proj.GitHubProject,
		"--owner", proj.GitHubOwner,
		"--url", prURL,
		"--format", "json",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add item to project: %v", err)
	}

	item := map[string]any{}
	if err := json.Unmarshal(out, &item); err != nil {
		return fmt.Errorf("could not unmarshal response from json: %v", err)
	}

	itemID := item["id"].(string)
	for key, value := range metadata {
		if len(key) == 0 || len(value) == 0 {
			continue
		}

		if _, found := fieldIDs[key]; !found {
			fieldID, err := ghGetFieldID(ctx, proj, key)
			if err != nil {
				// Skip fields that don't exist in the project
				config.Printf("  Warning: field '%s' not found in project, skipping\n", key)
				fieldIDs[key] = "" // Mark as checked but not found
				continue
			}
			fieldIDs[key] = fieldID
		}

		// Skip if field was previously not found
		if fieldIDs[key] == "" {
			continue
		}

		err := ghItemEdit(ctx, proj.GitHubProjectID, itemID, "text", fieldIDs[key], value)
		if err != nil {
			return fmt.Errorf("failed to edit item: %v", err)
		}
	}

	return nil
}

func ghGetProjectID(ctx context.Context, proj *config.ProjectConfig) (string, error) {
	type ghProject struct {
		ID string `json:"id"`
	}

	out, err := exec.CommandContext(ctx, "gh", "project",
		"view", proj.GitHubProject,
		"--owner", proj.GitHubOwner,
		"--format", "json",
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get project ID: %v", err)
	}

	var p ghProject
	if err := json.Unmarshal(out, &p); err != nil {
		return "", fmt.Errorf("could not unmarshal response from json: %v", err)
	}

	return p.ID, nil
}

// ghGetFieldID retrieves the field ID for a given field name from the GitHub project
func ghGetFieldID(ctx context.Context, proj *config.ProjectConfig, fieldName string) (string, error) {
	// Run gh CLI to fetch project fields
	cmd := exec.CommandContext(ctx, "gh", "project",
		"field-list", proj.GitHubProject,
		"--owner", proj.GitHubOwner,
		"--format", "json",
	)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("failed to fetch project fields: %w\nstderr: %s", err, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to fetch project fields: %w", err)
	}

	var response struct {
		Fields []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"fields"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return "", fmt.Errorf("failed to parse project fields: %w", err)
	}

	// Find the field with matching name
	for _, field := range response.Fields {
		if field.Name == fieldName {
			return field.ID, nil
		}
	}

	return "", fmt.Errorf("field '%s' not found in project", fieldName)
}

func ghItemEdit(ctx context.Context, projectID, itemID, fieldType, fieldID, value string) error {
	if DryRun {
		return nil
	}

	valueArg := ""
	switch fieldType {
	case "text":
		valueArg = "--text"
	case "option":
		valueArg = "--single-select-option-id"
	default:
		return fmt.Errorf("unknown field type: %s", fieldType)
	}

	_, err := exec.
		CommandContext(ctx, "gh", "project", "item-edit",
			"--project-id", projectID,
			"--id", itemID,
			"--field-id", fieldID,
			valueArg, value,
		).CombinedOutput()

	return err
}
