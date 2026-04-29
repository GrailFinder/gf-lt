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

## Requirements

- `ToolUse` must be set to `true`
- The MCP server must be running and accessible at the configured URL
- The server should implement the MCP specification with tool support