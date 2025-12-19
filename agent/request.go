package agent

import (
	"bytes"
	"encoding/json"
	"gf-lt/config"
	"gf-lt/models"
	"io"
	"log/slog"
	"net/http"
)

var httpClient = &http.Client{}

type AgentClient struct {
	cfg      *config.Config
	getToken func() string
	log      slog.Logger
}

func NewAgentClient(cfg *config.Config, log slog.Logger, gt func() string) *AgentClient {
	return &AgentClient{
		cfg:      cfg,
		getToken: gt,
		log:      log,
	}
}

func (ag *AgentClient) FormMsg(sysprompt, msg string) (io.Reader, error) {
	agentConvo := []models.RoleMsg{
		{Role: "system", Content: sysprompt},
		{Role: "user", Content: msg},
	}
	agentChat := &models.ChatBody{
		Model:    ag.cfg.CurrentModel,
		Stream:   true,
		Messages: agentConvo,
	}
	b, err := json.Marshal(agentChat)
	if err != nil {
		ag.log.Error("failed to form agent msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(b), nil
}

func (ag *AgentClient) LLMRequest(body io.Reader) ([]byte, error) {
	req, err := http.NewRequest("POST", ag.cfg.CurrentAPI, body)
	if err != nil {
		ag.log.Error("llamacpp api", "error", err)
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+ag.getToken())
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := httpClient.Do(req)
	if err != nil {
		ag.log.Error("llamacpp api", "error", err)
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
