package main

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"
)

func TestMCPClientListTools(t *testing.T) {
	ctx := context.Background()

	srv, err := mcptest.NewServer(t,
		server.ServerTool{
			Tool: mcp.NewTool(
				"test_echo",
				mcp.WithDescription("Echo back the input text"),
				mcp.WithString("text", mcp.Required(), mcp.Description("Text to echo")),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				text, _ := req.RequireString("text")
				return mcp.NewToolResultText(text), nil
			},
		},
		server.ServerTool{
			Tool: mcp.NewTool(
				"test_add",
				mcp.WithDescription("Add two numbers"),
				mcp.WithNumber("a", mcp.Required(), mcp.Description("First number")),
				mcp.WithNumber("b", mcp.Required(), mcp.Description("Second number")),
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

	client := srv.Client()

	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(result.Tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	if !toolNames["test_echo"] {
		t.Error("Expected tool test_echo not found")
	}
	if !toolNames["test_add"] {
		t.Error("Expected tool test_add not found")
	}
}

func TestMCPClientCallTool(t *testing.T) {
	ctx := context.Background()

	srv, err := mcptest.NewServer(t,
		server.ServerTool{
			Tool: mcp.NewTool(
				"test_echo",
				mcp.WithDescription("Echo back the input text"),
				mcp.WithString("text", mcp.Required(), mcp.Description("Text to echo")),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				text, _ := req.RequireString("text")
				return mcp.NewToolResultText(text), nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	client := srv.Client()

	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "test_echo",
			Arguments: map[string]any{
				"text": "hello world",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.IsError {
		t.Error("Tool call returned error")
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected content in result")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("Expected TextContent")
	}

	if textContent.Text != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", textContent.Text)
	}
}

func TestMCPClientToolError(t *testing.T) {
	ctx := context.Background()

	srv, err := mcptest.NewServer(t,
		server.ServerTool{
			Tool: mcp.NewTool(
				"test_error",
				mcp.WithDescription("Tool that returns an error"),
				mcp.WithString("message", mcp.Required(), mcp.Description("Error message")),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				msg, _ := req.RequireString("message")
				return mcp.NewToolResultError(msg), nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	client := srv.Client()

	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "test_error",
			Arguments: map[string]any{
				"message": "something went wrong",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !result.IsError {
		t.Error("Expected error result")
	}
}

func resultFromFloat(f float64) string {
	if f == float64(int64(f)) {
		return resultFromInt(int64(f))
	}
	return resultFromInt(int64(f)) // simpler for now
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
