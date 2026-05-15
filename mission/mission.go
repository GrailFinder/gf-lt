package mission

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type MissionStatus string

const (
	StatusRunning    MissionStatus = "running"
	StatusPaused      MissionStatus = "paused"
	StatusSuccess     MissionStatus = "success"
	StatusFailed      MissionStatus = "failed"
	StatusAborted     MissionStatus = "aborted"
)

type Mission struct {
	Status       MissionStatus
	Issue       *Issue
	Checkpoint  *Checkpoint
	Manager     *IssueManager
	PMInterval  int
	MaxFailures int
	Quiet       bool
}

func NewMission(issue *Issue, issueManager *IssueManager, pmInterval, maxFailures int, quiet bool) *Mission {
	return &Mission{
		Status:      StatusRunning,
		Issue:      issue,
		Checkpoint: NewCheckpoint(issue, ""),
		Manager:    issueManager,
		PMInterval: pmInterval,
		MaxFailures: maxFailures,
		Quiet:      quiet,
	}
}

func (m *Mission) Log(format string, args ...interface{}) {
	if m.Quiet {
		return
	}
	fmt.Printf("[MISSION] "+format+"\n", args...)
}

func (m *Mission) LogTool(name string, args ...interface{}) {
	if m.Quiet {
		return
	}
	fmt.Printf("[TOOL] %s", name)
	if len(args) > 0 {
		fmt.Printf(" - ")
		for i, arg := range args {
			if i > 0 {
				fmt.Printf(", ")
			}
			fmt.Printf("%v", arg)
		}
	}
	fmt.Println()
}

func (m *Mission) IncrementToolCalls() {
	m.Checkpoint.IncrementToolCalls()
}

func (m *Mission) ShouldPMCheckIn() bool {
	return m.Checkpoint.ToolCallCount > 0 && m.Checkpoint.ToolCallCount%m.PMInterval == 0
}

func (m *Mission) AddFailure() {
	m.Checkpoint.AddFailure()
}

func (m *Mission) ResetFailures() {
	m.Checkpoint.ResetFailures()
}

func (m *Mission) ShouldAbort() bool {
	return m.Checkpoint.ConsecutiveFailures >= m.MaxFailures
}

func (m *Mission) MoveToInProgress() error {
	if m.Issue.Status != StatusOpen {
		return fmt.Errorf("issue is not in open status")
	}
	
	newPath := filepath.Join(m.Manager.IssuesDir, string(StatusInProgress), m.Issue.ID+".json")
	m.Issue.Status = StatusInProgress
	m.Issue.UpdatedAt = time.Now()
	
	if err := os.MkdirAll(filepath.Join(m.Manager.IssuesDir, string(StatusInProgress)), 0755); err != nil {
		return fmt.Errorf("failed to create in_progress directory: %w", err)
	}
	
	data, err := json.MarshalIndent(m.Issue, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal issue: %w", err)
	}
	
	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write issue to in_progress: %w", err)
	}
	
	oldPath := filepath.Join(m.Manager.IssuesDir, string(StatusOpen), m.Issue.ID+".json")
	if err := os.Remove(oldPath); err != nil {
		return fmt.Errorf("failed to remove from open: %w", err)
	}
	
	m.Checkpoint.IssuePath = newPath
	m.Log("Issue moved to in_progress")
	return nil
}

func (m *Mission) MoveToReview() error {
	oldStatus := m.Issue.Status
	newPath := filepath.Join(m.Manager.IssuesDir, string(StatusReview), m.Issue.ID+".json")
	m.Issue.Status = StatusReview
	m.Issue.UpdatedAt = time.Now()
	
	if err := os.MkdirAll(filepath.Join(m.Manager.IssuesDir, string(StatusReview)), 0755); err != nil {
		return fmt.Errorf("failed to create review directory: %w", err)
	}
	
	data, err := json.MarshalIndent(m.Issue, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal issue: %w", err)
	}
	
	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write issue to review: %w", err)
	}
	
	oldPath := filepath.Join(m.Manager.IssuesDir, string(oldStatus), m.Issue.ID+".json")
	if err := os.Remove(oldPath); err != nil {
		return fmt.Errorf("failed to remove from previous status: %w", err)
	}
	
	m.Log("Issue moved to review")
	return nil
}

func (m *Mission) MoveToDone() error {
	return m.moveToStatus(StatusDone)
}

func (m *Mission) MoveToArchive() error {
	return m.moveToStatus(StatusArchive)
}

func (m *Mission) MoveToStatus(newStatus IssueStatus) error {
	return m.moveToStatus(newStatus)
}

func (m *Mission) moveToStatus(newStatus IssueStatus) error {
	oldStatus := m.Issue.Status
	newPath := filepath.Join(m.Manager.IssuesDir, string(newStatus), m.Issue.ID+".json")
	m.Issue.Status = newStatus
	m.Issue.UpdatedAt = time.Now()
	
	dir := filepath.Join(m.Manager.IssuesDir, string(newStatus))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	
	data, err := json.MarshalIndent(m.Issue, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal issue: %w", err)
	}
	
	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write issue: %w", err)
	}
	
	oldPath := filepath.Join(m.Manager.IssuesDir, string(oldStatus), m.Issue.ID+".json")
	os.Remove(oldPath)
	
	m.Log("Issue moved to %s", newStatus)
	return nil
}

func (m *Mission) SaveCheckpoint(path string) error {
	return SaveCheckpoint(m.Checkpoint, path)
}

func (m *Mission) AddCommit(commitHash string) {
	m.Checkpoint.AddCommit(commitHash)
}

func (m *Mission) SetBranchName(name string) {
	m.Issue.SetBranchName(name)
	m.Checkpoint.BranchName = name
}

func (m *Mission) AddIssueComment(author, body string) {
	m.Issue.AddComment(author, body)
}

func (m *Mission) SaveIssue() error {
	path := filepath.Join(m.Manager.IssuesDir, string(m.Issue.Status), m.Issue.ID+".json")
	return SaveIssue(m.Issue, path)
}

func (m *Mission) GetConversationForLLM() []Message {
	return m.Checkpoint.Conversation
}

func (m *Mission) AddToConversation(role, content string) {
	m.Checkpoint.AddMessage(role, content)
}

type MissionResult struct {
	Status     MissionStatus
	IssueID    string
	BranchName string
	PRURL      string
	Commits    []string
	ToolCalls  int
	Duration   time.Duration
	Error      error
}

func (r MissionResult) ToJSON() string {
	data, _ := json.MarshalIndent(map[string]interface{}{
		"status":       r.Status,
		"issue_id":     r.IssueID,
		"branch_name":  r.BranchName,
		"pr_url":       r.PRURL,
		"commits":      r.Commits,
		"tool_calls":    r.ToolCalls,
		"session_duration": r.Duration.String(),
		"error":        r.Error,
	}, "", "  ")
	return string(data)
}