package bridge

import (
	"encoding/json"
	"strings"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// Codex's local_shell tool is executed by the client — the model only issues
// exec commands and the Codex CLI runs them locally. Unlike genuinely hosted
// tools it can therefore be lowered onto any upstream as a plain function tool
// and restored as local_shell_call items on the way back, the same way
// tool_search is bridged.

const LocalShellToolName = "local_shell"

// LocalShellFunctionDescription is shown to upstream models for the lowered tool.
const LocalShellFunctionDescription = "Execute a shell command on the user's machine. " +
	"The command runs locally in the Codex CLI and its output is returned to you."

// LocalShellFunctionParameters mirrors the exec action Codex accepts on
// local_shell_call items so lowered calls round-trip losslessly.
func LocalShellFunctionParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": `Command to execute as an argv vector, e.g. ["bash","-lc","ls -la"].`,
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Optional timeout in milliseconds.",
			},
			"working_directory": map[string]any{
				"type":        "string",
				"description": "Optional working directory for the command.",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

// LocalShellCallArguments flattens a local_shell_call "action" object into the
// lowered function's argument object (dropping the action "type" discriminator
// so history matches the declared parameter schema).
func LocalShellCallArguments(action any) string {
	object, _ := action.(map[string]any)
	args := make(map[string]any, len(object))
	for key, value := range object {
		if key == "type" {
			continue
		}
		args[key] = value
	}
	raw, err := kitutil.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// LocalShellActionRaw rebuilds a local_shell_call exec action from lowered
// function-call arguments. A string command degrades to a bash -lc invocation;
// anything unusable becomes an empty argv so the client never executes garbage.
func LocalShellActionRaw(arguments string) json.RawMessage {
	var parsed map[string]any
	_ = kitutil.Unmarshal([]byte(arguments), &parsed)
	if parsed == nil {
		parsed = map[string]any{}
	}
	switch command := parsed["command"].(type) {
	case []any:
	case string:
		if strings.TrimSpace(command) == "" {
			parsed["command"] = []string{}
		} else {
			parsed["command"] = []string{"bash", "-lc", command}
		}
	default:
		parsed["command"] = []string{}
	}
	parsed["type"] = "exec"
	raw, err := kitutil.Marshal(parsed)
	if err != nil {
		return json.RawMessage(`{"type":"exec","command":[]}`)
	}
	return raw
}
