# adk-loop-agent

Minimal ADK Go `LoopAgent` example — one Custom Agent sub-agent
(`attempt_agent`) that "succeeds" on its 3rd call and escalates to stop the
loop early, no LLM.

## Run

```bash
go run . console
```

Type anything and press Enter; you'll see `attempt #1`, `attempt #2`,
`attempt #3` then the loop stops (escalated), even though `MaxIterations` is
set to 5. Ctrl-D (EOF) or Ctrl-C to exit.

## What this demonstrates

- `loopagent.New(loopagent.Config{AgentConfig: ..., MaxIterations: N})` —
  same embedded-config shape as Sequential/Parallel, plus `MaxIterations`.
- A sub-agent stops the loop early by setting `event.Actions.Escalate = true`
  on a yielded event — `MaxIterations` is only a safety net for the case
  where nothing ever escalates (`0` means "loop forever or until escalate").
- The retry counter here is a closure-captured `*int` for this single-process
  demo only; a real agent would track attempt state via session state.

Verified against `google.golang.org/adk/v2 v2.0.0`.
