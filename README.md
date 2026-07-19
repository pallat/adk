# adk

Extensions and examples for [ADK Go](https://google.golang.org/adk) (Agent Development Kit).

## Modules

| Path | Description |
|---|---|
| [`litellm/`](litellm/) | `model.LLM` adapter that routes ADK Go agents through [LiteLLM](https://github.com/BerriAI/litellm) or any OpenAI-compatible endpoint (Ollama, vLLM, OpenAI, etc.) |
| [`examples/`](examples/) | Standalone example agents (LLM, sequential, parallel, loop, custom) built on ADK Go |

Each subdirectory is its own Go module (own `go.mod`) — see the README in each for setup and usage.

## License

MIT — see [LICENSE](LICENSE).
