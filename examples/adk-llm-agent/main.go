package main

import (
	"context"
	"log"
	"os"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

type SearchTimeArgs struct {
	Timezone string `json:"timezone" jsonschema:"the IANA timezone name, e.g. Asia/Bangkok"`
}

type SearchTimeResult struct {
	Time string `json:"time"`
}

// searchTime is a plain Go function exposed to the LLM as a callable tool.
// functiontool.New infers the JSON schema from SearchTimeArgs/SearchTimeResult.
func searchTime(ctx agent.Context, args SearchTimeArgs) (SearchTimeResult, error) {
	return SearchTimeResult{Time: "10:30 " + args.Timezone}, nil // ปกติเรียก time API จริงตรงนี้
}

func main() {
	ctx := context.Background()

	// APIKey left empty: gemini.NewModel reads GEMINI_API_KEY / GOOGLE_API_KEY
	// from the environment if ClientConfig.APIKey is unset.
	geminiModel, err := gemini.NewModel(ctx, "gemini-2.5-flash", &genai.ClientConfig{})
	if err != nil {
		log.Fatalf("failed to create gemini model: %v", err)
	}

	searchTimeTool, err := functiontool.New(functiontool.Config{
		Name:        "search_time",
		Description: "Returns the current time for a given IANA timezone.",
	}, searchTime)
	if err != nil {
		log.Fatalf("failed to create search_time tool: %v", err)
	}

	assistant, err := llmagent.New(llmagent.Config{
		Name:        "assistant_agent",
		Model:       geminiModel,
		Instruction: "You are a helpful assistant. Answer concisely in Thai.",
		Tools:       []tool.Tool{searchTimeTool},
	})
	if err != nil {
		log.Fatalf("failed to create llm agent: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(assistant),
	}
	l := full.NewLauncher()
	if err := l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
