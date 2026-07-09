package mcp

import (
	"context"
	"fmt"
	"gf-lt/config"
	"gf-lt/tools"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPServer struct {
	name    string
	url     string
	session *mcp.ClientSession
	tools   []mcp.Tool
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
	transport := &mcp.StreamableClientTransport{
		Endpoint:             s.url,
		DisableStandaloneSSE: true,
	}

	client := mcp.NewClient(&mcp.Implementation{Name: ClientName, Version: ClientVersion}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	s.session = session

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		session.Close()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	s.tools = make([]mcp.Tool, len(result.Tools))
	for i, t := range result.Tools {
		s.tools[i] = *t
	}
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
		for i := range server.tools {
			openAITool := convertToolToOpenAI(server.name, &server.tools[i])
			tools = append(tools, openAITool)
		}
	}
	return tools
}

func (m *Manager) HasTools() bool {
	return len(m.servers) > 0
}

// IsVRAMFreeTool checks whether the given tool name belongs to an MCP server
// that is configured for VRAM management (model unload/reload around tool calls).
func (m *Manager) IsVRAMFreeTool(name string) bool {
	if m.cfg.ModelManagement == nil || len(m.cfg.ModelManagement.VRAMFreeServers) == 0 {
		m.logger.Debug("IsVRAMFreeTool: ModelManagement not configured, skipping", "tool", name)
		return false
	}
	server, ok := m.toolMap[name]
	if !ok {
		m.logger.Debug("IsVRAMFreeTool: tool not found in any MCP server", "tool", name)
		return false
	}
	for _, s := range m.cfg.ModelManagement.VRAMFreeServers {
		if server.name == s {
			m.logger.Debug("IsVRAMFreeTool: match", "tool", name, "server", server.name)
			return true
		}
	}
	m.logger.Debug("IsVRAMFreeTool: server not in VRAMFreeServers", "tool", name, "server", server.name, "vramFreeServers", m.cfg.ModelManagement.VRAMFreeServers)
	return false
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

	result, err := server.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: mcpArgs,
	})
	if err != nil {
		return []byte(fmt.Sprintf("MCP tool call failed: %v", err))
	}

	if result.IsError {
		var errMsg strings.Builder
		for _, content := range result.Content {
			if tc, ok := content.(*mcp.TextContent); ok {
				errMsg.WriteString(tc.Text)
			}
		}
		return []byte("MCP tool error: " + errMsg.String())
	}

	var output strings.Builder
	for _, content := range result.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			output.WriteString(c.Text)
		case *mcp.ImageContent:
			if p := extractImagePathFromText(output.String()); p != "" {
				continue
			}
			ext := ".png"
			if c.MIMEType != "" {
				if parts := strings.Split(c.MIMEType, "/"); len(parts) == 2 {
					ext = "." + parts[1]
				}
			}
			f, err := os.CreateTemp("", "mcp-image-*"+ext)
			if err != nil {
				fmt.Fprintf(&output, "[image: %d bytes]", len(c.Data))
				continue
			}
			f.Write(c.Data)
			f.Close()
			fmt.Fprintf(&output, "[image: %s]", f.Name())
		case *mcp.EmbeddedResource:
			if c.Resource != nil {
				if c.Resource.Text != "" {
					fmt.Fprintf(&output, "[resource: %s - %s]", c.Resource.URI, c.Resource.Text)
				} else if len(c.Resource.Blob) > 0 {
					ext := ".bin"
					if c.Resource.MIMEType != "" {
						if parts := strings.Split(c.Resource.MIMEType, "/"); len(parts) == 2 {
							ext = "." + parts[1]
						}
					}
					f, err := os.CreateTemp("", "mcp-resource-*"+ext)
					if err != nil {
						fmt.Fprintf(&output, "[resource: %s (%d bytes)]", c.Resource.URI, len(c.Resource.Blob))
						continue
					}
					f.Write(c.Resource.Blob)
					f.Close()
					fmt.Fprintf(&output, "[resource: %s - %s]", c.Resource.URI, f.Name())
				}
			}
		}
	}

	return []byte(output.String())
}

var imagePathFromTextRe = regexp.MustCompile(`(?:Image(?: saved to)?: )([^\s\[]+)`)

func extractImagePathFromText(text string) string {
	matches := imagePathFromTextRe.FindStringSubmatch(text)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func convertToolToOpenAI(serverName string, tool *mcp.Tool) map[string]any {
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

func convertInputSchema(schema any) map[string]any {
	// InputSchema can be map[string]any or ToolInputSchema
	if schemaMap, ok := schema.(map[string]any); ok {
		result := map[string]any{
			"type": "object",
		}
		if t, ok := schemaMap["type"].(string); ok {
			result["type"] = t
		}
		if required, ok := schemaMap["required"].([]any); ok {
			reqStrings := make([]string, len(required))
			for i, r := range required {
				if rs, ok := r.(string); ok {
					reqStrings[i] = rs
				}
			}
			result["required"] = reqStrings
		}
		if props, ok := schemaMap["properties"].(map[string]any); ok {
			result["properties"] = props
		}
		return result
	}
	return map[string]any{"type": "object"}
}

func (m *Manager) Close() {
	for _, server := range m.servers {
		if server.session != nil {
			server.session.Close()
		}
	}
}
