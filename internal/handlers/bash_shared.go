// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/bash_shared.go

package handlers

import (
	"strings"

	"mcprecall/internal/jsonx"
)

const (
	maxLogCommits        = 20
	maxTerraformResource = 10
	maxDockerContainers  = 20
	maxBuildErrors       = 20
)

// extractStdout extracts plain stdout from a native Bash tool response
// {exit_code, stdout, stderr} or falls back to ExtractText for other shapes.
func extractStdout(output any) string {
	if obj, ok := output.(*jsonx.Obj); ok {
		if s, ok := objStr(obj, "stdout"); ok {
			return stripSshNoise(stripAnsi(s))
		}
		if s, ok := objStr(obj, "output"); ok {
			return stripSshNoise(stripAnsi(s))
		}
	}
	text := ExtractText(output)
	if parsed, err := jsonx.ParseString(text); err == nil {
		if obj, ok := parsed.(*jsonx.Obj); ok {
			if s, ok := objStr(obj, "stdout"); ok {
				return stripSshNoise(stripAnsi(s))
			}
			if s, ok := objStr(obj, "output"); ok {
				return stripSshNoise(stripAnsi(s))
			}
		}
	}
	return stripSshNoise(stripAnsi(text))
}

// extractStderr extracts plain stderr from a native Bash tool response.
func extractStderr(output any) string {
	if obj, ok := output.(*jsonx.Obj); ok {
		if s, ok := objStr(obj, "stderr"); ok {
			return stripAnsi(s)
		}
	}
	return ""
}

// extractCommand extracts the command string from tool_input, or "" if absent.
func extractCommand(input any) string {
	if obj, ok := input.(*jsonx.Obj); ok {
		if s, ok := objStr(obj, "command"); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
