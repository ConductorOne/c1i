package cmd

var mcpToolsHistoryCmd = toolItems.historyCmd("Stream the change history for a single MCP tool (NDJSON output)",
	`One NDJSON record per historical version of the tool, newest-first as
the server returns them. The record carries both the full tool snapshot at
that point in time and the change-history envelope (actor, change_kind,
created_at, trace_id, syslog_event_id, annotations).`)

func init() { toolItems.registerHistory(mcpToolsCmd, mcpToolsHistoryCmd) }
