package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"jira2gh/pkg/config"
	"net/http"
)

// ExtractJiraPRs assumes that epics automatically link to all the PRs of their respective linked issues
func ExtractJiraPRs(ctx context.Context, jira *config.JiraConfig, issueID string) (map[string]struct{}, error) {
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

	prs := map[string]struct{}{}
	for _, link := range remoteLinks {
		prs[link.Object.URL] = struct{}{}
	}

	return prs, nil
}
