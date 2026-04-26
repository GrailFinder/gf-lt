package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
}

func TestManagerToolDiscovery(t *testing.T) {
	mcpTool := mcp.Tool{
		Name:        "echo",
		Description: "Echo back the input",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Text to echo",
				},
			},
			"required": []any{"text"},
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

	t1, t2 := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)

	server.AddTool(&mcp.Tool{
		Name:        "echo",
		Description: "Echo back the input",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type": "string",
				},
			},
			"required": []any{"text"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		text := args["text"].(string)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "echo: " + text},
			},
		}, nil
	})

	server.AddTool(&mcp.Tool{
		Name:        "add",
		Description: "Add two numbers",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "number"},
				"b": map[string]any{"type": "number"},
			},
			"required": []any{"a", "b"},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		a := args["a"].(float64)
		b := args["b"].(float64)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: resultFromFloat(a + b)},
			},
		}, nil
	})

	_, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "gf-lt", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(result.Tools))
	}

	callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "echo",
		Arguments: map[string]any{
			"text": "hello",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	textContent := callResult.Content[0].(*mcp.TextContent)
	if textContent.Text != "echo: hello" {
		t.Errorf("Expected 'echo: hello', got '%s'", textContent.Text)
	}

	addResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "add",
		Arguments: map[string]any{
			"a": 5.0,
			"b": 3.0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	addContent := addResult.Content[0].(*mcp.TextContent)
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
