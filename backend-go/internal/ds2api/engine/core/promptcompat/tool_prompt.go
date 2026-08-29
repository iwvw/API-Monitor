package promptcompat

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
)

const CurrentToolsContextFilename = "TOOLS.txt"

const toolsTranscriptTitle = "# TOOLS.txt"
const toolsTranscriptSummary = "Available tool descriptions and parameter schemas for this request."

type toolPromptParts struct {
	Descriptions string
	Instructions string
	Names        []string
}

func injectToolPrompt(messages []map[string]any, tools []any, policy ToolChoicePolicy) ([]map[string]any, []string) {
	return injectToolPromptWithDescriptions(messages, tools, policy, true)
}

func injectToolPromptInstructionsOnly(messages []map[string]any, tools []any, policy ToolChoicePolicy) ([]map[string]any, []string) {
	return injectToolPromptWithDescriptions(messages, tools, policy, false)
}

func injectToolPromptWithDescriptions(messages []map[string]any, tools []any, policy ToolChoicePolicy, includeDescriptions bool) ([]map[string]any, []string) {
	if policy.IsNone() {
		return messages, nil
	}
	parts := buildToolPromptParts(tools, policy)
	if parts.Instructions == "" {
		return messages, parts.Names
	}
	if includeDescriptions {
		messages = mergeToolPromptDescriptions(messages, parts.Descriptions)
	}
	return appendToolCallFormatSpec(messages, parts.Instructions), parts.Names
}

// injectToolCallFormatSpecOnly appends the tool-name-independent tool-call
// format spec (EPSE syntax rules) without any tool descriptions or schemas. It
// is used for requests that expose their tool catalog inside a system message
// (so there is no top-level `tools` array to extract schemas from). The model
// still needs to be told to emit tool calls in the <|EPSE|tool_calls> format,
// which this block supplies. No tool names are returned because none can be
// reliably extracted from free-form system text; stream tool-call detection
// does not depend on the tool-name list.
func injectToolCallFormatSpecOnly(messages []map[string]any, policy ToolChoicePolicy) ([]map[string]any, []string) {
	if policy.IsNone() {
		return messages, nil
	}
	spec := toolcall.ToolCallFormatSpec()
	return appendToolCallFormatSpec(messages, spec), nil
}

// mergeToolPromptDescriptions keeps the tool descriptions/schemas in the
// system prompt: appended to the first system message, or a new leading system
// message when none exists.
func mergeToolPromptDescriptions(messages []map[string]any, descriptions string) []map[string]any {
	descriptions = strings.TrimSpace(descriptions)
	if descriptions == "" {
		return messages
	}
	for i := range messages {
		if messages[i]["role"] == "system" {
			old, _ := messages[i]["content"].(string)
			messages[i]["content"] = strings.TrimSpace(old + "\n\n" + descriptions)
			return messages
		}
	}
	return append([]map[string]any{{"role": "system", "content": descriptions}}, messages...)
}

// appendToolCallFormatSpec appends the tool-call format spec as a system-role
// message at the end of the message sequence, so it lands immediately before
// the final <Assistant> completion marker. When the sequence already ends with
// an assistant message, the block is inserted before that trailing assistant
// turn instead.
func appendToolCallFormatSpec(messages []map[string]any, spec string) []map[string]any {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return messages
	}
	insertAt := len(messages)
	if n := len(messages); n > 0 {
		if role, _ := messages[n-1]["role"].(string); strings.EqualFold(strings.TrimSpace(role), "assistant") {
			insertAt = n - 1
		}
	}
	block := map[string]any{
		"role":    "system",
		"content": spec,
	}
	out := make([]map[string]any, 0, len(messages)+1)
	out = append(out, messages[:insertAt]...)
	out = append(out, block)
	out = append(out, messages[insertAt:]...)
	return out
}

func buildToolPromptParts(tools []any, policy ToolChoicePolicy) toolPromptParts {
	toolSchemas := make([]string, 0, len(tools))
	names := make([]string, 0, len(tools))
	isAllowed := func(name string) bool {
		if strings.TrimSpace(name) == "" {
			return false
		}
		if len(policy.Allowed) == 0 {
			return true
		}
		_, ok := policy.Allowed[name]
		return ok
	}

	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name, desc, schema := toolcall.ExtractToolMeta(tool)
		name = strings.TrimSpace(name)
		if !isAllowed(name) {
			continue
		}
		names = append(names, name)
		if desc == "" {
			desc = "No description available"
		}
		b, _ := json.Marshal(schema)
		toolSchemas = append(toolSchemas, fmt.Sprintf("Tool: %s\nDescription: %s\nParameters: %s", name, desc, string(b)))
	}
	if len(toolSchemas) == 0 {
		return toolPromptParts{Names: names}
	}
	descriptions := "You have access to these tools:\n\n" + strings.Join(toolSchemas, "\n\n")
	instructions := toolcall.BuildToolCallInstructions(names)
	if hasReadLikeTool(names) {
		instructions += "\n\nRead-tool cache guard: If a Read/read_file-style tool result says the file is unchanged, already available in history, should be referenced from previous context, or otherwise provides no file body, treat that result as missing content. Do not repeatedly call the same read request for that missing body. Request a full-content read if the tool supports it, or tell the user that the file contents need to be provided again."
	}
	if policy.Mode == ToolChoiceRequired {
		instructions += "\n7) For this response, you MUST call at least one tool from the allowed list."
	}
	if policy.Mode == ToolChoiceForced && strings.TrimSpace(policy.ForcedName) != "" {
		instructions += "\n7) For this response, you MUST call exactly this tool name: " + strings.TrimSpace(policy.ForcedName)
		instructions += "\n8) Do not call any other tool."
	}
	return toolPromptParts{
		Descriptions: descriptions,
		Instructions: instructions,
		Names:        names,
	}
}

func BuildOpenAIToolsContextTranscript(toolsRaw any, policy ToolChoicePolicy) (string, []string) {
	if policy.IsNone() {
		return "", nil
	}
	tools, ok := toolsRaw.([]any)
	if !ok || len(tools) == 0 {
		return "", nil
	}
	parts := buildToolPromptParts(tools, policy)
	if strings.TrimSpace(parts.Descriptions) == "" {
		return "", parts.Names
	}
	var b strings.Builder
	b.WriteString(toolsTranscriptTitle)
	b.WriteString("\n")
	b.WriteString(toolsTranscriptSummary)
	b.WriteString("\n\n")
	b.WriteString(parts.Descriptions)
	b.WriteString("\n")
	return b.String(), parts.Names
}

func hasReadLikeTool(names []string) bool {
	for _, name := range names {
		switch normalizeToolNameForGuard(name) {
		case "read", "readfile":
			return true
		}
	}
	return false
}

func normalizeToolNameForGuard(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
