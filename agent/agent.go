package agent

// Agent defines an interface for processing tool outputs.
// An Agent can clean, summarize, or otherwise transform raw tool outputs
// before they are presented to the main LLM.
type Agent interface {
	// Process takes the original tool arguments and the raw output from the tool,
	// and returns a cleaned/summarized version suitable for the main LLM context.
	Process(args map[string]string, rawOutput []byte) []byte
}

// registry holds mapping from tool names to agents.
var registry = make(map[string]Agent)

// Register adds an agent for a specific tool name.
// If an agent already exists for the tool, it will be replaced.
func Register(toolName string, a Agent) {
	registry[toolName] = a
}

// Get returns the agent for a tool name, or nil if none is registered.
func Get(toolName string) Agent {
	return registry[toolName]
}

// FormatterAgent is a simple agent that applies formatting functions.
type FormatterAgent struct {
	formatFunc func([]byte) (string, error)
}

// NewFormatterAgent creates a FormatterAgent that uses the given formatting function.
func NewFormatterAgent(formatFunc func([]byte) (string, error)) *FormatterAgent {
	return &FormatterAgent{formatFunc: formatFunc}
}

// Process applies the formatting function to raw output.
func (a *FormatterAgent) Process(args map[string]string, rawOutput []byte) []byte {
	if a.formatFunc == nil {
		return rawOutput
	}
	formatted, err := a.formatFunc(rawOutput)
	if err != nil {
		// On error, return raw output with a warning prefix
		return []byte("[formatting failed, showing raw output]\n" + string(rawOutput))
	}
	return []byte(formatted)
}

// DefaultFormatter returns a FormatterAgent that uses the appropriate formatting
// based on tool name.
func DefaultFormatter(toolName string) Agent {
	switch toolName {
	case "websearch":
		return NewFormatterAgent(FormatSearchResults)
	case "read_url":
		return NewFormatterAgent(FormatWebPageContent)
	default:
		return nil
	}
}