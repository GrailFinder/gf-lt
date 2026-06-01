# MCP Server Integration

This application can connect to external MCP (Model Context Protocol) servers to extend available tools.

## Configuration

Add MCP server configurations to your `config.toml`:

```toml
ToolUse = true

[MCPServers.myserver]
url = "http://localhost:8099"
```

## Multiple Servers

You can connect to multiple MCP servers simultaneously:

```toml
ToolUse = true

[MCPServers.local]
url = "http://localhost:8099"

[MCPServers.production]
url = "http://production-mcp.example.com:8080"

[MCPServers.filesystem]
url = "http://localhost:9000"
```

Tools from each server are prefixed with their server name to avoid conflicts. For example, a tool named `read_file` from server `filesystem` becomes `mcp_filesystem_read_file`.

## Available Settings

| Setting | Description |
|---------|-------------|
| `url` | HTTP endpoint of the MCP server |

## VRAM Management

Some MCP servers run GPU-intensive tools (e.g. image generation, audio processing) that
need the same VRAM your LLM model occupies in llama.cpp. Since both can't fit
simultaneously, gf-lt can **unload the LLM model** before calling such tools and
**reload it** after they complete.

On the imgen server side, each request spawns a fresh `sd`/`sd-cli` subprocess
with `--offload-to-cpu`, so VRAM is only used during active inference and freed
immediately after the subprocess exits.

### Configuration

```toml
[ModelManagement]
VRAMFreeServers = ["imgen"]
```

- **`VRAMFreeServers`** — list of MCP server names (matching the `[MCPServers.*]` key)
  whose tool calls should trigger the unload/reload cycle.

### Behavior

When the LLM calls any tool from a listed server (e.g. `mcp_imgen_image.generate`):

1. The currently loaded model is unloaded via `POST /models/unload`
2. The MCP tool executes with full VRAM available
3. The original model is reloaded before the next inference

If no model is currently loaded, or the config section is absent, the cycle is skipped.

## Requirements

- `ToolUse` must be set to `true`
- The MCP server must be running and accessible at the configured URL
- The server should implement the MCP specification with tool support