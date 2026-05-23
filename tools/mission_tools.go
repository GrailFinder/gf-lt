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
	"path/filepath"
	"strings"
	"time"
)

var (
	currentMission *mission.Mission
	pmAgent        *agent.AgentClient
)

const pmSystemPrompt = `You are the Project Manager for an autonomous coding agent. The agent is working on a software issue without human intervention. Your role is to:

1. **Keep the agent aligned with the issue goals** - Remind it what it's supposed to accomplish
2. **Provide guidance when stuck** - Suggest approaches when the agent is blocked
3. **Review progress** - Assess if the agent is on track or going off-course
4. **Advocate for quality** - Remind about tests, code review, and acceptance criteria
5. **Be concise** - The agent doesn't need lengthy explanations; give clear, actionable guidance

You are aware that:
- The agent has full access to bash, git, file editing, and other tools
- The agent should create feature branches and commit incrementally
- The agent must write/run tests and meet acceptance criteria before completion
- The agent should use pm_consult when uncertain or blocked

When giving guidance, focus on:
- Whether the agent is progressing toward acceptance criteria
- Whether it should pivot to a different approach
- Whether it needs to write tests or run existing ones
- Whether the current commit structure makes sense
- Whether it should ask for more clarification or push forward`

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
	return string(resp)
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

	result := map[string]interface{}{
		"success":     true,
		"pr_title":     title,
		"branch_name":  currentMission.Issue.BranchName,
		"issue_id":     currentMission.Issue.ID,
		"base_branch": baseBranch,
	}

	if body == "" {
		body = fmt.Sprintf("## Summary\n\nFixes issue #%s\n\n## Changes\n\n<!-- Describe changes made -->\n\n## Testing\n\n<!-- Describe testing performed -->", currentMission.Issue.ID)
	}
	result["pr_body"] = body

	if err := currentMission.MoveToReview(); err != nil {
		result["warning"] = fmt.Sprintf("Failed to move issue to review: %v", err)
	}

	currentMission.Checkpoint.IncrementToolCalls()
	currentMission.Status = mission.StatusSuccess

	return []byte(mustMarshalJSON(result))
}

func pmConsultTool(args map[string]string) []byte {
	if currentMission == nil {
		return []byte(`{"error": "No active mission"}`)
	}

	question := args["question"]
	if question == "" {
		question = "How am I doing? Any guidance?"
	}

	currentMission.Log("PM consultation requested: %s", question)

	context := map[string]interface{}{
		"issue_id":             currentMission.Issue.ID,
		"issue_title":          currentMission.Issue.Title,
		"issue_description":    currentMission.Issue.Description,
		"branch_name":          currentMission.Issue.BranchName,
		"project_path":         currentMission.Issue.ProjectPath,
		"acceptance_criteria":  currentMission.Issue.AcceptanceCriteria,
		"tool_calls":           currentMission.Checkpoint.ToolCallCount,
		"commits_made":         currentMission.Checkpoint.CommitsMade,
		"consecutive_failures": currentMission.Checkpoint.ConsecutiveFailures,
		"pm_question":          question,
	}

	prompt := mustMarshalJSON(context)
	response := pmAgentChat("Here is the current mission context and my question:\n\n" + prompt)

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

func MissionToolDefs() string {
	return `[
  {
    "name": "move_issue",
    "args": ["status"],
    "when_to_use": "Move the current issue to a different status. Status values: review (PR created), done (merged), archive (abandoned). Example: move_issue status=review"
  },
  {
    "name": "create_issue",
    "args": ["id", "title", "description", "branch_name"],
    "when_to_use": "Create a new sub-issue for related work. Use when an issue needs to be split into smaller parts. Example: create_issue id=99 title='Fix login timeout' description='The login page times out after 30s'"
  },
  {
    "name": "create_pr",
    "args": ["title", "body", "base"],
    "when_to_use": "Mark the current issue as complete by creating a pull/merge request. This signals mission completion. Example: create_pr title='Fix: login timeout' body='## Summary\nFixes #42'"
  },
  {
    "name": "pm_consult",
    "args": ["question"],
    "when_to_use": "Request guidance from the project manager. Use when stuck, need direction, or want feedback. Example: pm_consult question='Should I focus on tests or documentation?'"
  },
  {
    "name": "add_issue_comment",
    "args": ["body", "author"],
    "when_to_use": "Add a comment to the issue file for tracking progress. Example: add_issue_comment body='Completed user login implementation'"
  }
]`
}