package models

import (
	"regexp"
	"strings"
)

type AudioFormat string

const (
	AFWav AudioFormat = "wav"
	AFMP3 AudioFormat = "mp3"
)

var threeOrMoreDashesRE = regexp.MustCompile(`-{3,}`)

// CleanText removes markdown and special characters that are not suitable for TTS
func CleanText(text string) string {
	// Remove markdown-like characters that might interfere with TTS
	text = strings.ReplaceAll(text, "*", "") // Bold/italic markers
	text = strings.ReplaceAll(text, "#", "") // Headers
	text = strings.ReplaceAll(text, "_", "") // Underline/italic markers
	text = strings.ReplaceAll(text, "~", "") // Strikethrough markers
	text = strings.ReplaceAll(text, "`", "") // Code markers
	text = strings.ReplaceAll(text, "[", "") // Link brackets
	text = strings.ReplaceAll(text, "]", "") // Link brackets
	text = strings.ReplaceAll(text, "!", "") // Exclamation marks (if not punctuation)
	// Remove HTML tags using regex
	htmlTagRegex := regexp.MustCompile(`<[^>]*>`)
	text = htmlTagRegex.ReplaceAllString(text, "")
	// Split text into lines to handle table separators
	lines := strings.Split(text, "\n")
	var filteredLines []string
	for _, line := range lines {
		// Check if the line looks like a table separator (e.g., |----|, |===|, | - - - |)
		// A table separator typically contains only |, -, =, and spaces
		isTableSeparator := regexp.MustCompile(`^\s*\|\s*[-=\s]+\|\s*$`).MatchString(strings.TrimSpace(line))
		if !isTableSeparator {
			// If it's not a table separator, remove vertical bars but keep the content
			processedLine := strings.ReplaceAll(line, "|", "")
			filteredLines = append(filteredLines, processedLine)
		}
		// If it is a table separator, skip it (don't add to filteredLines)
	}
	text = strings.Join(filteredLines, "\n")
	text = threeOrMoreDashesRE.ReplaceAllString(text, "")
	text = strings.TrimSpace(text) // Remove leading/trailing whitespace
	return text
}
