# adk-parallel-agent

Minimal ADK Go `ParallelAgent` example — three Custom Agent sub-agents
(`fetch_docs_agent`, `fetch_tickets_agent`, `fetch_logs_agent`) wired to run
concurrently, no LLM.

## Run

```bash
go run . console
```

Type anything and press Enter; all three sub-agents' events arrive on the
same invocation, but their relative order is not guaranteed (concurrent).
Ctrl-D (EOF) or Ctrl-C to exit.

## What this demonstrates

- `parallelagent.New(parallelagent.Config{AgentConfig: agent.Config{...}})`
  — same shape as `SequentialAgent`, `SubAgents` in the embedded config.
- Sub-agents run concurrently; each gets a distinct `Branch()` so they don't
  see each other's conversation history mid-run.
- Best used for independent work (fan-out fetches, multi-source lookups) to
  cut total latency versus running them one at a time.

Verified against `google.golang.org/adk/v2 v2.0.0`.
