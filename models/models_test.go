package models

import (
	"strings"
	"testing"
)

func TestRoleMsgToTextWithImages(t *testing.T) {
	tests := []struct {
		name     string
		msg      RoleMsg
		index    int
		expected string // substring to check
	}{
		{
			name:  "text and image",
			index: 0,
			msg: func() RoleMsg {
				msg := NewMultimodalMsg("user", []interface{}{})
				msg.AddTextPart("Look at this picture")
				msg.AddImagePart("data:image/jpeg;base64,abc123", "/home/user/Pictures/cat.jpg")
				return msg
			}(),
			expected: "[orange::i][image: /home/user/Pictures/cat.jpg][-:-:-]",
		},
		{
			name:  "image only",
			index: 1,
			msg: func() RoleMsg {
				msg := NewMultimodalMsg("user", []interface{}{})
				msg.AddImagePart("data:image/png;base64,xyz789", "/tmp/screenshot_20250217_123456.png")
				return msg
			}(),
			expected: "[orange::i][image: /tmp/screenshot_20250217_123456.png][-:-:-]",
		},
		{
			name:  "long filename truncated",
			index: 2,
			msg: func() RoleMsg {
				msg := NewMultimodalMsg("user", []interface{}{})
				msg.AddTextPart("Check this")
				msg.AddImagePart("data:image/jpeg;base64,foo", "/very/long/path/to/a/really_long_filename_that_exceeds_forty_characters.jpg")
				return msg
			}(),
			expected: "[orange::i][image: .../to/a/really_long_filename_that_exceeds_forty_characters.jpg][-:-:-]",
		},
		{
			name:  "multiple images",
			index: 3,
			msg: func() RoleMsg {
				msg := NewMultimodalMsg("user", []interface{}{})
				msg.AddTextPart("Multiple images")
				msg.AddImagePart("data:image/jpeg;base64,a", "/path/img1.jpg")
				msg.AddImagePart("data:image/png;base64,b", "/path/img2.png")
				return msg
			}(),
			expected: "[orange::i][image: /path/img1.jpg][-:-:-]\n[orange::i][image: /path/img2.png][-:-:-]",
		},
		{
			name:  "old format without path",
			index: 4,
			msg: RoleMsg{
				Role:            "user",
				hasContentParts: true,
				ContentParts: []interface{}{
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:image/jpeg;base64,old",
						},
					},
				},
			},
			expected: "[orange::i][image: image][-:-:-]",
		},
		{
			name:  "old format with path",
			index: 5,
			msg: RoleMsg{
				Role:            "user",
				hasContentParts: true,
				ContentParts: []interface{}{
					map[string]interface{}{
						"type": "image_url",
						"path": "/old/path/photo.jpg",
						"image_url": map[string]interface{}{
							"url": "data:image/jpeg;base64,old",
						},
					},
				},
			},
			expected: "[orange::i][image: /old/path/photo.jpg][-:-:-]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.msg.ToText(tt.index)
			if !strings.Contains(result, tt.expected) {
				t.Errorf("ToText() result does not contain expected indicator\ngot: %s\nwant substring: %s", result, tt.expected)
			}
			// Ensure the indicator appears before text content
			if strings.Contains(tt.expected, "cat.jpg") && strings.Contains(result, "Look at this picture") {
				indicatorPos := strings.Index(result, "[orange::i][image: /home/user/Pictures/cat.jpg][-:-:-]")
				textPos := strings.Index(result, "Look at this picture")
				if indicatorPos == -1 || textPos == -1 || indicatorPos >= textPos {
					t.Errorf("image indicator should appear before text")
				}
			}
		})
	}
}

func TestExtractDisplayPath(t *testing.T) {
	// Save original base dir
	originalBaseDir := imageBaseDir
	defer func() { imageBaseDir = originalBaseDir }()

	tests := []struct {
		name     string
		baseDir  string
		path     string
		expected string
	}{
		{
			name:     "no base dir shows full path",
			baseDir:  "",
			path:     "/home/user/images/cat.jpg",
			expected: "/home/user/images/cat.jpg",
		},
		{
			name:     "relative path within base dir",
			baseDir:  "/home/user",
			path:     "/home/user/images/cat.jpg",
			expected: "images/cat.jpg",
		},
		{
			name:     "path outside base dir shows full path",
			baseDir:  "/home/user",
			path:     "/tmp/test.jpg",
			expected: "/tmp/test.jpg",
		},
		{
			name:     "same directory",
			baseDir:  "/home/user/images",
			path:     "/home/user/images/cat.jpg",
			expected: "cat.jpg",
		},
		{
			name:     "long path truncated",
			baseDir:  "",
			path:     "/very/long/path/to/a/really_long_filename_that_exceeds_sixty_characters_limit_yes_it_is_very_long.jpg",
			expected: "..._that_exceeds_sixty_characters_limit_yes_it_is_very_long.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageBaseDir = tt.baseDir
			result := extractDisplayPath(tt.path)
			if result != tt.expected {
				t.Errorf("extractDisplayPath(%q) with baseDir=%q = %q, want %q",
					tt.path, tt.baseDir, result, tt.expected)
			}
		})
	}
}
