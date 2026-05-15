package mission

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const CheckpointVersion = 1

type Checkpoint struct {
	Version             int       `json:"version"`
	IssueID             string    `json:"issue_id"`
	IssuePath           string    `json:"issue_path"`
	ProjectPath         string    `json:"project_path"`
	BranchName          string    `json:"branch_name"`
	AgentCardID         string    `json:"agent_card_id"`
	AgentCardPath       string    `json:"agent_card_path"`
	Conversation        []Message `json:"conversation"`
	ToolCallCount       int       `json:"tool_call_count"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CommitsMade         []string  `json:"commits_made"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func LoadCheckpoint(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read checkpoint: %w", err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("failed to parse checkpoint JSON: %w", err)
	}

	return &cp, nil
}

func SaveCheckpoint(cp *Checkpoint, path string) error {
	cp.Version = CheckpointVersion
	cp.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	return nil
}

func NewCheckpoint(issue *Issue, agentCardPath string) *Checkpoint {
	return &Checkpoint{
		Version:       CheckpointVersion,
		IssueID:       issue.ID,
		IssuePath:     "",
		ProjectPath:   issue.ProjectPath,
		BranchName:    issue.BranchName,
		AgentCardID:   "issue-solver",
		AgentCardPath: agentCardPath,
		Conversation:  []Message{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

func (cp *Checkpoint) AddMessage(role, content string) {
	cp.Conversation = append(cp.Conversation, Message{
		Role:    role,
		Content: content,
	})
}

func (cp *Checkpoint) IncrementToolCalls() {
	cp.ToolCallCount++
}

func (cp *Checkpoint) AddFailure() {
	cp.ConsecutiveFailures++
}

func (cp *Checkpoint) ResetFailures() {
	cp.ConsecutiveFailures = 0
}

func (cp *Checkpoint) AddCommit(commitHash string) {
	cp.CommitsMade = append(cp.CommitsMade, commitHash)
}

func DefaultCheckpointPath() string {
	return "mission-checkpoint.json"
}