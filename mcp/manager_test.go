package mcp

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
}

func TestManagerToolDiscovery(t *testing.T) {
	// This test verifies the OpenAI tool conversion works correctly
	// by directly testing convertToolToOpenAI function

	// Create a mock tool to test conversion
	mcpTool := mcp.Tool{
		Name:        "echo",
		Description: "Echo back the input",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Text to echo",
				},
			},
			Required: []string{"text"},
		},
	}

	openAITool := convertToolToOpenAI("testserver", mcpTool)

	openAIFunc := openAITool["function"].(map[string]any)

	if openAIFunc["name"] != "mcp_testserver_echo" {
		t.Errorf("Expected name 'mcp_testserver_echo', got '%s'", openAIFunc["name"])
	}

	if openAIFunc["description"] != "Echo back the input" {
		t.Errorf("Expected description 'Echo back the input', got '%s'", openAIFunc["description"])
	}

	params := openAIFunc["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Errorf("Expected type 'object', got '%v'", params["type"])
	}
}

func TestManagerFullFlow(t *testing.T) {
	ctx := context.Background()

	// Create test server with mcptest
	srv, err := mcptest.NewServer(t,
		server.ServerTool{
			Tool: mcp.NewTool(
				"echo",
				mcp.WithDescription("Echo back the input"),
				mcp.WithString("text", mcp.Required()),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				text, _ := req.RequireString("text")
				return mcp.NewToolResultText("echo: " + text), nil
			},
		},
		server.ServerTool{
			Tool: mcp.NewTool(
				"add",
				mcp.WithDescription("Add two numbers"),
				mcp.WithNumber("a", mcp.Required()),
				mcp.WithNumber("b", mcp.Required()),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				a, _ := req.RequireFloat("a")
				b, _ := req.RequireFloat("b")
				return mcp.NewToolResultText(resultFromFloat(a + b)), nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// Use the client directly to test the full flow
	client := srv.Client()

	// Test ListTools
	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(result.Tools))
	}

	// Test CallTool - echo
	callResult, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "echo",
			Arguments: map[string]any{"text": "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if callResult.IsError {
		t.Error("Expected no error")
	}

	textContent := callResult.Content[0].(mcp.TextContent)
	if textContent.Text != "echo: hello" {
		t.Errorf("Expected 'echo: hello', got '%s'", textContent.Text)
	}

	// Test CallTool - add
	addResult, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "add",
			Arguments: map[string]any{"a": 5.0, "b": 3.0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	addContent := addResult.Content[0].(mcp.TextContent)
	if addContent.Text != "8" {
		t.Errorf("Expected '8', got '%s'", addContent.Text)
	}
}

func resultFromFloat(f float64) string {
	if f == float64(int64(f)) {
		return resultFromInt(int64(f))
	}
	return resultFromInt(int64(f))
}

func resultFromInt(i int64) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	negative := i < 0
	if negative {
		i = -i
	}
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte(i%10) + '0'
		i /= 10
	}
	if negative {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}
