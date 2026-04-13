package models

import "regexp"

const (
	LoadedMark        = "(loaded) "
	ToolRespMultyType = "multimodel_content"
	DefaultFirstMsg   = "Hello! What can I do for you?"
	BasicSysMsg       = "Large Language Model that helps user with any of his requests."
)

type APIType int

const (
	APITypeChat APIType = iota
	APITypeCompletion
)

var (
	ToolCallRE       = regexp.MustCompile(`__tool_call__\s*([\s\S]*?)__tool_call__`)
	QuotesRE         = regexp.MustCompile(`(".*?")`)
	StarRE           = regexp.MustCompile(`(\*.*?\*)`)
	ThinkRE          = regexp.MustCompile(`(?s)<think>.*?</think>`)
	CodeBlockRE      = regexp.MustCompile(`(?s)\x60{3}(?:.*?)\n(.*?)\n\s*\x60{3}\s*`)
	CodeBlockLeftRE  = regexp.MustCompile(`(?s)\x60{3}(?:.*?)\n`)
	SingleBacktickRE = regexp.MustCompile(`\x60([^\x60]*)\x60`)
	RoleRE           = regexp.MustCompile(`^(\w+):`)
)

var (
	SysLabels = []string{"assistant"}
)
