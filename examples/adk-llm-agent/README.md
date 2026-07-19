# adk-llm-agent

Minimal ADK Go `LlmAgent` example — one Gemini-backed agent with a single
function tool (`search_time`).

## Run

```bash
export GEMINI_API_KEY=your-key-here
go run . console
```

Type a question (e.g. "what time is it in Asia/Bangkok?") and press Enter;
the model decides on its own whether to call `search_time`. Ctrl-D (EOF) or
Ctrl-C to exit.

`go build` succeeds without an API key (compile-time only) — you need
`GEMINI_API_KEY` (or `GOOGLE_API_KEY`) set only to actually run it.

## What this demonstrates

- `gemini.NewModel(ctx, modelName, *genai.ClientConfig)` returns a
  `model.LLM` — `llmagent.Config.Model` takes that interface, not a string.
- `functiontool.New[TArgs, TResults](cfg, handler)` wraps a plain Go function
  as a `tool.Tool`. The handler signature is
  `func(agent.Context, TArgs) (TResults, error)` — schema is inferred from
  the `TArgs`/`TResults` struct fields, not from `context.Context`-based
  functions with primitive args.
- `llmagent.New(llmagent.Config{...})` is the one that actually runs an
  LLM reasoning loop (model → tool call → observe → repeat), unlike Custom
  Agent or the workflow agents which never call a model themselves.

Verified against `google.golang.org/adk/v2 v2.0.0` (`go build` only — running
end-to-end needs a real API key).
