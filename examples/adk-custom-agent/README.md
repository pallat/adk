# adk-custom-agent

Minimal ADK Go Custom Agent example — no LLM, no tools, just a plain Go
function wired as `agent.Config.Run`.

## Run

```bash
go run . console
```

Type anything and press Enter; the agent replies with a static greeting.
Ctrl-D (EOF) or Ctrl-C to exit.

## What this demonstrates

- Custom agents are built via `agent.New(agent.Config{Run: ...})`, not by
  implementing the `agent.Agent` interface directly (it has unexported
  methods by design).
- `Run` has the signature
  `func(agent.InvocationContext) iter.Seq2[*session.Event, error]`.
- Emitting an event requires `session.NewEventWithContext` +
  `model.LLMResponse{Content: *genai.Content}`.

Verified against `google.golang.org/adk v1.5.0`.
