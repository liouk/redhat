package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"jira2gh/pkg/config"
	"net/http"
	"net/url"
	"strings"
)

type PR struct {
	URL       string
	JiraEpic  string
	JiraIssue string
	ItemID    string
}

func (pr *PR) Metadata() map[string]string {
	return map[string]string{
		"Jira Epic":  pr.JiraEpic,
		"Jira Issue": pr.JiraIssue,
	}
}

func (pr *PR) String() string {
	return fmt.Sprintf("Epic: %-20s Issue: %-20s URL: %s", pr.JiraEpic, pr.JiraIssue, pr.URL)
}

// ExtractJiraPRs scrapes remote links from the epic's linked issues
func ExtractJiraPRs(ctx context.Context, jira *config.JiraConfig, issueID string) (map[string]PR, error) {
	respBody, err := jiraRequest(ctx, jira, fmt.Sprintf("rest/api/2/issue/%s", issueID))
	if err != nil {
		return nil, err
	}

	var issueType struct {
		Fields struct {
			IssueType struct {
				Name string `json:"name"`
			} `json:"issuetype"`
			ParentEpic string `json:"customfield_12311140"`
		} `json:"fields"`
	}

	if err := json.Unmarshal(respBody, &issueType); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var epic string
	var issuesToScrape []string

	switch issueType.Fields.IssueType.Name {
	case "Epic":
		epic = issueID
		// Get all issues linked to this epic
		linkedIssues, err := getEpicLinkedIssues(ctx, jira, issueID)
		if err != nil {
			return nil, err
		}
		issuesToScrape = linkedIssues
	default:
		epic = issueType.Fields.ParentEpic
		issuesToScrape = []string{issueID}
	}

	prs := map[string]PR{}
	for _, issue := range issuesToScrape {
		remoteLinks, err := getIssueRemoteLinks(ctx, jira, issue)
		if err != nil {
			return nil, err
		}

		for _, link := range remoteLinks {
			url := link.Object.URL
			if !strings.Contains(url, "github.com") || !strings.Contains(url, "/pull") {
				continue
			}
			prs[url] = PR{
				URL:       url,
				JiraIssue: issue,
				JiraEpic:  epic,
			}
		}
	}

	return prs, nil
}

func getEpicLinkedIssues(ctx context.Context, jira *config.JiraConfig, epicID string) ([]string, error) {
	// Use JQL to find all issues linked to this epic
	jql := fmt.Sprintf("\"Epic Link\" = %s", epicID)

	baseURL, err := url.JoinPath(jira.Host, "rest/api/2/search")
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("jql", jql)
	q.Set("fields", "key")
	u.RawQuery = q.Encode()

	respBody, err := jiraRequestURL(ctx, jira, u.String())
	if err != nil {
		return nil, err
	}

	var searchResult struct {
		Issues []struct {
			Key string `json:"key"`
		} `json:"issues"`
	}

	if err := json.Unmarshal(respBody, &searchResult); err != nil {
		return nil, fmt.Errorf("failed to parse search result: %w", err)
	}

	issues := make([]string, len(searchResult.Issues))
	for i, issue := range searchResult.Issues {
		issues[i] = issue.Key
	}

	return issues, nil
}

func getIssueRemoteLinks(ctx context.Context, jira *config.JiraConfig, issueID string) ([]struct {
	ID     int    `json:"id"`
	Self   string `json:"self"`
	Object struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"object"`
}, error) {
	respBody, err := jiraRequest(ctx, jira, fmt.Sprintf("rest/api/2/issue/%s/remotelink", issueID))
	if err != nil {
		return nil, err
	}

	var remoteLinks []struct {
		ID     int    `json:"id"`
		Self   string `json:"self"`
		Object struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"object"`
	}

	if err := json.Unmarshal(respBody, &remoteLinks); err != nil {
		return nil, fmt.Errorf("failed to parse remote links: %w", err)
	}

	return remoteLinks, nil
}

func jiraRequest(ctx context.Context, jira *config.JiraConfig, path string) ([]byte, error) {
	url, err := url.JoinPath(jira.Host, path)
	if err != nil {
		return nil, err
	}
	return jiraRequestURL(ctx, jira, url)
}

func jiraRequestURL(ctx context.Context, jira *config.JiraConfig, url string) ([]byte, error) {
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

	return body, nil
}
