# LLM-Powered Operations

Loom can use LLM inference to generate or modify files as part of a module's operation sequence. This lets you produce content that is too dynamic or context-dependent for static templates — config summaries, documentation, policy files, or anything that benefits from natural language reasoning.

## When to Use It

Use `llm` operations when:

- The output structure is too variable for a fixed template
- You want to generate prose (README sections, runbook entries, PR descriptions)
- You need to modify an existing file in a way that requires understanding its content

Use `newFiles` + templates instead when the output is predictable and structured — templates are faster, deterministic, and cheaper.

## Basic Example

```yaml
operations:
  - name: generate-deployment
    llm:
      provider: anthropic
      model: claude-sonnet-4-20250514
      systemPrompt: "Output only valid YAML. No explanation."
      prompt: |
        Generate a Kubernetes Deployment for {{ .serviceName }}
        in namespace {{ .namespace }} using image {{ .image }}.
      target: "deploy/{{ .serviceName }}.yaml"
      providerConfig:
        tokenEnv: ANTHROPIC_API_KEY
```

When `loom run` reaches this operation, it renders the prompt with the module's params, calls the model, and writes the response to `deploy/payments.yaml` in the target repository.

## Two Modes

### generate

Creates a new file. **Fails if the target already exists** — this is intentional to prevent silent overwrites.

```yaml
llm:
  mode: generate   # default — can be omitted
  target: "docs/{{ .serviceName }}-runbook.md"
  prompt: "Write a runbook for {{ .serviceName }}."
```

### modify

Reads the existing file at `target`, sends its content to the model alongside your prompt, and overwrites the file with the response. Use this to append, reformat, or update existing content.

```yaml
llm:
  mode: modify
  target: README.md
  prompt: "Add a ## {{ .serviceName }} section at the end."
```

## Providers

All major LLM providers are supported:

| Provider | `provider` value | Auth |
|----------|-----------------|------|
| Anthropic | `anthropic` | `ANTHROPIC_API_KEY` |
| OpenAI | `openai` | `OPENAI_API_KEY` |
| Google Gemini | `gemini` | `GEMINI_API_KEY` |
| Google Vertex AI | `vertex` | Application Default Credentials |
| OpenRouter | `openrouter` | `OPENROUTER_API_KEY` |
| AWS Bedrock | `bedrock` | AWS credential chain |

API keys are always read from environment variables — never put secrets in `loom.yaml`.

```yaml
providerConfig:
  tokenEnv: MY_ANTHROPIC_KEY   # reads $MY_ANTHROPIC_KEY instead of $ANTHROPIC_API_KEY
```

## Combining with Other Operations

`llm` is just another operation in the sequence. You can mix it freely with `newFiles`, `patch`, `shell`, and git operations:

```yaml
operations:
  - name: scaffold-files
    newFiles:
      source: "."
      dest: ""

  - name: generate-readme
    llm:
      provider: anthropic
      model: claude-sonnet-4-20250514
      prompt: "Write a README for {{ .serviceName }} based on the Kubernetes manifests in this repo."
      target: README.md

  - name: commit
    commitPush:
      message: "feat: onboard {{ .serviceName }}"
```

## Retry on Failure

LLM APIs can be flaky. Use `retries` and `retryDelay` to automatically retry on failure with exponential backoff:

```yaml
llm:
  provider: openai
  model: gpt-4o
  prompt: "..."
  target: "out.yaml"
  retries: 3
  retryDelay: "2s"   # delays: 2s → 4s → 8s
```

## Full Reference

See [`llm` operation reference](/reference/op-llm) for all fields and provider-specific configuration.
