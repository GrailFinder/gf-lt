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
	currentMission   *mission.Mission
	pmAgent          *agent.AgentClient
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

func moveIssueTool(args map[string]string) ([]byte, error) {
	if currentMission == nil {
		return nil, models.Unavailable("no active mission", "these tools only work in mission mode (--mission / --mission-tools)")
	}

	status := args["status"]
	if status == "" {
		return nil, models.InvalidArgs("status is required (review, done, archive)", "")
	}

	// Don't allow overwriting a completed mission
	if currentMission.Status == mission.StatusSuccess {
		return nil, models.Conflict(fmt.Sprintf("mission is already completed (status %q) and cannot be moved further", currentMission.Status), "read the issue to see its final state")
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
		return nil, models.InvalidArgs(fmt.Sprintf("invalid status: %s", status), "use: review, done, archive")
	}

	if err := currentMission.MoveToStatus(targetStatus); err != nil {
		return nil, models.Internal("failed to save checkpoint", err)
	}

	if err := currentMission.SaveCheckpoint("mission-checkpoint.json"); err != nil {
		currentMission.Log("Warning: failed to save checkpoint after move_issue: %v", err)
	}

	return []byte(fmt.Sprintf(`{"success": true, "status": "%s", "issue_id": "%s"}`, status, currentMission.Issue.ID)), nil
}

func createIssueTool(args map[string]string) ([]byte, error) {
	id := args["id"]
	title := args["title"]
	description := args["description"]
	branchName := args["branch_name"]

	if title == "" {
		return nil, models.InvalidArgs("title is required", "")
	}

	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UnixMilli())
	}

	if description == "" {
		description = "No description provided"
	}

	// Project path: explicit > mission > FilePickerDir
	projectPath := cfg.FilePickerDir
	if currentMission != nil && currentMission.Issue.ProjectPath != "" {
		projectPath = currentMission.Issue.ProjectPath
	}
	if args["project_path"] != "" {
		projectPath = args["project_path"]
	}

	// Parse acceptance_criteria from JSON array string
	var acceptanceCriteria []string
	if acJSON := args["acceptance_criteria"]; acJSON != "" {
		json.Unmarshal([]byte(acJSON), &acceptanceCriteria)
	}

	// Parse context_files from JSON array string
	var contextFiles []string
	if cfJSON := args["context_files"]; cfJSON != "" {
		json.Unmarshal([]byte(cfJSON), &contextFiles)
	}

	// Parse labels from comma-separated string
	var labels []string
	if labelsStr := args["labels"]; labelsStr != "" {
		for _, l := range strings.Split(labelsStr, ",") {
			if trimmed := strings.TrimSpace(l); trimmed != "" {
				labels = append(labels, trimmed)
			}
		}
	}

	issue := &mission.Issue{
		Version:            mission.IssueVersion,
		ID:                 id,
		Title:              title,
		Description:        description,
		Status:             mission.StatusOpen,
		ProjectPath:        projectPath,
		BranchName:         branchName,
		Labels:             labels,
		Priority:           args["priority"],
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		AcceptanceCriteria: acceptanceCriteria,
		ContextFiles:       contextFiles,
	}
	if currentMission != nil {
		issue.RelatedIssues = []string{currentMission.Issue.ID}
	}

	// Issues directory: mission > config > default
	issuesDir := cfg.IssuesDir
	if currentMission != nil {
		issuesDir = currentMission.Manager.IssuesDir
	}
	if issuesDir == "" {
		issuesDir = "./issues"
	}

	openDir := filepath.Join(issuesDir, string(mission.StatusOpen))
	if err := os.MkdirAll(openDir, 0755); err != nil {
		return nil, models.Internal("failed to create the issues directory", err)
	}

	path := filepath.Join(openDir, id+".json")
	if err := mission.SaveIssue(issue, path); err != nil {
		return nil, models.Internal("failed to save issue", err)
	}

	if currentMission != nil {
		if err := currentMission.SaveCheckpoint("mission-checkpoint.json"); err != nil {
			currentMission.Log("Warning: failed to save checkpoint after create_issue: %v", err)
		}
	}

	return []byte(fmt.Sprintf(`{"success": true, "issue_id": "%s", "path": "%s"}`, id, path)), nil
}

func createPRTool(args map[string]string) ([]byte, error) {
	if currentMission == nil {
		return nil, models.Unavailable("no active mission", "these tools only work in mission mode (--mission / --mission-tools)")
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
	currentMission.Log("createPRTool: issue.BranchName=%q", branchName)
	if branchName == "" {
		if projectPath := currentMission.Issue.ProjectPath; projectPath != "" {
			if b, err := getCurrentBranch(projectPath); err == nil {
				branchName = b
				currentMission.Log("createPRTool: getCurrentBranch returned %q", branchName)
			} else {
				currentMission.Log("createPRTool: getCurrentBranch error=%v", err)
			}
		}
	}
	if branchName == "" {
		branchName = "unknown"
	}
	currentMission.Issue.BranchName = branchName
	currentMission.Log("createPRTool: final branchName=%q", branchName)

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
		"pr_title":    title,
		"branch_name": branchName,
		"issue_id":    currentMission.Issue.ID,
		"base_branch": baseBranch,
		"pr_body":     body,
		"pr_file":     prFile,
	}

	// Don't move to review here — missionComplete() handles that after signaling success.
	// Otherwise the issue file gets double-moved and deleted.

	currentMission.Status = mission.StatusSuccess

	payload, err := mustMarshalJSON(result)
	if err != nil {
		return nil, err
	}
	return []byte(payload), nil
}

func getCurrentBranch(projectPath string) (string, error) {
	currentMission.Log("getCurrentBranch: projectPath=%s", projectPath)
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		currentMission.Log("getCurrentBranch: error=%v", err)
		return "", err
	}
	branch := strings.TrimSpace(string(out))
	currentMission.Log("getCurrentBranch: result=%q", branch)
	return branch, nil
}

func pmConsultTool(args map[string]string) ([]byte, error) {
	if currentMission == nil {
		return nil, models.Unavailable("no active mission", "these tools only work in mission mode (--mission / --mission-tools)")
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
		"You are the project manager. The coding agent is asking for your guidance.\n"+
			"Address the agent directly as \"you\" and give clear, imperative instructions.\n\n"+
			"Issue: %s (%s)\n"+
			"Description: %s\n"+
			"Acceptance criteria:\n%s\n"+
			"Branch: %s\nTool calls so far: %d\nCommits: %v\nConsecutive failures: %d\n"+
			"Project path: %s\n\n"+
			"Agent's question: %s\n\n"+
			"Tell the agent exactly what to do next. Be short and direct.",
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

	payload, err := mustMarshalJSON(map[string]interface{}{
		"pm_response": response,
		"issue_id":    currentMission.Issue.ID,
	})
	if err != nil {
		return nil, err
	}
	return []byte(payload), nil
}

func addIssueCommentTool(args map[string]string) ([]byte, error) {
	if currentMission == nil {
		return nil, models.Unavailable("no active mission", "these tools only work in mission mode (--mission / --mission-tools)")
	}

	body := args["body"]
	author := args["author"]

	if body == "" {
		return nil, models.InvalidArgs("body is required", "")
	}

	if author == "" {
		author = "solver"
	}

	currentMission.AddIssueComment(author, body)
	if err := currentMission.SaveIssue(); err != nil {
		return nil, models.Internal("failed to save the issue comment", err)
	}

	return []byte(fmt.Sprintf(`{"success": true, "comment_by": "%s", "issue_id": "%s"}`, author, currentMission.Issue.ID)), nil
}

// mustMarshalJSON encodes a tool success payload.
//
// It used to swallow a marshal failure into an `{"error": ...}` body and return
// it as if it were a success, which is the old convention in miniature: a
// failure the host could not see. It propagates instead.
func mustMarshalJSON(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", models.Internal("failed to encode the tool response", err)
	}
	return string(data), nil
}
