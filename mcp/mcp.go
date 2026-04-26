package mcp

import (
	"context"
	"fmt"
	"gf-lt/config"
	"gf-lt/tools"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type MCPServer struct {
	name   string
	url    string
	client *client.Client
	tools  []mcp.Tool
}

const (
	ClientName    = "gf-lt"
	ClientVersion = "1.0.0"
)

type Manager struct {
	cfg     *config.Config
	logger  *slog.Logger
	servers map[string]*MCPServer
	toolMap map[string]*MCPServer
}

func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	return &Manager{
		cfg:     cfg,
		logger:  logger,
		servers: make(map[string]*MCPServer),
		toolMap: make(map[string]*MCPServer),
	}
}

func (m *Manager) ConnectAll(ctx context.Context) error {
	if len(m.cfg.MCPServers) == 0 {
		return nil
	}

	for name, serverCfg := range m.cfg.MCPServers {
		if serverCfg.URL == "" {
			m.logger.Warn("MCP server URL not configured, skipping", "server", name)
			continue
		}

		server := &MCPServer{
			name: name,
			url:  serverCfg.URL,
		}

		if err := server.connect(ctx, m.logger); err != nil {
			m.logger.Error("failed to connect to MCP server", "server", name, "url", serverCfg.URL, "error", err)
			continue
		}

		m.servers[name] = server
		m.logger.Info("connected to MCP server", "server", name, "url", serverCfg.URL, "tools", len(server.tools))
	}

	return nil
}

func (s *MCPServer) connect(ctx context.Context, logger *slog.Logger) error {
	trans, err := transport.NewStreamableHTTP(s.url)
	if err != nil {
		return fmt.Errorf("failed to create HTTP transport: %w", err)
	}

	s.client = client.NewClient(trans)

	if err := s.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start MCP client: %w", err)
	}

	_, err = s.client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo: mcp.Implementation{
				Name:    ClientName,
				Version: ClientVersion,
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	})
	if err != nil {
		s.client.Close()
		return fmt.Errorf("failed to initialize MCP session: %w", err)
	}

	result, err := s.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		s.client.Close()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	s.tools = result.Tools
	return nil
}

func (m *Manager) GetTools() []mcp.Tool {
	var allTools []mcp.Tool
	for _, server := range m.servers {
		allTools = append(allTools, server.tools...)
	}
	return allTools
}

func (m *Manager) GetOpenAITools() []any {
	var tools []any
	for _, server := range m.servers {
		for _, tool := range server.tools {
			openAITool := convertToolToOpenAI(server.name, tool)
			tools = append(tools, openAITool)
		}
	}
	return tools
}

func (m *Manager) HasTools() bool {
	return len(m.servers) > 0
}

func (m *Manager) RegisterToolHandlers(fnMap map[string]tools.FnHandler) {
	for _, server := range m.servers {
		for _, tool := range server.tools {
			prefixedName := fmt.Sprintf("mcp_%s_%s", server.name, tool.Name)
			m.toolMap[prefixedName] = server

			fnMap[prefixedName] = func(args map[string]string) []byte {
				return m.callTool(prefixedName, args)
			}
		}
	}
}

func (m *Manager) callTool(name string, args map[string]string) []byte {
	server, ok := m.toolMap[name]
	if !ok {
		return []byte(fmt.Sprintf("MCP tool %s not found", name))
	}

	toolName := strings.TrimPrefix(name, fmt.Sprintf("mcp_%s_", server.name))

	mcpArgs := make(map[string]any)
	for k, v := range args {
		mcpArgs[k] = v
	}

	result, err := server.client.CallTool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: mcpArgs,
		},
	})
	if err != nil {
		return []byte(fmt.Sprintf("MCP tool call failed: %v", err))
	}

	if result.IsError {
		var errMsg string
		for _, content := range result.Content {
			if tc, ok := content.(mcp.TextContent); ok {
				errMsg += tc.Text
			}
		}
		return []byte(fmt.Sprintf("MCP tool error: %s", errMsg))
	}

	var output strings.Builder
	for _, content := range result.Content {
		switch c := content.(type) {
		case mcp.TextContent:
			output.WriteString(c.Text)
		case mcp.ImageContent:
			output.WriteString(fmt.Sprintf("[image: %s]", c.Data))
		case mcp.EmbeddedResource:
			switch rc := c.Resource.(type) {
			case mcp.TextResourceContents:
				output.WriteString(fmt.Sprintf("[resource: %s - %s]", rc.URI, rc.Text))
			case mcp.BlobResourceContents:
				output.WriteString(fmt.Sprintf("[resource: %s (binary)]", rc.URI))
			}
		}
	}

	return []byte(output.String())
}

func convertToolToOpenAI(serverName string, tool mcp.Tool) map[string]any {
	inputSchema := convertInputSchema(tool.InputSchema)

	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        fmt.Sprintf("mcp_%s_%s", serverName, tool.Name),
			"description": tool.Description,
			"parameters":  inputSchema,
		},
	}
}

func convertInputSchema(schema mcp.ToolInputSchema) map[string]any {
	result := map[string]any{
		"type": "object",
	}

	if schema.Type != "" {
		result["type"] = schema.Type
	}

	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	if schema.Properties != nil {
		result["properties"] = schema.Properties
	}

	return result
}

func (m *Manager) Close() {
	for _, server := range m.servers {
		if server.client != nil {
			server.client.Close()
		}
	}
}
