package cmd

var mcpToolsGetCmd = toolItems.getCmd("Get a single MCP tool (pretty JSON)", "")

func init() { toolItems.registerGet(mcpToolsCmd, mcpToolsGetCmd) }
