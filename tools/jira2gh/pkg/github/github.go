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
	if proj.AssignFieldValue != nil {
		projID, err := ghGetProjectID(ctx, proj)
		if err != nil {
			return err
		}
		proj.GitHubProjectID = projID

		fieldID, valueID, err := ghGetFieldValue(ctx, proj)
		if err != nil {
			return err
		}
		proj.AssignFieldValue.FieldID = fieldID
		proj.AssignFieldValue.ValueID = valueID
	}

	config.Printf("\nAdding %d PRs to GitHub project %s/%s...\n", len(prs), proj.GitHubOwner, proj.GitHubProject)

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
				err := ghItemAdd(ctx, proj, prURL)
				if err != nil {
					config.Println()
					return err
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
		err := ghItemAdd(ctx, proj, prURL)
		if err != nil {
			config.Println()
			return err
		}
		config.Println("ok")
	}

	config.Printf("\nSuccessfully added all %d PRs to the project!\n", len(prs))
	return nil
}

func ghItemAdd(ctx context.Context, proj *config.ProjectConfig, prURL string) error {
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

	if proj.AssignFieldValue != nil {
		return ghItemEdit(ctx, proj, item["id"].(string))
	}

	return nil
}

func ghItemEdit(ctx context.Context, proj *config.ProjectConfig, itemID string) error {
	if DryRun {
		return nil
	}

	_, err := exec.
		CommandContext(ctx, "gh", "project", "item-edit",
			"--project-id", proj.GitHubProjectID,
			"--id", itemID,
			"--field-id", proj.AssignFieldValue.FieldID,
			"--single-select-option-id", proj.AssignFieldValue.ValueID,
		).CombinedOutput()

	return err
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

func ghGetFieldValue(ctx context.Context, proj *config.ProjectConfig) (string, string, error) {
	type ghFields struct {
		Fields []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Options []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"options"`
		} `json:"fields"`
	}

	out, err := exec.CommandContext(ctx, "gh", "project",
		"field-list", proj.GitHubProject,
		"--owner", proj.GitHubOwner,
		"--format", "json",
	).CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("failed to get project ID: %v", err)
	}

	var fields ghFields
	if err := json.Unmarshal(out, &fields); err != nil {
		return "", "", fmt.Errorf("could not unmarshal response from json: %v", err)
	}

	var fieldID, valueID string

	for _, field := range fields.Fields {
		if field.Name == proj.AssignFieldValue.Field {
			fieldID = field.ID

			for _, value := range field.Options {
				if value.Name == proj.AssignFieldValue.Value {
					valueID = value.ID
					break
				}
			}

			break
		}
	}

	if len(fieldID) == 0 {
		return "", "", fmt.Errorf("field %s not found in project %s", proj.AssignFieldValue.Field, proj.GitHubProject)
	}

	if len(valueID) == 0 {
		return "", "", fmt.Errorf("value %s not found in project %s", proj.AssignFieldValue.Value, proj.GitHubProject)
	}

	return fieldID, valueID, nil
}

func ghGetItemID(ctx context.Context, proj *config.ProjectConfig) (string, error) {
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
