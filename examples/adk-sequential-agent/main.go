package main

import (
	"context"
	"iter"
	"log"
	"os"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagents/sequentialagent"
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

func runDraft(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		yield(emit(ic, "draft: เขียน proposal เวอร์ชันแรกแล้ว"), nil)
	}
}

func runReview(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		yield(emit(ic, "review: ตรวจ proposal แล้ว ผ่าน"), nil)
	}
}

func main() {
	ctx := context.Background()

	draftAgent, err := agent.New(agent.Config{
		Name:        "draft_agent",
		Description: "Writes a first-draft proposal.",
		Run:         runDraft,
	})
	if err != nil {
		log.Fatalf("failed to create draft agent: %v", err)
	}

	reviewAgent, err := agent.New(agent.Config{
		Name:        "review_agent",
		Description: "Reviews the draft proposal.",
		Run:         runReview,
	})
	if err != nil {
		log.Fatalf("failed to create review agent: %v", err)
	}

	// SequentialAgent has no Run of its own to configure — it runs
	// AgentConfig.SubAgents in list order and forwards each of their events.
	pipeline, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "proposal_pipeline",
			SubAgents: []agent.Agent{draftAgent, reviewAgent},
		},
	})
	if err != nil {
		log.Fatalf("failed to create sequential agent: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(pipeline),
	}
	l := full.NewLauncher()
	if err := l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
