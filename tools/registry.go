package tools

import (
	"fmt"
	"sort"
	"strings"

	"gf-lt/models"
)

// This file is the single source of truth for "what can the agent do right now?".
//
// The tool set is assembled from several places (the FnMap literal, Init() adding
// `memory` when enabled, RegisterMissionTools(), removeBrowserTools(), window
// tool registration, MCP servers). Answering that question previously required
// reading all of them and re-deriving the conditionals by hand, which is exactly
// the kind of thing an agent should be able to just ask. The accessors below make
// it answerable, and RegistryIssues() makes drift detectable in a test.

// AvailableTools returns the sorted set of tool names that currently have a
// handler registered in FnMap. This is the live set, not the advertised one.
func AvailableTools() []string {
	names := make([]string, 0, len(FnMap))
	for name := range FnMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AvailableToolCount returns the number of live tools.
func AvailableToolCount() int { return len(FnMap) }

// internalCount returns how many live handlers are application-models.Internal.
func internalCount() int {
	n := 0
	for name := range FnMap {
		if internalTools[name] {
			n++
		}
	}
	return n
}

// ToolSchemas returns the OpenAI-style tool schemas currently advertised to the
// model (BaseTools + MissionBaseTools).
func ToolSchemas() []models.Tool {
	out := make([]models.Tool, 0, len(BaseTools)+len(MissionBaseTools))
	out = append(out, BaseTools...)
	out = append(out, MissionBaseTools...)
	return out
}

// ToolSchemaNames returns the sorted set of advertised tool schema names.
func ToolSchemaNames() []string {
	names := make([]string, 0)
	for _, t := range ToolSchemas() {
		names = append(names, t.Function.Name)
	}
	sort.Strings(names)
	return names
}

// ToolDescription returns the advertised description of a tool, or "" if the
// tool has no schema.
func ToolDescription(name string) string {
	for _, t := range ToolSchemas() {
		if t.Function.Name == name {
			return t.Function.Description
		}
	}
	return ""
}

// internalTools are callable through CallToolWithAgent but are not advertised to
// the model: they exist for application code to invoke, not for the LLM to
// discover. They are exempt from the advertised/handler consistency check.
var internalTools = map[string]bool{
	// summarize_chat is a stub handler driven by bot.go (summarizeAndStartNewChat)
	// through the summarize_chat agent. The bare handler only echoes its args back,
	// so advertising it would be worse than not advertising it.
	"summarize_chat": true,
}

// RegistryIssues reports inconsistencies between the handler set (FnMap) and the
// advertised schema set (BaseTools + MissionBaseTools):
//
//   - "advertised but no handler": the model is told about a tool whose handler
//     does not exist, so the call fails at run time
//   - "handler but not advertised": a working tool the model never learns about
//   - "duplicate schema": the same tool name is advertised twice
//
// Returns nil when the registry is consistent. Wired into the test suite.
func RegistryIssues() []string {
	var issues []string
	handlers := AvailableTools()
	schemas := ToolSchemaNames()

	handlerSet := make(map[string]bool, len(handlers))
	for _, h := range handlers {
		handlerSet[h] = true
	}
	schemaSet := make(map[string]bool, len(schemas))
	for _, s := range schemas {
		if schemaSet[s] {
			issues = append(issues, fmt.Sprintf("duplicate schema: %s", s))
		}
		schemaSet[s] = true
	}
	for _, s := range schemas {
		if !handlerSet[s] {
			issues = append(issues, fmt.Sprintf("advertised but no handler: %s", s))
		}
	}
	for _, h := range handlers {
		if internalTools[h] {
			continue
		}
		if !schemaSet[h] {
			issues = append(issues, fmt.Sprintf("handler but not advertised: %s", h))
		}
	}
	sort.Strings(issues)
	return issues
}

// ToolsHelp renders a self-describing listing of the live tool set: one line per
// tool, with its description. This is the thing an agent should be able to call
// once at session start to bootstrap, so it is deliberately compact and stable.
func ToolsHelp() string {
	issues := RegistryIssues()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Available tools (%d):\n", len(FnMap)-internalCount()))
	for _, name := range AvailableTools() {
		if internalTools[name] {
			continue // application-invoked, not yours to call
		}
		desc := ToolDescription(name)
		if desc == "" {
			sb.WriteString(fmt.Sprintf("  %-20s (no description)\n", name))
			continue
		}
		// Keep it to one line per tool; descriptions can be multi-sentence.
		if i := strings.IndexByte(desc, '\n'); i >= 0 {
			desc = desc[:i]
		}
		if len(desc) > 110 {
			desc = desc[:107] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", name, desc))
	}
	if len(issues) > 0 {
		sb.WriteString("\nregistry warnings (registry drift, not your problem):\n")
		for _, issue := range issues {
			sb.WriteString("  ! " + issue + "\n")
		}
	}
	return sb.String()
}
