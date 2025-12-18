package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatSearchResults takes raw JSON from websearch and returns a concise summary.
func FormatSearchResults(rawJSON []byte) (string, error) {
	// Try to unmarshal as generic slice of maps
	var results []map[string]interface{}
	if err := json.Unmarshal(rawJSON, &results); err != nil {
		// If that fails, try as a single map (maybe wrapper object)
		var wrapper map[string]interface{}
		if err2 := json.Unmarshal(rawJSON, &wrapper); err2 == nil {
			// Look for a "results" or "data" field
			if data, ok := wrapper["results"].([]interface{}); ok {
				// Convert to slice of maps
				for _, item := range data {
					if m, ok := item.(map[string]interface{}); ok {
						results = append(results, m)
					}
				}
			} else if data, ok := wrapper["data"].([]interface{}); ok {
				for _, item := range data {
					if m, ok := item.(map[string]interface{}); ok {
						results = append(results, m)
					}
				}
			} else {
				// No slice found, treat wrapper as single result
				results = []map[string]interface{}{wrapper}
			}
		} else {
			return "", fmt.Errorf("failed to unmarshal search results: %v (also %v)", err, err2)
		}
	}

	if len(results) == 0 {
		return "No search results found.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d results:\n", len(results)))
	for i, r := range results {
		// Extract common fields
		title := getString(r, "title", "Title", "name", "heading")
		snippet := getString(r, "snippet", "description", "content", "body", "text", "summary")
		url := getString(r, "url", "link", "uri", "source")

		sb.WriteString(fmt.Sprintf("%d. ", i+1))
		if title != "" {
			sb.WriteString(fmt.Sprintf("**%s**", title))
		} else {
			sb.WriteString("(No title)")
		}
		if snippet != "" {
			// Truncate snippet to reasonable length
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf(" — %s", snippet))
		}
		if url != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", url))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// FormatWebPageContent takes raw JSON from read_url and returns a concise summary.
func FormatWebPageContent(rawJSON []byte) (string, error) {
	// Try to unmarshal as generic map
	var data map[string]interface{}
	if err := json.Unmarshal(rawJSON, &data); err != nil {
		// If that fails, try as string directly
		var content string
		if err2 := json.Unmarshal(rawJSON, &content); err2 == nil {
			return truncateText(content, 500), nil
		}
		// Both failed, return first error
		return "", fmt.Errorf("failed to unmarshal web page content: %v", err)
	}

	// Look for common content fields
	content := getString(data, "content", "text", "body", "article", "html", "markdown", "data")
	if content == "" {
		// If no content field, marshal the whole thing as a short string
		summary := fmt.Sprintf("%v", data)
		return truncateText(summary, 300), nil
	}
	return truncateText(content, 500), nil
}

// Helper to get a string value from a map, trying multiple keys.
func getString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if val, ok := m[k]; ok {
			switch v := val.(type) {
			case string:
				return v
			case fmt.Stringer:
				return v.String()
			default:
				return fmt.Sprintf("%v", v)
			}
		}
	}
	return ""
}

// Helper to truncate text and add ellipsis.
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}