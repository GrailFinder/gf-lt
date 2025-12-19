package agent

// I see two types of agents possible:
// ones who do their own tools calls
// ones that works only with the output

// A: main chat -> agent (handles everything: tool + processing)
// B: main chat -> tool -> agent (process tool output)

// AgenterA gets a task "find out weather in london"
// proceeds to make tool calls on its own
type AgenterA interface {
	ProcessTask(task string) []byte
}

// AgenterB defines an interface for processing tool outputs
type AgenterB interface {
	// Process takes the original tool arguments and the raw output from the tool,
	// and returns a cleaned/summarized version suitable for the main LLM context
	Process(args map[string]string, rawOutput []byte) []byte
}

// registry holds mapping from tool names to agents
var RegistryB = make(map[string]AgenterB)
var RegistryA = make(map[AgenterA][]string)

// Register adds an agent for a specific tool name
// If an agent already exists for the tool, it will be replaced
func RegisterB(toolName string, a AgenterB) {
	RegistryB[toolName] = a
}

func RegisterA(toolNames []string, a AgenterA) {
	RegistryA[a] = toolNames
}
