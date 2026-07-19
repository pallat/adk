package main

import (
	"context"
	"iter"
	"log"
	"os"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagents/parallelagent"
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

func fetcher(source string) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			yield(emit(ic, "fetched from "+source), nil)
		}
	}
}

func main() {
	ctx := context.Background()

	fetchDocs, err := agent.New(agent.Config{
		Name:        "fetch_docs_agent",
		Description: "Fetches documentation search results.",
		Run:         fetcher("docs"),
	})
	if err != nil {
		log.Fatalf("failed to create fetch_docs_agent: %v", err)
	}

	fetchTickets, err := agent.New(agent.Config{
		Name:        "fetch_tickets_agent",
		Description: "Fetches related support tickets.",
		Run:         fetcher("tickets"),
	})
	if err != nil {
		log.Fatalf("failed to create fetch_tickets_agent: %v", err)
	}

	fetchLogs, err := agent.New(agent.Config{
		Name:        "fetch_logs_agent",
		Description: "Fetches related service logs.",
		Run:         fetcher("logs"),
	})
	if err != nil {
		log.Fatalf("failed to create fetch_logs_agent: %v", err)
	}

	// ParallelAgent runs every SubAgent concurrently and forwards each of
	// their events as they arrive — order across sub-agents is not guaranteed.
	// Branch() differs per sub-agent so they don't see each other's history.
	fetchAll, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{
			Name:      "fetch_all_sources",
			SubAgents: []agent.Agent{fetchDocs, fetchTickets, fetchLogs},
		},
	})
	if err != nil {
		log.Fatalf("failed to create parallel agent: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(fetchAll),
	}
	l := full.NewLauncher()
	if err := l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
