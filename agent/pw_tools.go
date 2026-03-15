package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gf-lt/models"
)

type ToolFunc func(map[string]string) []byte

var pwToolMap = make(map[string]ToolFunc)

func RegisterPWTool(name string, fn ToolFunc) {
	pwToolMap[name] = fn
}

func GetPWTools() []models.Tool {
	return pwTools
}

var pwTools = []models.Tool{
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_start",
			Description: "Start a Playwright browser instance. Must be called first before any other browser automation. Uses headless mode by default.",
			Parameters: models.ToolFuncParams{
				Type:       "object",
				Required:   []string{},
				Properties: map[string]models.ToolArgProps{},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_stop",
			Description: "Stop the Playwright browser instance. Call when done with browser automation.",
			Parameters: models.ToolFuncParams{
				Type:       "object",
				Required:   []string{},
				Properties: map[string]models.ToolArgProps{},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_is_running",
			Description: "Check if Playwright browser is currently running.",
			Parameters: models.ToolFuncParams{
				Type:       "object",
				Required:   []string{},
				Properties: map[string]models.ToolArgProps{},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_navigate",
			Description: "Navigate to a URL in the browser.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"url"},
				Properties: map[string]models.ToolArgProps{
					"url": {Type: "string", Description: "URL to navigate to"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_click",
			Description: "Click on an element on the current webpage. Use 'index' for multiple matches (default 0).",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"selector"},
				Properties: map[string]models.ToolArgProps{
					"selector": {Type: "string", Description: "CSS selector for the element"},
					"index":    {Type: "integer", Description: "Index for multiple matches (default 0)"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_fill",
			Description: "Type text into an input field. Use 'index' for multiple matches (default 0).",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"selector", "text"},
				Properties: map[string]models.ToolArgProps{
					"selector": {Type: "string", Description: "CSS selector for the input element"},
					"text":     {Type: "string", Description: "Text to type into the field"},
					"index":    {Type: "integer", Description: "Index for multiple matches (default 0)"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_extract_text",
			Description: "Extract text content from the page or specific elements. Use selector 'body' for all page text.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"selector": {Type: "string", Description: "CSS selector (default 'body' for all page text)"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_screenshot",
			Description: "Take a screenshot of the page or a specific element. Returns a file path to the image.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"selector":  {Type: "string", Description: "CSS selector for element to screenshot"},
					"full_page": {Type: "boolean", Description: "Capture full page (default false)"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_screenshot_and_view",
			Description: "Take a screenshot and return the image for viewing. Use to visually verify page state.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"selector":  {Type: "string", Description: "CSS selector for element to screenshot"},
					"full_page": {Type: "boolean", Description: "Capture full page (default false)"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_wait_for_selector",
			Description: "Wait for an element to appear on the page before proceeding.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"selector"},
				Properties: map[string]models.ToolArgProps{
					"selector": {Type: "string", Description: "CSS selector to wait for"},
					"timeout":  {Type: "integer", Description: "Timeout in milliseconds (default 30000)"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_drag",
			Description: "Drag the mouse from point (x1,y1) to (x2,y2).",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"x1", "y1", "x2", "y2"},
				Properties: map[string]models.ToolArgProps{
					"x1": {Type: "number", Description: "Starting X coordinate"},
					"y1": {Type: "number", Description: "Starting Y coordinate"},
					"x2": {Type: "number", Description: "Ending X coordinate"},
					"y2": {Type: "number", Description: "Ending Y coordinate"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_click_at",
			Description: "Click at specific X,Y coordinates on the page.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"x", "y"},
				Properties: map[string]models.ToolArgProps{
					"x": {Type: "number", Description: "X coordinate"},
					"y": {Type: "number", Description: "Y coordinate"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_get_html",
			Description: "Get the HTML content of the page or a specific element.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"selector": {Type: "string", Description: "CSS selector (default 'body')"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_get_dom",
			Description: "Get a structured DOM representation with tag, attributes, text, and children.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"selector": {Type: "string", Description: "CSS selector (default 'body')"},
				},
			},
		},
	},
	{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pw_search_elements",
			Description: "Search for elements by text content or CSS selector.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"text":     {Type: "string", Description: "Text content to search for"},
					"selector": {Type: "string", Description: "CSS selector to search for"},
				},
			},
		},
	},
}

var toolCallRE = regexp.MustCompile(`__tool_call__(.+?)__tool_call__`)

type ParsedToolCall struct {
	ID   string
	Name string
	Args map[string]string
}

func findToolCall(resp []byte) (func() []byte, string, bool) {
	var genericResp map[string]interface{}
	if err := json.Unmarshal(resp, &genericResp); err != nil {
		return findToolCallFromText(string(resp))
	}
	if choices, ok := genericResp["choices"].([]interface{}); ok && len(choices) > 0 {
		if firstChoice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := firstChoice["message"].(map[string]interface{}); ok {
				if toolCalls, ok := message["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
					return parseOpenAIToolCall(toolCalls)
				}
				if content, ok := message["content"].(string); ok {
					return findToolCallFromText(content)
				}
			}
			if text, ok := firstChoice["text"].(string); ok {
				return findToolCallFromText(text)
			}
		}
	}
	if content, ok := genericResp["content"].(string); ok {
		return findToolCallFromText(content)
	}
	return findToolCallFromText(string(resp))
}

func parseOpenAIToolCall(toolCalls []interface{}) (func() []byte, string, bool) {
	if len(toolCalls) == 0 {
		return nil, "", false
	}
	tc := toolCalls[0].(map[string]interface{})
	id, _ := tc["id"].(string)
	function, _ := tc["function"].(map[string]interface{})
	name, _ := function["name"].(string)
	argsStr, _ := function["arguments"].(string)
	var args map[string]string
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return func() []byte {
			return []byte(fmt.Sprintf(`{"error": "failed to parse arguments: %v"}`, err))
		}, id, true
	}
	return func() []byte {
		fn, ok := pwToolMap[name]
		if !ok {
			return []byte(fmt.Sprintf(`{"error": "tool %s not found"}`, name))
		}
		return fn(args)
	}, id, true
}

func findToolCallFromText(text string) (func() []byte, string, bool) {
	jsStr := toolCallRE.FindString(text)
	if jsStr == "" {
		return nil, "", false
	}
	jsStr = strings.TrimSpace(jsStr)
	jsStr = strings.TrimPrefix(jsStr, "__tool_call__")
	jsStr = strings.TrimSuffix(jsStr, "__tool_call__")
	jsStr = strings.TrimSpace(jsStr)
	start := strings.Index(jsStr, "{")
	end := strings.LastIndex(jsStr, "}")
	if start == -1 || end == -1 || end <= start {
		return func() []byte {
			return []byte(`{"error": "no valid JSON found in tool call"}`)
		}, "", true
	}
	jsStr = jsStr[start : end+1]
	var fc models.FuncCall
	if err := json.Unmarshal([]byte(jsStr), &fc); err != nil {
		return func() []byte {
			return []byte(fmt.Sprintf(`{"error": "failed to parse tool call: %v}`, err))
		}, "", true
	}
	if fc.ID == "" {
		fc.ID = "call_" + generateToolCallID()
	}
	return func() []byte {
		fn, ok := pwToolMap[fc.Name]
		if !ok {
			return []byte(fmt.Sprintf(`{"error": "tool %s not found"}`, fc.Name))
		}
		return fn(fc.Args)
	}, fc.ID, true
}

func generateToolCallID() string {
	return strconv.Itoa(len(pwToolMap) % 10000)
}
