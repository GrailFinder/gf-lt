package rag

import (
	"testing"
)

func TestDetectPhrases(t *testing.T) {
	tests := []struct {
		query  string
		expect []string
	}{
		{
			query:  "bald prophet and two she bears",
			expect: []string{"bald prophet", "two she", "two she bears", "she bears"},
		},
		{
			query:  "she bears",
			expect: []string{"she bears"},
		},
		{
			query:  "the quick brown fox",
			expect: []string{"quick brown", "quick brown fox", "brown fox"},
		},
		{
			query:  "in the house", // stop words
			expect: []string{},     // "in" and "the" are stop words
		},
		{
			query:  "a", // short
			expect: []string{},
		},
	}
	for _, tt := range tests {
		got := detectPhrases(tt.query)
		if len(got) != len(tt.expect) {
			t.Errorf("detectPhrases(%q) = %v, want %v", tt.query, got, tt.expect)
			continue
		}
		for i := range got {
			if got[i] != tt.expect[i] {
				t.Errorf("detectPhrases(%q) = %v, want %v", tt.query, got, tt.expect)
				break
			}
		}
	}
}

func TestCountPhraseMatches(t *testing.T) {
	tests := []struct {
		text   string
		query  string
		expect int
	}{
		{
			text:   "two she bears came out of the wood",
			query:  "she bears",
			expect: 1,
		},
		{
			text:   "bald head and she bears",
			query:  "bald prophet and two she bears",
			expect: 1, // only "she bears" matches
		},
		{
			text:   "no match here",
			query:  "she bears",
			expect: 0,
		},
		{
			text:   "she bears and bald prophet",
			query:  "bald prophet she bears",
			expect: 2, // "she bears" and "bald prophet"
		},
	}
	for _, tt := range tests {
		got := countPhraseMatches(tt.text, tt.query)
		if got != tt.expect {
			t.Errorf("countPhraseMatches(%q, %q) = %d, want %d", tt.text, tt.query, got, tt.expect)
		}
	}
}

func TestAreSlugsAdjacent(t *testing.T) {
	tests := []struct {
		slug1  string
		slug2  string
		expect bool
	}{
		{
			slug1:  "kjv_bible.epub_1786_0",
			slug2:  "kjv_bible.epub_1787_0",
			expect: true,
		},
		{
			slug1:  "kjv_bible.epub_1787_0",
			slug2:  "kjv_bible.epub_1786_0",
			expect: true,
		},
		{
			slug1:  "kjv_bible.epub_1786_0",
			slug2:  "kjv_bible.epub_1788_0",
			expect: false,
		},
		{
			slug1:  "otherfile.txt_1_0",
			slug2:  "kjv_bible.epub_1786_0",
			expect: false,
		},
		{
			slug1:  "file_1_0",
			slug2:  "file_1_1",
			expect: true,
		},
		{
			slug1:  "file_1_0",
			slug2:  "file_2_0", // different batch
			expect: true,       // sequential batches with same chunk index are adjacent
		},
	}
	for _, tt := range tests {
		got := areSlugsAdjacent(tt.slug1, tt.slug2)
		if got != tt.expect {
			t.Errorf("areSlugsAdjacent(%q, %q) = %v, want %v", tt.slug1, tt.slug2, got, tt.expect)
		}
	}
}

func TestParseSlugIndices(t *testing.T) {
	tests := []struct {
		slug      string
		wantBatch int
		wantChunk int
		wantOk    bool
	}{
		{"kjv_bible.epub_1786_0", 1786, 0, true},
		{"file_1_5", 1, 5, true},
		{"no_underscore", 0, 0, false},
		{"file_abc_def", 0, 0, false},
		{"file_123_456_extra", 456, 0, false}, // regex matches last two numbers
	}
	for _, tt := range tests {
		batch, chunk, ok := parseSlugIndices(tt.slug)
		if ok != tt.wantOk {
			t.Errorf("parseSlugIndices(%q) ok = %v, want %v", tt.slug, ok, tt.wantOk)
			continue
		}
		if ok && (batch != tt.wantBatch || chunk != tt.wantChunk) {
			t.Errorf("parseSlugIndices(%q) = (%d, %d), want (%d, %d)", tt.slug, batch, chunk, tt.wantBatch, tt.wantChunk)
		}
	}
}
