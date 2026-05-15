package mission

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const IssueVersion = "1.0"

type IssueStatus string

const (
	StatusOpen       IssueStatus = "open"
	StatusInProgress IssueStatus = "in_progress"
	StatusReview     IssueStatus = "review"
	StatusDone       IssueStatus = "done"
	StatusArchive    IssueStatus = "archive"
)

type Comment struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type Issue struct {
	Version           string       `json:"version"`
	ID                string       `json:"id"`
	Title             string       `json:"title"`
	Description       string       `json:"description"`
	Status            IssueStatus  `json:"status"`
	ProjectPath       string       `json:"project_path"`
	BranchName        string       `json:"branch_name,omitempty"`
	Labels            []string     `json:"labels,omitempty"`
	Priority          string       `json:"priority,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
	RelatedIssues     []string     `json:"related_issues,omitempty"`
	AcceptanceCriteria []string    `json:"acceptance_criteria,omitempty"`
	ContextFiles      []string     `json:"context_files,omitempty"`
	Comments          []Comment    `json:"comments,omitempty"`
}

func LoadIssue(issuePath string) (*Issue, error) {
	data, err := os.ReadFile(issuePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read issue file: %w", err)
	}

	var issue Issue
	if err := json.Unmarshal(data, &issue); err != nil {
		return nil, fmt.Errorf("failed to parse issue JSON: %w", err)
	}

	if issue.Version == "" {
		issue.Version = IssueVersion
	}

	return &issue, nil
}

func SaveIssue(issue *Issue, issuePath string) error {
	issue.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(issue, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal issue JSON: %w", err)
	}

	if err := os.WriteFile(issuePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write issue file: %w", err)
	}

	return nil
}

func CreateIssue(issuePath string, issue *Issue) error {
	issue.Version = IssueVersion
	issue.UpdatedAt = time.Now()
	if issue.CreatedAt.IsZero() {
		issue.CreatedAt = time.Now()
	}
	return SaveIssue(issue, issuePath)
}

func (i *Issue) AddComment(author, body string) {
	i.Comments = append(i.Comments, Comment{
		Author:    author,
		Body:      body,
		CreatedAt: time.Now(),
	})
}

func (i *Issue) MoveStatus(newStatus IssueStatus) {
	i.Status = newStatus
	i.UpdatedAt = time.Now()
}

func (i *Issue) SetBranchName(name string) {
	i.BranchName = name
	i.UpdatedAt = time.Now()
}

type IssueManager struct {
	IssuesDir string
}

func NewIssueManager(issuesDir string) *IssueManager {
	return &IssueManager{IssuesDir: issuesDir}
}

func (m *IssueManager) StatusDir(status IssueStatus) string {
	return filepath.Join(m.IssuesDir, string(status))
}

func (m *IssueManager) IssuePath(id string, status IssueStatus) string {
	return filepath.Join(m.StatusDir(status), id+".json")
}

func (m *IssueManager) LoadByID(id string, status IssueStatus) (*Issue, error) {
	return LoadIssue(m.IssuePath(id, status))
}

func (m *IssueManager) LoadByIDAny(id string) (*Issue, IssueStatus, error) {
	statuses := []IssueStatus{StatusOpen, StatusInProgress, StatusReview, StatusDone, StatusArchive}
	for _, status := range statuses {
		path := m.IssuePath(id, status)
		if _, err := os.Stat(path); err == nil {
			issue, err := LoadIssue(path)
			if err == nil {
				return issue, status, nil
			}
		}
	}
	return nil, "", fmt.Errorf("issue %s not found in any status directory", id)
}

func (m *IssueManager) PickOpenIssue() (*Issue, IssueStatus, error) {
	openDir := m.StatusDir(StatusOpen)
	entries, err := os.ReadDir(openDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read open directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		issue, err := LoadIssue(filepath.Join(openDir, entry.Name()))
		if err == nil {
			return issue, StatusOpen, nil
		}
	}

	return nil, "", fmt.Errorf("no open issues found")
}

func (m *IssueManager) MoveIssue(id string, fromStatus, toStatus IssueStatus) error {
	fromPath := m.IssuePath(id, fromStatus)
	toPath := m.IssuePath(id, toStatus)

	if err := os.MkdirAll(m.StatusDir(toStatus), 0755); err != nil {
		return fmt.Errorf("failed to create status directory: %w", err)
	}

	data, err := os.ReadFile(fromPath)
	if err != nil {
		return fmt.Errorf("failed to read issue file: %w", err)
	}

	var issue Issue
	if err := json.Unmarshal(data, &issue); err != nil {
		return fmt.Errorf("failed to parse issue JSON: %w", err)
	}

	issue.MoveStatus(toStatus)

	if err := os.WriteFile(toPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write to new location: %w", err)
	}

	if err := os.Remove(fromPath); err != nil {
		return fmt.Errorf("failed to remove old file: %w", err)
	}

	return nil
}

func (m *IssueManager) CreateNewIssue(id, title, description, projectPath, branchName string) (*Issue, error) {
	issue := &Issue{
		Version:     IssueVersion,
		ID:          id,
		Title:       title,
		Description: description,
		Status:      StatusOpen,
		ProjectPath: projectPath,
		BranchName:  branchName,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	path := m.IssuePath(id, StatusOpen)
	if err := CreateIssue(path, issue); err != nil {
		return nil, err
	}

	return issue, nil
}