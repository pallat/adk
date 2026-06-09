# litellm — ADK Go adapter for OpenAI-compatible endpoints

`github.com/pallat/adk/litellm` implements `model.LLM` for [ADK Go](https://google.golang.org/adk),
letting you route agent requests through [LiteLLM](https://github.com/BerriAI/litellm) or any
OpenAI-compatible endpoint (Ollama, vLLM, OpenAI, etc.).

## Installation

```bash
go get github.com/pallat/adk/litellm
```

## Quick start

```go
import (
    "os"

    "github.com/pallat/adk/litellm"
    "google.golang.org/adk/agents/llmagent"
)

model := litellm.New(
    os.Getenv("LITELLM_MODEL"),    // "gpt-4o", "qwen3.5", "ollama/llama3.2"
    os.Getenv("LITELLM_BASE_URL"), // "http://localhost:4000"  (no /v1 suffix)
    os.Getenv("LITELLM_API_KEY"),  // "" for local endpoints
)

agent, _ := llmagent.New(llmagent.Config{
    Model: model,
    // ... rest of agent config
})
```

> `LITELLM_BASE_URL` must not include a `/v1` suffix — the adapter appends `/chat/completions` itself.

## Configuration

| Env var | Example | Notes |
|---|---|---|
| `LITELLM_MODEL` | `gpt-4o` | Passed as the `model` field in each request |
| `LITELLM_BASE_URL` | `http://localhost:4000` | LiteLLM proxy or any OpenAI-compatible base URL |
| `LITELLM_API_KEY` | `sk-...` | Optional; omit for local endpoints |

### Generation parameters

Pass `GenerateContentConfig` to the agent to control sampling:

```go
import "google.golang.org/genai"

agent, _ := llmagent.New(llmagent.Config{
    Model: model,
    GenerateContentConfig: &genai.GenerateContentConfig{
        Temperature: genai.Ptr[float32](0.2),
        TopP:        genai.Ptr[float32](0.9),
        TopK:        genai.Ptr[float32](40),
    },
})
```

> `top_k` is not a standard OpenAI field but LiteLLM and Ollama both accept it.

## Feature support

| Feature | Status |
|---|---|
| Text generation | ✅ |
| Tool / function calling (proper `tool_calls`) | ✅ |
| Text-based function calling (qwen, gemma) | ✅ |
| Structured output (`OutputSchema`) | ✅ |
| System instruction | ✅ |
| Streaming | ❌ (ignored; always returns a single response) |
| Token usage metadata | ❌ (not mapped back) |

### Model compatibility

| Model | Function calling style | Notes |
|---|---|---|
| GPT-4o, Claude | Proper `tool_calls` | Works well |
| qwen3.5 | Proper `tool_calls` | Adapter cuts tools after first result to prevent infinite loops |
| qwen / gemma (small) | JSON embedded in `content` | Parsed via text-based fallback |

## How it works

ADK Go requires every model to implement:

```go
type LLM interface {
    Name() string
    GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error]
}
```

The adapter:

1. Converts `req.Contents` (ADK's `genai.Content` slice) into OpenAI `messages`
2. Prepends `req.Config.SystemInstruction` as a `system` message
3. Extracts tool declarations from `req.Config.Tools`, reading `ParametersJsonSchema` (ADK v1.4+)
4. POSTs to `{baseURL}/chat/completions`
5. Converts the response back to `*genai.Content`, detecting `FunctionCall` parts to set `TurnComplete` correctly

### TurnComplete logic

`TurnComplete: false` tells ADK to execute the returned tool calls and loop back.
The adapter sets it based on whether the response contains `FunctionCall` parts — not `finish_reason` —
because text-based models return `finish_reason: "stop"` even when calling a tool.

### Text-based function calling

Some models (qwen, gemma) embed JSON in the `content` field instead of using the `tool_calls` array.
The adapter parses these automatically, supporting:

```
{"function_name": "...", "arguments": {...}}
{"name": "...", "arguments": {...}}
```json
{"name": "...", "arguments": {...}}
```
```

Tool results for these models are sent back as `user` messages (`[Tool result for …]: …`)
instead of `tool` role messages, since the models are not fine-tuned on that protocol.

## Why LiteLLM?

ADK Go v1.4 ships with two built-in adapters: `model/gemini` and `model/apigee`, both
tied to Google Cloud. LiteLLM acts as a universal proxy that exposes an OpenAI-compatible
API in front of 100+ model providers, making it the natural bridge.

## License

MIT
