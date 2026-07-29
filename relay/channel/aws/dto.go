package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type AwsClaudeRequest struct {
	// AnthropicVersion should be "bedrock-2023-05-31"
	AnthropicVersion  string              `json:"anthropic_version"`
	AnthropicBeta     json.RawMessage     `json:"anthropic_beta,omitempty"`
	System            any                 `json:"system,omitempty"`
	Messages          []dto.ClaudeMessage `json:"messages"`
	MaxTokens         *uint               `json:"max_tokens,omitempty"`
	Temperature       *float64            `json:"temperature,omitempty"`
	TopP              *float64            `json:"top_p,omitempty"`
	TopK              *int                `json:"top_k,omitempty"`
	StopSequences     []string            `json:"stop_sequences,omitempty"`
	Tools             any                 `json:"tools,omitempty"`
	ToolChoice        any                 `json:"tool_choice,omitempty"`
	ContextManagement json.RawMessage     `json:"context_management,omitempty"`
	Thinking          *dto.Thinking       `json:"thinking,omitempty"`
	OutputConfig      json.RawMessage     `json:"output_config,omitempty"`
	//Metadata         json.RawMessage     `json:"metadata,omitempty"`
}

func formatRequest(requestBody io.Reader, requestHeader http.Header) (*AwsClaudeRequest, error) {
	var awsClaudeRequest AwsClaudeRequest
	err := common.DecodeJson(requestBody, &awsClaudeRequest)
	if err != nil {
		return nil, err
	}
	awsClaudeRequest.AnthropicVersion = "bedrock-2023-05-31"

	// check header anthropic-beta
	anthropicBetaValues := requestHeader.Get("anthropic-beta")
	if len(anthropicBetaValues) > 0 {
		var tempArray []string
		tempArray = strings.Split(anthropicBetaValues, ",")
		if len(tempArray) > 0 {
			betaJson, err := common.Marshal(tempArray)
			if err != nil {
				return nil, err
			}
			awsClaudeRequest.AnthropicBeta = betaJson
		}
	}
	logger.LogJson(context.Background(), "json", awsClaudeRequest)
	return &awsClaudeRequest, nil
}

// NovaMessage Nova模型使用messages-v1格式
type NovaMessage struct {
	Role    string        `json:"role"`
	Content []NovaContent `json:"content"`
}

type NovaContent struct {
	Text       string          `json:"text,omitempty"`
	ToolUse    *NovaToolUse    `json:"toolUse,omitempty"`
	ToolResult *NovaToolResult `json:"toolResult,omitempty"`
}

type NovaSystemContent struct {
	Text string `json:"text"`
}

type NovaToolUse struct {
	ToolUseID string `json:"toolUseId"`
	Name      string `json:"name"`
	Input     any    `json:"input"`
}

type NovaToolResult struct {
	ToolUseID string                  `json:"toolUseId"`
	Content   []NovaToolResultContent `json:"content"`
}

type NovaToolResultContent struct {
	Text string `json:"text"`
}

type NovaRequest struct {
	SchemaVersion   string               `json:"schemaVersion"` // 请求版本，例如 "1.0"
	System          []NovaSystemContent  `json:"system,omitempty"`
	Messages        []NovaMessage        `json:"messages"`                  // 对话消息列表
	InferenceConfig *NovaInferenceConfig `json:"inferenceConfig,omitempty"` // 推理配置，可选
	ToolConfig      *NovaToolConfig      `json:"toolConfig,omitempty"`
}

type NovaInferenceConfig struct {
	MaxTokens     *uint    `json:"maxTokens,omitempty"`     // 最大生成的 token 数
	Temperature   *float64 `json:"temperature,omitempty"`   // 随机性 (默认 0.7, 范围 0-1)
	TopP          *float64 `json:"topP,omitempty"`          // nucleus sampling (默认 0.9, 范围 0-1)
	TopK          *int     `json:"topK,omitempty"`          // 限制候选 token 数 (默认 50, 范围 0-128)
	StopSequences []string `json:"stopSequences,omitempty"` // 停止生成的序列
}

type NovaToolConfig struct {
	Tools      []NovaTool `json:"tools"`
	ToolChoice any        `json:"toolChoice,omitempty"`
}

type NovaTool struct {
	ToolSpec NovaToolSpec `json:"toolSpec"`
}

type NovaToolSpec struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	InputSchema NovaToolInputSchema `json:"inputSchema"`
}

type NovaToolInputSchema struct {
	JSON any `json:"json"`
}

// convertToNovaRequest translates the Chat Completions subset supported by
// Nova's messages-v1 schema. Unsupported media is rejected instead of being
// silently dropped, which keeps protocol capability claims truthful.
func convertToNovaRequest(req *dto.GeneralOpenAIRequest) (*NovaRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if req.N != nil && *req.N != 1 {
		return nil, fmt.Errorf("Nova supports exactly one completion, got n=%d", *req.N)
	}

	novaReq := &NovaRequest{
		SchemaVersion: "messages-v1",
		Messages:      make([]NovaMessage, 0, len(req.Messages)),
	}
	for messageIndex := range req.Messages {
		message := &req.Messages[messageIndex]
		content := make([]NovaContent, 0, len(message.ParseContent())+len(message.ParseToolCalls()))
		for _, block := range message.ParseContent() {
			if block.Type != dto.ContentTypeText {
				return nil, fmt.Errorf("Nova message %d contains unsupported %s content", messageIndex, block.Type)
			}
			content = append(content, NovaContent{Text: block.Text})
		}

		switch strings.ToLower(strings.TrimSpace(message.Role)) {
		case "system", "developer":
			if len(message.ParseToolCalls()) > 0 || message.ToolCallId != "" {
				return nil, fmt.Errorf("Nova system message %d cannot contain tool calls", messageIndex)
			}
			for _, block := range content {
				novaReq.System = append(novaReq.System, NovaSystemContent{Text: block.Text})
			}
		case "user":
			novaReq.Messages = append(novaReq.Messages, NovaMessage{Role: "user", Content: content})
		case "assistant":
			for _, toolCall := range message.ParseToolCalls() {
				if strings.TrimSpace(toolCall.Type) != "function" {
					return nil, fmt.Errorf("Nova assistant message %d contains unsupported %s tool call", messageIndex, toolCall.Type)
				}
				toolUseID := strings.TrimSpace(toolCall.ID)
				toolName := strings.TrimSpace(toolCall.Function.Name)
				if toolUseID == "" || toolName == "" {
					return nil, fmt.Errorf("Nova assistant message %d contains a tool call without id or name", messageIndex)
				}
				input := any(map[string]any{})
				if arguments := strings.TrimSpace(toolCall.Function.Arguments); arguments != "" {
					var object map[string]any
					if err := common.Unmarshal([]byte(arguments), &object); err != nil {
						return nil, fmt.Errorf("Nova tool call %q arguments must be a JSON object: %w", toolName, err)
					}
					input = object
				}
				content = append(content, NovaContent{ToolUse: &NovaToolUse{
					ToolUseID: toolUseID,
					Name:      toolName,
					Input:     input,
				}})
			}
			novaReq.Messages = append(novaReq.Messages, NovaMessage{Role: "assistant", Content: content})
		case "tool":
			toolUseID := strings.TrimSpace(message.ToolCallId)
			if toolUseID == "" {
				return nil, fmt.Errorf("Nova tool result message %d is missing tool_call_id", messageIndex)
			}
			var resultText strings.Builder
			for _, block := range content {
				resultText.WriteString(block.Text)
			}
			novaReq.Messages = append(novaReq.Messages, NovaMessage{
				Role: "user",
				Content: []NovaContent{{ToolResult: &NovaToolResult{
					ToolUseID: toolUseID,
					Content:   []NovaToolResultContent{{Text: resultText.String()}},
				}}},
			})
		default:
			return nil, fmt.Errorf("Nova message %d has unsupported role %q", messageIndex, message.Role)
		}
	}

	maxTokens := req.MaxTokens
	if req.MaxCompletionTokens != nil {
		maxTokens = req.MaxCompletionTokens
	}
	if maxTokens != nil || req.Temperature != nil || req.TopP != nil || req.TopK != nil || req.Stop != nil {
		novaReq.InferenceConfig = &NovaInferenceConfig{
			MaxTokens:   maxTokens,
			Temperature: req.Temperature,
			TopP:        req.TopP,
			TopK:        req.TopK,
		}
		if req.Stop != nil {
			if stopSequences := parseStopSequences(req.Stop); len(stopSequences) > 0 {
				novaReq.InferenceConfig.StopSequences = stopSequences
			}
		}
	}

	if len(req.Tools) > 0 {
		novaReq.ToolConfig = &NovaToolConfig{Tools: make([]NovaTool, 0, len(req.Tools))}
		for toolIndex, tool := range req.Tools {
			if strings.TrimSpace(tool.Type) != "function" {
				return nil, fmt.Errorf("Nova tool %d has unsupported type %q", toolIndex, tool.Type)
			}
			name := strings.TrimSpace(tool.Function.Name)
			if name == "" {
				return nil, fmt.Errorf("Nova tool %d is missing a function name", toolIndex)
			}
			parameters := tool.Function.Parameters
			if parameters == nil {
				parameters = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			novaReq.ToolConfig.Tools = append(novaReq.ToolConfig.Tools, NovaTool{ToolSpec: NovaToolSpec{
				Name:        name,
				Description: tool.Function.Description,
				InputSchema: NovaToolInputSchema{JSON: parameters},
			}})
		}
		toolChoice, err := convertNovaToolChoice(req.ToolChoice)
		if err != nil {
			return nil, err
		}
		novaReq.ToolConfig.ToolChoice = toolChoice
	} else if req.ToolChoice != nil {
		return nil, fmt.Errorf("Nova tool_choice requires at least one declared tool")
	}

	return novaReq, nil
}

func convertNovaToolChoice(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	if choice, ok := value.(string); ok {
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "", "auto":
			return map[string]any{"auto": map[string]any{}}, nil
		case "required", "any":
			return map[string]any{"any": map[string]any{}}, nil
		case "none":
			return nil, fmt.Errorf("Nova cannot represent tool_choice %q", choice)
		default:
			return nil, fmt.Errorf("Nova does not support tool_choice %q", choice)
		}
	}

	encoded, err := common.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("invalid Nova tool_choice: %w", err)
	}
	var choice map[string]any
	if err := common.Unmarshal(encoded, &choice); err != nil {
		return nil, fmt.Errorf("invalid Nova tool_choice: %w", err)
	}
	choiceType := strings.ToLower(strings.TrimSpace(common.Interface2String(choice["type"])))
	if choiceType == "auto" {
		return map[string]any{"auto": map[string]any{}}, nil
	}
	if choiceType == "required" || choiceType == "any" {
		return map[string]any{"any": map[string]any{}}, nil
	}
	if choiceType == "function" {
		name := strings.TrimSpace(common.Interface2String(choice["name"]))
		if nested, ok := choice["function"].(map[string]any); ok {
			name = strings.TrimSpace(common.Interface2String(nested["name"]))
		}
		if name == "" {
			return nil, fmt.Errorf("Nova function tool_choice is missing a name")
		}
		return map[string]any{"tool": map[string]any{"name": name}}, nil
	}
	return nil, fmt.Errorf("Nova does not support tool_choice type %q", choiceType)
}

// parseStopSequences 解析停止序列，支持字符串或字符串数组
func parseStopSequences(stop any) []string {
	if stop == nil {
		return nil
	}

	switch v := stop.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []string:
		return v
	case []interface{}:
		var sequences []string
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				sequences = append(sequences, str)
			}
		}
		return sequences
	}
	return nil
}
