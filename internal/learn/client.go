// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/learn/client.go

package learn

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcprecall/internal/jsonx"
)

func clientImpl() *mcp.Implementation {
	return &mcp.Implementation{Name: "mcp-recall-learn", Version: "1.0.0"}
}

// toolProps extracts the input-schema property names. The SDK exposes
// InputSchema as `any`; marshal it back to JSON to read the property keys.
func toolProps(schema any) []string {
	if schema == nil {
		return nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	v, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	obj, ok := v.(*jsonx.Obj)
	if !ok {
		return nil
	}
	pv, ok := obj.Get("properties")
	if !ok {
		return nil
	}
	props, ok := pv.(*jsonx.Obj)
	if !ok {
		return nil
	}
	return props.Keys()
}

func convertTools(tools []*mcp.Tool) []McpTool {
	out := make([]McpTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, McpTool{
			Name:        t.Name,
			Description: t.Description,
			InputProps:  toolProps(t.InputSchema),
		})
	}
	return out
}

// listMcpToolsStdio spawns a stdio MCP server, handshakes, and lists its tools.
func listMcpToolsStdio(command string, args []string, env map[string]string, timeout time.Duration) ([]McpTool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	client := mcp.NewClient(clientImpl(), nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	return convertTools(res.Tools), nil
}

// httpResult reports the tools plus which transport succeeded.
type httpResult struct {
	tools           []McpTool
	transport       string
	streamableError string
}

// listMcpToolsHTTP connects over Streamable HTTP, falling back to legacy SSE.
func listMcpToolsHTTP(url string, timeout time.Duration) (httpResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Streamable HTTP first.
	client := mcp.NewClient(clientImpl(), nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	if err == nil {
		res, lerr := session.ListTools(ctx, nil)
		session.Close()
		if lerr == nil {
			return httpResult{tools: convertTools(res.Tools), transport: "streamable-http"}, nil
		}
		err = lerr
	}
	streamableErr := err.Error()

	// Legacy SSE fallback.
	client2 := mcp.NewClient(clientImpl(), nil)
	session2, serr := client2.Connect(ctx, &mcp.SSEClientTransport{Endpoint: url}, nil)
	if serr != nil {
		return httpResult{}, serr
	}
	defer session2.Close()
	res, lerr := session2.ListTools(ctx, nil)
	if lerr != nil {
		return httpResult{}, lerr
	}
	return httpResult{tools: convertTools(res.Tools), transport: "sse", streamableError: streamableErr}, nil
}
