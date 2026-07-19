// Package litellm implements model.LLM for any OpenAI-compatible endpoint,
// including a local LiteLLM proxy (http://localhost:4000).
package litellm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Model routes requests to a LiteLLM (OpenAI-compatible) endpoint.
type Model struct {
	name    string
	baseURL string
	apiKey  string
}

// New returns a Model targeting baseURL (e.g. "http://localhost:4000").
// name is passed as the "model" field in the request (e.g. "gpt-4o", "ollama/llama3").
func New(name, baseURL, apiKey string) *Model {
	return &Model{name: name, baseURL: baseURL, apiKey: apiKey}
}

func (m *Model) Name() string { return m.name }

// ---- OpenAI-compatible wire types ----------------------------------------

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Tools          []chatTool      `json:"tools,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Temperature    *float32        `json:"temperature,omitempty"`
	TopP           *float32        `json:"top_p,omitempty"`
	TopK           *float32        `json:"top_k,omitempty"`
}

type responseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *jsonSchema `json:"json_schema,omitempty"`
}

type jsonSchema struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Strict bool   `json:"strict"`
}

type chatMessage struct {
	Role       string      `json:"role"`
	Content    string      `json:"content,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCall  `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ---- model.LLM implementation --------------------------------------------

func (m *Model) GenerateContent(ctx context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		msgs := buildMessages(req)

		// If the conversation already contains a tool result, don't send tools.
		// This forces the model to give a text response instead of looping back
		// into tool calls — works around models that keep calling tools indefinitely.
		tools := extractTools(req)
		if hasToolResult(msgs) {
			tools = nil
		}

		payload := chatRequest{
			Model:          m.name,
			Messages:       msgs,
			Tools:          tools,
			ResponseFormat: buildResponseFormat(req),
		}
		if req.Config != nil {
			payload.Temperature = req.Config.Temperature
			payload.TopP = req.Config.TopP
			payload.TopK = req.Config.TopK
		}

		body, err := json.Marshal(payload)
		if err != nil {
			yield(nil, fmt.Errorf("litellm marshal: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			m.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			yield(nil, fmt.Errorf("litellm new request: %w", err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if m.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("litellm http: %w", err))
			return
		}
		defer resp.Body.Close()

		var chatResp chatResponse
		if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
			yield(nil, fmt.Errorf("litellm decode: %w", err))
			return
		}

		if chatResp.Error != nil {
			yield(nil, fmt.Errorf("litellm api error: %s", chatResp.Error.Message))
			return
		}
		if len(chatResp.Choices) == 0 {
			yield(nil, fmt.Errorf("litellm: no choices"))
			return
		}

		choice := chatResp.Choices[0]
		content := messageToContent(choice.Message)

		// TurnComplete signals whether the model is done for this turn.
		// For proper tool_calls (finish_reason="tool_calls"), set false so ADK
		// executes the tools and calls the model again.
		// For text-based tool calls (parsed from content), the finish_reason is
		// "stop" but we still need ADK to loop — detect by checking FunctionCall parts.
		hasFuncCall := false
		if content != nil {
			for _, p := range content.Parts {
				if p.FunctionCall != nil {
					hasFuncCall = true
					break
				}
			}
		}
		turnComplete := !hasFuncCall && (choice.FinishReason == "stop" || choice.FinishReason == "length" || choice.FinishReason == "")
		yield(&model.LLMResponse{
			Content:      content,
			TurnComplete: turnComplete,
		}, nil)
	}
}

// ---- conversion helpers --------------------------------------------------

// hasToolResult returns true if any message in the conversation is a tool result.
func hasToolResult(msgs []chatMessage) bool {
	for _, m := range msgs {
		if m.Role == "tool" || (m.Role == "user" && strings.HasPrefix(m.Content, "[Tool result for ")) {
			return true
		}
	}
	return false
}

// buildMessages prepends system instruction (if any) then converts conversation contents.
func buildMessages(req *model.LLMRequest) []chatMessage {
	var msgs []chatMessage

	// System instruction from req.Config
	if req.Config != nil && req.Config.SystemInstruction != nil {
		var text string
		for _, p := range req.Config.SystemInstruction.Parts {
			text += p.Text
		}
		if text != "" {
			msgs = append(msgs, chatMessage{Role: "system", Content: text})
		}
	}

	msgs = append(msgs, contentsToMessages(req.Contents)...)
	return msgs
}

func contentsToMessages(contents []*genai.Content) []chatMessage {
	var msgs []chatMessage
	for _, c := range contents {
		if c == nil {
			continue
		}

		// 1. FunctionResponse → tool result message
		hasFuncResp := false
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				hasFuncResp = true
				resp, _ := json.Marshal(p.FunctionResponse.Response)
				id := p.FunctionResponse.ID

				// Text-based function calling models (qwen, gemma) don't understand
				// "tool" role — send result as user message in natural format instead.
				if strings.HasPrefix(id, "text-call-") {
					msgs = append(msgs, chatMessage{
						Role:    "user",
						Content: fmt.Sprintf("[Tool result for %s]: %s", p.FunctionResponse.Name, string(resp)),
					})
				} else {
					msgs = append(msgs, chatMessage{
						Role:       "tool",
						Content:    string(resp),
						ToolCallID: id,
					})
				}
			}
		}
		if hasFuncResp {
			continue
		}

		role := adkRoleToOpenAI(string(c.Role))

		// 2. FunctionCall → "assistant" message with tool_calls array
		var toolCalls []toolCall
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				args, _ := json.Marshal(p.FunctionCall.Args)
				toolCalls = append(toolCalls, toolCall{
					ID:   p.FunctionCall.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: p.FunctionCall.Name, Arguments: string(args)},
				})
			}
		}
		if len(toolCalls) > 0 {
			msgs = append(msgs, chatMessage{Role: role, ToolCalls: toolCalls})
			continue
		}

		// 3. Regular text message — skip if empty
		var sb strings.Builder
		for _, p := range c.Parts {
			sb.WriteString(p.Text)
		}
		if text := sb.String(); text != "" {
			msgs = append(msgs, chatMessage{Role: role, Content: text})
		}
	}
	return msgs
}

func messageToContent(msg chatMessage) *genai.Content {
	// Case 1: proper OpenAI tool_calls (GPT-4, Claude via LiteLLM)
	if len(msg.ToolCalls) > 0 {
		var parts []*genai.Part
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck
			parts = append(parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   tc.ID,
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}
		return &genai.Content{Role: "model", Parts: parts}
	}

	// Case 2: text-based function call (qwen, gemma — returns JSON in content)
	// Supported formats:
	//   {"function_name": "...", "arguments": {...}}
	//   {"name": "...", "arguments": {...}}
	//   ```json\n{...}\n```
	if fc := parseTextFunctionCall(msg.Content); fc != nil {
		return &genai.Content{Role: "model", Parts: []*genai.Part{{FunctionCall: fc}}}
	}

	// Case 3: regular text response
	return &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: msg.Content}},
	}
}

// parseTextFunctionCall tries to extract a function call from the model's text content.
// Returns nil if the content is not a function call.
func parseTextFunctionCall(content string) *genai.FunctionCall {
	// Strip markdown code fences
	text := strings.TrimSpace(content)
	if strings.HasPrefix(text, "```") {
		// Remove opening fence (```json or ```)
		if i := strings.Index(text, "\n"); i >= 0 {
			text = text[i+1:]
		}
		// Remove closing fence
		if i := strings.LastIndex(text, "```"); i >= 0 {
			text = text[:i]
		}
		text = strings.TrimSpace(text)
	}

	if !strings.HasPrefix(text, "{") {
		return nil
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return nil
	}

	// Detect function call by checking for name + arguments keys
	name, _ := m["function_name"].(string)
	if name == "" {
		name, _ = m["name"].(string)
	}
	if name == "" {
		return nil
	}

	args, _ := m["arguments"].(map[string]any)
	if args == nil {
		args, _ = m["args"].(map[string]any)
	}

	// Generate a synthetic ID (no real tool_call_id from the model)
	return &genai.FunctionCall{
		ID:   "text-call-" + name,
		Name: name,
		Args: args,
	}
}

// buildResponseFormat returns a response_format object when the request has
// a JSON output schema (set by ADK when OutputSchema is configured on the agent).
func buildResponseFormat(req *model.LLMRequest) *responseFormat {
	if req.Config == nil || req.Config.ResponseSchema == nil {
		return nil
	}
	return &responseFormat{
		Type: "json_schema",
		JSONSchema: &jsonSchema{
			Name:   "output",
			Schema: schemaToMap(req.Config.ResponseSchema),
			Strict: true,
		},
	}
}

func extractTools(req *model.LLMRequest) []chatTool {
	if req.Config == nil {
		return nil
	}
	var result []chatTool
	for _, gt := range req.Config.Tools {
		for _, fd := range gt.FunctionDeclarations {
			// ADK stores schema in ParametersJsonSchema (not Parameters)
			result = append(result, chatTool{
				Type: "function",
				Function: toolFunction{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  parametersSchema(fd),
				},
			})
		}
	}
	return result
}

// parametersSchema reads the JSON schema from a FunctionDeclaration.
// ADK stores input schema in ParametersJsonSchema (not Parameters).
func parametersSchema(fd *genai.FunctionDeclaration) any {
	if fd.ParametersJsonSchema != nil {
		return fd.ParametersJsonSchema
	}
	if fd.Parameters != nil {
		return schemaToMap(fd.Parameters)
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// schemaToMap converts genai.Schema to a map[string]any for OpenAI JSON schema format.
func schemaToMap(s *genai.Schema) map[string]any {
	if s == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	m := map[string]any{}
	if s.Type != "" {
		m["type"] = strings.ToLower(string(s.Type))
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if len(s.Properties) > 0 {
		props := map[string]any{}
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		m["properties"] = props
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	if s.Items != nil {
		m["items"] = schemaToMap(s.Items)
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	return m
}

func adkRoleToOpenAI(role string) string {
	if role == "model" {
		return "assistant"
	}
	return role
}
