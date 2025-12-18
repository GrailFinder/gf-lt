package agent

import (
	"gf-lt/config"
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

func (ag *AgentClient) LLMRequest(body io.Reader) ([]byte, error) {
	req, err := http.NewRequest("POST", ag.cfg.CurrentAPI, body)
	if err != nil {
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
