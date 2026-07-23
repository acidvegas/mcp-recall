// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/learn/httpclient_test.go
//
// Partial port of tests/learn-http-client.test.ts (upstream v1.9.0).
//
// Divergence note: the upstream test suite exercises a hand-rolled HTTP/SSE
// JSON-RPC client (bespoke SSE framing parser, `endpoint` event handling,
// scheme validation, "MCP error:" message shaping, combined
// "streamable HTTP: … legacy SSE: …" errors). The Go port replaces all of that
// with the official go-sdk transports (StreamableClientTransport +
// SSEClientTransport), so those parsing-detail assertions test SDK internals
// rather than our code. What remains meaningfully ours is the two-transport
// fallback orchestration and the failure path, covered here. The happy path is
// covered end-to-end against the real built server in learn_test.go
// (TestListMcpToolsStdioAgainstServer, stdio transport).

package learn

import (
	"testing"
	"time"
)

func TestListMcpToolsHTTPUnreachable(t *testing.T) {
	// Port 1 is virtually always refused → both transports fail, error returned.
	_, err := listMcpToolsHTTP("http://127.0.0.1:1/mcp", 2*time.Second)
	if err == nil {
		t.Fatal("expected an error for an unreachable endpoint")
	}
}
