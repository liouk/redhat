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
	URL         string
	Title       string
	Author      string
	State       string
	JiraFeature string
	JiraEpic    string
	JiraIssue   string
	ItemID      string
}

func (pr *PR) Metadata() map[string]string {
	metadata := map[string]string{}
	if pr.JiraFeature != "" {
		metadata["Jira Feature"] = pr.JiraFeature
	}
	if pr.JiraEpic != "" {
		metadata["Jira Epic"] = pr.JiraEpic
	}
	if pr.JiraIssue != "" {
		metadata["Jira Issue"] = pr.JiraIssue
	}
	return metadata
}

func (pr *PR) String() string {
	if pr.JiraFeature != "" {
		return fmt.Sprintf("Feature: %-20s Epic: %-20s Issue: %-20s URL: %s", pr.JiraFeature, pr.JiraEpic, pr.JiraIssue, pr.URL)
	}
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

	var feature string
	var epic string
	var issuesToScrape []string
	// Map to track epic for each issue
	issueEpicMap := make(map[string]string)

	switch issueType.Fields.IssueType.Name {
	case "Feature":
		feature = issueID
		// Features can contain Epics and Issues
		// Get all child items (Epics and Issues) linked to this Feature
		childItems, err := getFeatureChildItems(ctx, jira, issueID)
		if err != nil {
			return nil, err
		}
		// Include the Feature itself to capture PRs linked directly to it
		issuesToScrape = append([]string{issueID}, childItems...)
		issueEpicMap[issueID] = "" // Feature itself has no epic

		// For each child item, if it's an Epic, also get its linked issues
		for _, item := range childItems {
			itemType, itemEpic, err := getIssueTypeAndEpic(ctx, jira, item)
			if err != nil {
				return nil, err
			}

			if itemType == "Epic" {
				// This child is an Epic
				issueEpicMap[item] = item
				linkedIssues, err := getEpicLinkedIssues(ctx, jira, item)
				if err != nil {
					return nil, err
				}
				// Add all issues linked to this Epic
				for _, linkedIssue := range linkedIssues {
					issuesToScrape = append(issuesToScrape, linkedIssue)
					issueEpicMap[linkedIssue] = item
				}
			} else {
				// Regular issue under Feature
				issueEpicMap[item] = itemEpic
			}
		}
	case "Epic":
		epic = issueID
		// Get all issues linked to this epic
		linkedIssues, err := getEpicLinkedIssues(ctx, jira, issueID)
		if err != nil {
			return nil, err
		}
		// Include the Epic itself to capture PRs linked directly to it
		issuesToScrape = append([]string{issueID}, linkedIssues...)
		for _, issue := range issuesToScrape {
			issueEpicMap[issue] = epic
		}
	default:
		epic = issueType.Fields.ParentEpic
		issuesToScrape = []string{issueID}
		issueEpicMap[issueID] = epic
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
				URL:         url,
				Title:       link.Object.Title,
				JiraIssue:   issue,
				JiraEpic:    issueEpicMap[issue],
				JiraFeature: feature,
			}
		}
	}

	return prs, nil
}

func getFeatureChildItems(ctx context.Context, jira *config.JiraConfig, featureID string) ([]string, error) {
	// Use JQL to find all Epics and Issues linked to this Feature
	// This searches for items where the parent is the Feature
	jql := fmt.Sprintf("parent = %s", featureID)

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

	items := make([]string, len(searchResult.Issues))
	for i, issue := range searchResult.Issues {
		items[i] = issue.Key
	}

	return items, nil
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

func getIssueTypeAndEpic(ctx context.Context, jira *config.JiraConfig, issueID string) (string, string, error) {
	respBody, err := jiraRequest(ctx, jira, fmt.Sprintf("rest/api/2/issue/%s", issueID))
	if err != nil {
		return "", "", err
	}

	var issueData struct {
		Fields struct {
			IssueType struct {
				Name string `json:"name"`
			} `json:"issuetype"`
			ParentEpic string `json:"customfield_12311140"`
		} `json:"fields"`
	}

	if err := json.Unmarshal(respBody, &issueData); err != nil {
		return "", "", fmt.Errorf("failed to parse response: %w", err)
	}

	return issueData.Fields.IssueType.Name, issueData.Fields.ParentEpic, nil
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
