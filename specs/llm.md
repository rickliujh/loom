# LLM Operation — Behavioral Specification

> Version: `loom.rickliujh.github.io/v1beta1`

This document describes the expected behavior of the **`llm`** operation, which uses LLM inference to generate or modify a file in the target directory.

## Schema

```yaml
operations:
  - name: <string>
    llm:
      provider: <string>              # required
      model: <string>                 # required
      prompt: <string>                # required
      systemPrompt: <string>          # optional
      target: <string>                # required — file path relative to target dir
      mode: <string>                  # optional: "generate" (default) or "modify"
      maxTokens: <int>                # optional — max output tokens
      retries: <int>                  # optional — max retry attempts (default: 0)
      retryDelay: <duration>          # optional — initial retry delay (default: "2s"), doubles each attempt
      providerConfig:                 # optional — provider-specific settings
        tokenEnv: <string>            # env var name holding the API key
        project: <string>             # GCP project ID (vertex only)
        location: <string>            # GCP region (vertex only); default: "us-central1"
```

## Fields

All string fields are templatable — rendered against the module's resolved parameter map before inference.

| Field | Required | Description |
|-------|----------|-------------|
| `provider` | yes | LLM provider. See [Providers](#providers). |
| `model` | yes | Model identifier (provider-specific). |
| `prompt` | yes | User prompt sent to the model. In `modify` mode, existing file content is prepended automatically. |
| `systemPrompt` | no | System prompt. Omitted if empty. |
| `target` | yes | Output file path relative to the target directory. |
| `mode` | no | `"generate"` (default) creates/overwrites the file. `"modify"` reads the existing file and prepends it to the prompt before inference. |
| `maxTokens` | no | Maximum number of output tokens. Provider default used when omitted. |
| `retries` | no | Maximum retry attempts on inference failure. `0` means no retry. |
| `retryDelay` | no | Initial delay before first retry. Doubles on each subsequent attempt. Parsed as Go duration string (e.g. `"2s"`, `"500ms"`). Default: `"2s"`. |
| `providerConfig.tokenEnv` | no | Env var name holding the API key. Overrides the provider default env var. |
| `providerConfig.project` | no | GCP project ID. Required for `vertex`. |
| `providerConfig.location` | no | GCP region. Required for `vertex`. Default: `"us-central1"`. |

## Providers

| Provider | Value | Auth method | Default env var |
|----------|-------|-------------|-----------------|
| OpenAI | `openai` | API key | `OPENAI_API_KEY` |
| Anthropic | `anthropic` | API key | `ANTHROPIC_API_KEY` |
| Google Gemini | `gemini` | API key | `GEMINI_API_KEY` |
| Google Vertex AI | `vertex` | Application Default Credentials (ADC) | — |
| OpenRouter | `openrouter` | API key | `OPENROUTER_API_KEY` |
| AWS Bedrock | `bedrock` | AWS credential chain | — |

`tokenEnv` overrides the default env var for key-based providers. It is ignored by `vertex` (uses ADC) and `bedrock` (uses AWS credential chain).

## Execution Flow

1. **Template-render** all string fields against the module's resolved params.
2. **Resolve auth** — read API key from `providerConfig.tokenEnv` env var if set, otherwise fall back to provider default env var.
3. **Build prompt** — if `mode` is `"modify"`, read the existing file at `<targetDir>/<target>` and prepend it to the rendered prompt (see L3).
4. **Call model** — send system prompt (if set) and user prompt to the provider. Apply `maxTokens` if set.
5. **On failure** — if `retries > 0`, retry with exponential backoff (see L4). Cancel immediately if context is cancelled.
6. **Write output** — write the full model response text to `<targetDir>/<target>`, creating parent directories as needed.

## Behaviors

### L1: Template rendering

All string fields are rendered as Go templates against the module's resolved parameter map before any other step. This includes `provider`, `model`, `prompt`, `systemPrompt`, `target`, `mode`, `retryDelay`, `providerConfig.tokenEnv`, `providerConfig.project`, and `providerConfig.location`.

```yaml
# loom.yaml (params: serviceName=payments, env=prod)
llm:
  provider: anthropic
  model: "{{ .model }}"
  target: "deploy/{{ .serviceName }}.yaml"
  prompt: "Generate a deployment for {{ .serviceName }} in {{ .env }}."
```

### L2: Generate mode (default)

When `mode` is `"generate"` or omitted, the rendered prompt is sent to the model as-is. The full response text is written to `<targetDir>/<target>`. The file must **not** already exist — if it does, the operation fails before calling the model.

```
check target absent → prompt → model → response → write to target
```

### L3: Modify mode

When `mode` is `"modify"`, the existing file at `<targetDir>/<target>` is read before calling the model. Its content is prepended to the rendered prompt in the following format:

```
Here is the existing file content:

```
<existing file content>
```

<rendered prompt>
```

The model receives this composed prompt as the user message. The full response overwrites `<targetDir>/<target>`. Fails if the target file does not exist.

```
read target → prepend to prompt → model → response → overwrite target
```

### L4: Retry with exponential backoff

When `retries > 0`, a failed inference attempt (network error or empty response from model) is retried up to `retries` times. The delay before each retry doubles:

| Attempt | Delay before retry |
|---------|--------------------|
| 1 (initial) | `retryDelay` (default `"2s"`) |
| 2 | `retryDelay × 2` |
| 3 | `retryDelay × 4` |
| … | … |

If the context is cancelled during a retry wait, the operation fails immediately with a cancellation error. On success after retry, a log entry records the attempt number.

Both failure causes trigger retry:
- Inference call returns an error.
- Inference call returns no content (empty `Choices`).

### L5: System prompt

When `systemPrompt` is non-empty after template rendering, it is sent as a system-role message before the user prompt. When empty, no system message is sent.

### L6: Auth resolution

For key-based providers (`openai`, `anthropic`, `gemini`, `openrouter`):
1. If `providerConfig.tokenEnv` is set, read the key from that env var.
2. Otherwise, read from the provider's default env var (see [Providers](#providers) table).

`vertex` uses Application Default Credentials (ADC) — no API key is read.
`bedrock` uses the AWS SDK credential chain (env vars, shared config, IAM role) — no API key is read.

### L7: Vertex location default

For `vertex`, if `providerConfig.location` is empty after template rendering, it defaults to `"us-central1"`.

### L8: Dry run

When `--dry-run` is active, the LLM is **not** called and no file is written. A log entry at `info` level records what would have been invoked:

```
dry-run: would invoke LLM and write result  target=<path>  promptLength=<n>
```

## Error Conditions

| Condition | Error |
|-----------|-------|
| Unknown `provider` value | `creating <provider> client: unsupported provider "<value>"` |
| `retryDelay` is not a valid Go duration | `parsing retryDelay "<value>": ...` |
| `mode` is `"generate"` and target file already exists | `target already exists: <path>` |
| `mode` is `"modify"` and target file does not exist | `reading target for modify: ...` |
| Model client creation fails | `creating <provider> client: ...` |
| All inference attempts fail | `<provider> inference failed: ...` |
| All attempts return empty response | `<provider> returned no content` |
| Writing output file fails | `writing output: ...` |

## End-to-End Example

**Module params:** `serviceName=payments`, `namespace=fintech`, `image=payments:v1.2.0`

**loom.yaml:**
```yaml
operations:
  - name: generate-deployment
    llm:
      provider: anthropic
      model: claude-sonnet-4-20250514
      systemPrompt: "Output only valid YAML. No explanation."
      prompt: |
        Generate a Kubernetes Deployment manifest for a service named {{ .serviceName }}
        in namespace {{ .namespace }} using image {{ .image }}.
      target: "deploy/{{ .serviceName }}.yaml"
      mode: generate
      maxTokens: 2048
      retries: 2
      retryDelay: "3s"
      providerConfig:
        tokenEnv: ANTHROPIC_API_KEY

  - name: update-readme
    llm:
      provider: openai
      model: gpt-4o
      prompt: "Add a ## {{ .serviceName }} section describing this service."
      target: README.md
      mode: modify
      providerConfig:
        tokenEnv: OPENAI_API_KEY
```

**What happens:**

1. `generate-deployment` — all fields rendered. Fails if `deploy/payments.yaml` already exists. Prompt sent to Anthropic with system prompt. Response written to `deploy/payments.yaml`. If inference fails, retried up to 2 times with 3s → 6s backoff.
2. `update-readme` — existing `README.md` read, prepended to rendered prompt. Updated content from OpenAI overwrites `README.md`.
