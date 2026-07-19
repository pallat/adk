package main

import (
	"context"
	"fmt"
	"iter"
	"log"
	"os"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagents/loopagent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
)

// emit builds a single text event authored by the current agent in ic.
func emit(ic agent.InvocationContext, text string) *session.Event {
	event := session.NewEvent(ic, ic.InvocationID())
	event.Author = ic.Agent().Name()
	event.Branch = ic.Branch()
	event.LLMResponse = model.LLMResponse{
		Content: genai.NewContentFromText(text, genai.RoleModel),
	}
	return event
}

// runAttempt simulates "retry until it succeeds": it succeeds on its 3rd
// call and escalates to stop the loop early. counter is captured by closure
// purely for this single-process demo — a real agent would track attempts
// via session state instead.
func runAttempt(counter *int) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			*counter++
			event := emit(ic, fmt.Sprintf("attempt #%d", *counter))
			if *counter >= 3 {
				event.Actions.Escalate = true // สำเร็จแล้ว หยุด loop ทันที ไม่ต้องรอ MaxIterations
			}
			yield(event, nil)
		}
	}
}

func main() {
	ctx := context.Background()

	counter := 0
	attemptAgent, err := agent.New(agent.Config{
		Name:        "attempt_agent",
		Description: "Retries a task until it succeeds.",
		Run:         runAttempt(&counter),
	})
	if err != nil {
		log.Fatalf("failed to create attempt agent: %v", err)
	}

	// LoopAgent re-runs SubAgents every iteration until either (1) a
	// sub-agent's event sets Actions.Escalate, or (2) MaxIterations is hit —
	// MaxIterations here is a safety net in case the agent never escalates.
	retryLoop, err := loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "retry_loop",
			SubAgents: []agent.Agent{attemptAgent},
		},
		MaxIterations: 5,
	})
	if err != nil {
		log.Fatalf("failed to create loop agent: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(retryLoop),
	}
	l := full.NewLauncher()
	if err := l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
