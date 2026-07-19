package main

import (
	"context"
	"iter"
	"log"
	"os"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
)

// runGreeting is a pure-Go agent implementation: no LLM call, no tools.
// This is the minimal shape of a Custom Agent in ADK Go — a plain
// func(agent.InvocationContext) iter.Seq2[*session.Event, error] passed
// as agent.Config.Run. It yields 3 progress events in a loop instead of
// just one, to show that yield count is entirely up to this function.
func runGreeting(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
	steps := []string{
		"เริ่มทำงาน...",
		"กำลังประมวลผล...",
		"เสร็จแล้ว: สวัสดี จาก custom agent ล้วนๆ ไม่มี LLM",
	}

	return func(yield func(*session.Event, error) bool) {
		for _, text := range steps {
			event := session.NewEvent(ic, ic.InvocationID())
			event.Author = ic.Agent().Name()
			event.Branch = ic.Branch()
			event.LLMResponse = model.LLMResponse{
				Content: genai.NewContentFromText(text, genai.RoleModel),
			}
			if !yield(event, nil) {
				return // caller (Runner) ขอหยุดกลางทาง
			}
		}
	}
}

func main() {
	ctx := context.Background()

	greetingAgent, err := agent.New(agent.Config{
		Name:        "greeting_agent",
		Description: "Greets the user without calling any LLM.",
		Run:         runGreeting,
	})
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(greetingAgent),
	}
	l := full.NewLauncher()
	if err := l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
