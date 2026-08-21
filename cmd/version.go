package cmd

import "github.com/ConductorOne/c1i/internal/transport"

// Version aliases internal/transport's build-info-derived version, the same
// string every outbound request already identifies itself with (the
// user-agent, and an MCP gateway handshake's clientInfo.version) — one
// source, not a second copy of the debug.ReadBuildInfo() logic here.
var Version = transport.Version
