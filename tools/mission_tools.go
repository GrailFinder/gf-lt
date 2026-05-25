package tools

import (
	"encoding/json"
	"fmt"
	"gf-lt/agent"
	"gf-lt/config"
	"gf-lt/mission"
	"gf-lt/models"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	currentMission    *mission.Mission
	pmAgent           *agent.AgentClient
	MissionBaseTools []models.Tool
)

const pmSystemPrompt = `You are the Project Manager for an autonomous coding agent solving a software issue. The agent has full access to bash, git, file editing, and other tools. It should create feature branches, commit incrementally, and write/run tests.

Your job is to give concise, actionable guidance. Focus on these areas:

- **Task alignment**: Is the agent working toward the acceptance criteria or going off-track?
- **Progress**: Is it making forward progress or spinning on the same problem?
- **Error handling**: Has it recovered from failures? If the same mistake keeps repeating, flag it.
- **Scope**: Is it staying focused or adding unrelated changes?

Keep responses brief — a few sentences, not paragraphs. The agent needs clear direction, not encouragement. If things look fine, just say so and let it continue.`

func SetCurrentMission(m *mission.Mission) {
	currentMission = m
}

func IsMissionMode() bool {
	return currentMission != nil
}

func GetCurrentMission() *mission.Mission {
	return currentMission
}

func RegisterMissionTools() {
	FnMap["move_issue"] = moveIssueTool
	FnMap["create_issue"] = createIssueTool
	FnMap["create_pr"] = createPRTool
	FnMap["pm_consult"] = pmConsultTool
	FnMap["add_issue_comment"] = addIssueCommentTool

	MissionBaseTools = []models.Tool{
		{
			Type: "function",
			Function: models.ToolFunc{
				Name:        "move_issue",
				Description: "Move the current issue to a different status (review, done, archive). Example: move_issue status=review",
				Parameters: models.ToolFuncParams{
					Type:     "object",
					Required: []string{"status"},
					Properties: map[string]models.ToolArgProps{
						"status": {Type: "string", Description: "Target status: review, done, or archive"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: models.ToolFunc{
				Name:        "create_issue",
				Description: "Create a new sub-issue for related work. Example: create_issue id=99 title='Fix login timeout' description='The login page times out after 30s'",
				Parameters: models.ToolFuncParams{
					Type:     "object",
					Required: []string{"id", "title", "description"},
					Properties: map[string]models.ToolArgProps{
						"id":          {Type: "string", Description: "Issue ID (numeric string)"},
						"title":       {Type: "string", Description: "Issue title"},
						"description": {Type: "string", Description: "Issue description"},
						"branch_name": {Type: "string", Description: "Branch name for this sub-issue (optional)"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: models.ToolFunc{
				Name:        "create_pr",
				Description: "Mark the current issue as complete by creating a pull/merge request. This signals mission completion. Writes a .gf-lt-pr.md file to the project repo.",
				Parameters: models.ToolFuncParams{
					Type:     "object",
					Required: []string{"title"},
					Properties: map[string]models.ToolArgProps{
						"title": {Type: "string", Description: "PR title"},
						"body":  {Type: "string", Description: "PR body/description (markdown)"},
						"base":  {Type: "string", Description: "Base branch (default: main)"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: models.ToolFunc{
				Name:        "pm_consult",
				Description: "Request guidance from the project manager. Use when stuck, need direction, or want feedback on approach. Example: pm_consult question='Should I focus on tests or documentation?'",
				Parameters: models.ToolFuncParams{
					Type:     "object",
					Required: []string{},
					Properties: map[string]models.ToolArgProps{
						"question": {Type: "string", Description: "Your question or what you need guidance on"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: models.ToolFunc{
				Name:        "add_issue_comment",
				Description: "Add a comment to the issue file for tracking progress. Example: add_issue_comment body='Completed user login implementation'",
				Parameters: models.ToolFuncParams{
					Type:     "object",
					Required: []string{"body"},
					Properties: map[string]models.ToolArgProps{
						"body":   {Type: "string", Description: "Comment text"},
						"author": {Type: "string", Description: "Comment author (default: solver)"},
					},
				},
			},
		},
	}
}

func InitPMAgent(cfg *config.Config, log *slog.Logger) {
	getToken := func() string {
		if getTokenFunc != nil {
			return getTokenFunc()
		}
		return ""
	}
	pmAgent = agent.NewAgentClient(cfg, log, getToken)
}

func pmAgentChat(userMsg string) string {
	if pmAgent == nil {
		return "PM agent not initialized"
	}
	body, err := pmAgent.FormFirstMsg(pmSystemPrompt, userMsg)
	if err != nil {
		currentMission.Log("PM agent error: failed to form message: %v", err)
		return fmt.Sprintf("PM agent error: %v", err)
	}
	resp, err := pmAgent.LLMRequest(body)
	if err != nil {
		currentMission.Log("PM agent error: request failed: %v", err)
		return fmt.Sprintf("PM agent error: %v", err)
	}
	text := strings.TrimSpace(string(resp))
	if text == "" {
		currentMission.Log("PM agent returned empty response, using fallback")
		return "No guidance available from PM check-in. Continue with the current approach, review acceptance criteria, and verify tests pass."
	}
	return text
}

// PMAgentChat is the exported wrapper for use by main package.
func PMAgentChat(userMsg string) string {
	return pmAgentChat(userMsg)
}

// SummarizeChat sends a batch of old messages to the LLM for compression
// and returns a concise summary string. Used by context window management.
func SummarizeChat(messages []models.RoleMsg) (string, error) {
	getToken := func() string {
		if getTokenFunc != nil {
			return getTokenFunc()
		}
		return ""
	}
	ag := agent.NewAgentClient(cfg, slog.Default(), getToken)

	var sb strings.Builder
	for _, msg := range messages {
		role := msg.Role
		text := msg.GetText()

		// Include tool call info
		if msg.ToolCall != nil {
			text = fmt.Sprintf("[tool call: %s] args: %s", msg.ToolCall.FuncCall.Name, msg.ToolCall.FuncCall.Args)
		} else if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				if text != "" {
					text += "\n"
				}
				text += fmt.Sprintf("[tool call: %s] args: %s", tc.FuncCall.Name, tc.FuncCall.Args)
			}
		}
		if text == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n\n", role, text))
	}
	conversationText := strings.TrimSpace(sb.String())

	sysPrompt := "You are a conversation summarizer for an autonomous coding agent. Produce a concise, structured summary that preserves all actionable context."
	userPrompt := fmt.Sprintf(
		"Summarize the following conversation history concisely. Focus on:\n"+
			"1. What code changes were made (which files, what functions modified)\n"+
			"2. What tool calls were executed and their key results (test outcomes, errors)\n"+
			"3. What decisions were made and why\n"+
			"4. What the current state is (branch, files changed, tests passing)\n"+
			"5. What remains to be done\n\n"+
			"Preserve file paths, function names, and error messages. Be specific, not generic.\n\n%s",
			conversationText)

	body, err := ag.FormFirstMsg(sysPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("failed to form summary request: %w", err)
	}
	resp, err := ag.LLMRequest(body)
	if err != nil {
		return "", fmt.Errorf("summary request failed: %w", err)
	}
	return string(resp), nil
}

func moveIssueTool(args map[string]string) []byte {
	if currentMission == nil {
		return []byte(`{"error": "No active mission"}`)
	}

	status := args["status"]
	if status == "" {
		return []byte(`{"error": "status is required (review, done, archive)"}`)
	}

	var targetStatus mission.IssueStatus
	switch strings.ToLower(status) {
	case "review":
		targetStatus = mission.StatusReview
	case "done":
		targetStatus = mission.StatusDone
	case "archive":
		targetStatus = mission.StatusArchive
	default:
		return []byte(fmt.Sprintf(`{"error": "Invalid status: %s. Use: review, done, archive"}`, status))
	}

	if err := currentMission.MoveToStatus(targetStatus); err != nil {
		return []byte(fmt.Sprintf(`{"error": "%v"}`, err))
	}

	currentMission.Checkpoint.IncrementToolCalls()
	if err := currentMission.SaveCheckpoint("mission-checkpoint.json"); err != nil {
		currentMission.Log("Warning: failed to save checkpoint after move_issue: %v", err)
	}

	return []byte(fmt.Sprintf(`{"success": true, "status": "%s", "issue_id": "%s"}`, status, currentMission.Issue.ID))
}

func createIssueTool(args map[string]string) []byte {
	if currentMission == nil {
		return []byte(`{"error": "No active mission"}`)
	}

	id := args["id"]
	title := args["title"]
	description := args["description"]
	branchName := args["branch_name"]

	if id == "" || title == "" {
		return []byte(`{"error": "id and title are required"}`)
	}

	if description == "" {
		description = "No description provided"
	}

	projectPath := currentMission.Issue.ProjectPath
	if args["project_path"] != "" {
		projectPath = args["project_path"]
	}

	issue := &mission.Issue{
		Version:        mission.IssueVersion,
		ID:             id,
		Title:          title,
		Description:    description,
		Status:         mission.StatusOpen,
		ProjectPath:    projectPath,
		BranchName:     branchName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		RelatedIssues:  []string{currentMission.Issue.ID},
	}

	path := filepath.Join(currentMission.Manager.IssuesDir, string(mission.StatusOpen), id+".json")
	if err := os.MkdirAll(filepath.Join(currentMission.Manager.IssuesDir, string(mission.StatusOpen)), 0755); err != nil {
		return []byte(fmt.Sprintf(`{"error": "Failed to create directory: %v"}`, err))
	}

	if err := mission.SaveIssue(issue, path); err != nil {
		return []byte(fmt.Sprintf(`{"error": "Failed to save issue: %v"}`, err))
	}

	currentMission.Checkpoint.IncrementToolCalls()
	if err := currentMission.SaveCheckpoint("mission-checkpoint.json"); err != nil {
		currentMission.Log("Warning: failed to save checkpoint after create_issue: %v", err)
	}

	return []byte(fmt.Sprintf(`{"success": true, "issue_id": "%s", "path": "%s"}`, id, path))
}

func createPRTool(args map[string]string) []byte {
	if currentMission == nil {
		return []byte(`{"error": "No active mission"}`)
	}

	title := args["title"]
	body := args["body"]
	baseBranch := args["base"]

	if title == "" {
		title = fmt.Sprintf("Fix: %s", currentMission.Issue.Title)
	}

	if body == "" {
		body = fmt.Sprintf("## Summary\n\nFixes issue #%s\n\n## Changes\n\n<!-- Describe changes made -->\n\n## Testing\n\n<!-- Describe testing performed -->", currentMission.Issue.ID)
	}

	// Auto-detect branch name from git if not set on the issue
	branchName := currentMission.Issue.BranchName
	if branchName == "" {
		if projectPath := currentMission.Issue.ProjectPath; projectPath != "" {
			if b, err := getCurrentBranch(projectPath); err == nil {
				branchName = b
			}
		}
	}
	if branchName == "" {
		branchName = "unknown"
	}
	currentMission.Issue.BranchName = branchName

	// Only append acceptance criteria from the issue if the LLM didn't already include them
	acBullets := ""
	if len(currentMission.Issue.AcceptanceCriteria) > 0 {
		lower := strings.ToLower(body)
		if !strings.Contains(lower, "acceptance criteria") && !strings.Contains(lower, "acceptance_criteria") {
			for _, c := range currentMission.Issue.AcceptanceCriteria {
				acBullets += fmt.Sprintf("- %s\n", c)
			}
		}
	}

	prFile := ""
	issuesDir := currentMission.Manager.IssuesDir
	if issuesDir != "" {
		reviewDir := filepath.Join(issuesDir, "review")
		if err := os.MkdirAll(reviewDir, 0755); err == nil {
			base := baseBranch
			if base == "" {
				base = "main"
			}
			bt := "`"
			affectedFiles := ""
			if len(currentMission.Issue.ContextFiles) > 0 {
				for _, f := range currentMission.Issue.ContextFiles {
					affectedFiles += "- " + f + "\n"
				}
			}
			prContent := "# PR: " + title + "\n\n" +
				"**Issue**: #" + currentMission.Issue.ID + " - " + currentMission.Issue.Title + "\n" +
				"**Branch**: " + bt + branchName + bt + "\n" +
				"**Base**: " + bt + base + bt + "\n\n" +
				"## Description\n\n" + body + "\n"
			if affectedFiles != "" {
				prContent += "\n## Affected\n\n" + affectedFiles + "\n"
			}
			if acBullets != "" {
				prContent += "\n## Acceptance Criteria\n\n" + acBullets + "\n"
			}
			prContent += "\n---\n\n*Generated by gf-lt auto-issue-solver*\n"

			prPath := filepath.Join(reviewDir, fmt.Sprintf("issue-%s-pr.md", currentMission.Issue.ID))
			if err := os.WriteFile(prPath, []byte(prContent), 0644); err != nil {
				currentMission.Log("Warning: failed to write PR file: %v", err)
			} else {
				prFile = prPath
				currentMission.Log("PR file written to %s", prPath)
			}
		}
	}

	result := map[string]interface{}{
		"success":     true,
		"pr_title":     title,
		"branch_name":  branchName,
		"issue_id":     currentMission.Issue.ID,
		"base_branch": baseBranch,
		"pr_body":     body,
		"pr_file":     prFile,
	}

	// Don't move to review here — missionComplete() handles that after signaling success.
	// Otherwise the issue file gets double-moved and deleted.

	currentMission.Checkpoint.IncrementToolCalls()
	currentMission.Status = mission.StatusSuccess

	return []byte(mustMarshalJSON(result))
}

func getCurrentBranch(projectPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func pmConsultTool(args map[string]string) []byte {
	if currentMission == nil {
		return []byte(`{"error": "No active mission"}`)
	}

	question := args["question"]
	if question == "" {
		question = "Any concerns or should I keep going?"
	}

	currentMission.Log("PM consultation requested: %s", question)

	ac := "N/A"
	if len(currentMission.Issue.AcceptanceCriteria) > 0 {
		ac = "- " + strings.Join(currentMission.Issue.AcceptanceCriteria, "\n- ")
	}

	prompt := fmt.Sprintf(
		"The agent requests guidance.\n\n"+
			"Issue: %s (%s)\n"+
			"Description: %s\n"+
			"Acceptance criteria:\n%s\n"+
			"Branch: %s\nTool calls: %d\nCommits: %v\nConsecutive failures: %d\n"+
			"Project path: %s\n\n"+
			"Agent's question: %s",
		currentMission.Issue.Title, currentMission.Issue.ID,
		currentMission.Issue.Description,
		ac,
		currentMission.Issue.BranchName,
		currentMission.Checkpoint.ToolCallCount,
		currentMission.Checkpoint.CommitsMade,
		currentMission.Checkpoint.ConsecutiveFailures,
		currentMission.Issue.ProjectPath,
		question,
	)

	response := pmAgentChat(prompt)

	return []byte(mustMarshalJSON(map[string]interface{}{
		"pm_response": response,
		"issue_id":    currentMission.Issue.ID,
	}))
}

func addIssueCommentTool(args map[string]string) []byte {
	if currentMission == nil {
		return []byte(`{"error": "No active mission"}`)
	}

	body := args["body"]
	author := args["author"]

	if body == "" {
		return []byte(`{"error": "body is required"}`)
	}

	if author == "" {
		author = "solver"
	}

	currentMission.AddIssueComment(author, body)
	if err := currentMission.SaveIssue(); err != nil {
		return []byte(fmt.Sprintf(`{"error": "Failed to save issue comment: %v"}`, err))
	}

	currentMission.Checkpoint.IncrementToolCalls()

	return []byte(fmt.Sprintf(`{"success": true, "comment_by": "%s", "issue_id": "%s"}`, author, currentMission.Issue.ID))
}

func mustMarshalJSON(v interface{}) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": "Failed to marshal response: %v"}`, err)
	}
	return string(data)
}

// IsToolError returns true if the tool response appears to contain an error.
// Checks for: bash [error] prefix, JSON error field, go test FAIL, compile errors.
func IsToolError(toolName, resp string) bool {
	trimmed := strings.TrimSpace(resp)

	// Bash/system command errors (prefix [error])
	if strings.HasPrefix(trimmed, "[error]") {
		return true
	}

	// JSON error field (mission tools and others)
	if strings.HasPrefix(trimmed, "{") {
		if strings.Contains(trimmed, `"error"`) {
			return true
		}
	}

	// Go test failures
	if strings.HasPrefix(toolName, "bash") || toolName == "run_command" {
		if strings.Contains(resp, "\nFAIL\n") || strings.Contains(resp, "\nFAIL\t") {
			return true
		}
		// Go compile errors ("cannot use", "undefined", "expected")
		if strings.Contains(resp, "cannot use") || strings.Contains(resp, "undefined") {
			if strings.Contains(resp, ".go:") {
				return true
			}
		}
	}

	return false
}