//go:build extra
// +build extra

package extra

import (
	"testing"
)

func TestCleanText(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello world", "Hello world"},
		{"**Bold text**", "Bold text"},
		{"*Italic text*", "Italic text"},
		{"# Header", "Header"},
		{"_Underlined text_", "Underlined text"},
		{"~Strikethrough text~", "Strikethrough text"},
		{"`Code text`", "Code text"},
		{"[Link text](url)", "Link text(url)"},
		{"Mixed *markdown* and #headers#!", "Mixed markdown and headers"},
		{"<html>tags</html>", "tags"},
		{"|---|", ""}, // Table separator
		{"|====|", ""}, // Table separator with equals
		{"| - - - |", ""}, // Table separator with spaced dashes
		{"| cell1 | cell2 |", "cell1  cell2"}, // Table row with content
		{"  Trailing spaces  ", "Trailing spaces"},
		{"", ""},
		{"***", ""},
	}

	for _, test := range tests {
		result := cleanText(test.input)
		if result != test.expected {
			t.Errorf("cleanText(%q) = %q; expected %q", test.input, result, test.expected)
		}
	}
}