# adk-sequential-agent

Minimal ADK Go `SequentialAgent` example — two Custom Agent sub-agents
(`draft_agent`, `review_agent`) wired to run one after another, no LLM.

## Run

```bash
go run . console
```

Type anything and press Enter; both sub-agents reply in list order:
draft first, then review. Ctrl-D (EOF) or Ctrl-C to exit.

## What this demonstrates

- `sequentialagent.New(sequentialagent.Config{AgentConfig: agent.Config{...}})`
  — `SubAgents` go in the embedded `agent.Config`, not a separate field.
- `SequentialAgent` sets its own `Run` internally; passing a custom `Run` in
  `AgentConfig` is rejected with an error.
- Sub-agents can be any `agent.Agent` — here both are Custom Agents (plain Go
  functions), but LLM Agents compose in exactly the same way.
- Order is strict: `SequentialAgent.Run` iterates `SubAgents()` and forwards
  each sub-agent's events before moving to the next.

Verified against `google.golang.org/adk/v2 v2.0.0`.
