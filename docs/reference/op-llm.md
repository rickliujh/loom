# llm

Uses LLM inference to generate or modify a file in the target directory.

## Usage

```yaml
- name: generate-deployment
  llm:
    provider: anthropic
    model: claude-sonnet-4-20250514
    systemPrompt: "Output only valid YAML. No explanation."
    prompt: |
      Generate a Kubernetes Deployment manifest for {{ .serviceName }}
      in namespace {{ .namespace }} using image {{ .image }}.
    target: "deploy/{{ .serviceName }}.yaml"
    mode: generate
    maxTokens: 2048
    retries: 2
    retryDelay: "3s"
    providerConfig:
      tokenEnv: ANTHROPIC_API_KEY
```

| Field | Required | Description |
|-------|----------|-------------|
| `provider` | yes | LLM provider. See [Providers](#providers). |
| `model` | yes | Model identifier (provider-specific). |
| `prompt` | yes | User prompt. Templated. In `modify` mode, existing file content is prepended automatically. |
| `systemPrompt` | no | System prompt. Omitted if empty. Templated. |
| `target` | yes | Output file path relative to the target directory. Templated. |
| `mode` | no | `"generate"` (default) or `"modify"`. See [Modes](#modes). |
| `maxTokens` | no | Maximum output tokens. Provider default used when omitted. |
| `retries` | no | Maximum retry attempts on failure. Default: `0` (no retry). |
| `retryDelay` | no | Initial delay between retries (e.g. `"2s"`, `"500ms"`). Doubles each attempt. Default: `"2s"`. |
| `providerConfig.tokenEnv` | no | Env var name holding the API key. Overrides provider default. |
| `providerConfig.project` | no | GCP project ID. Required for `vertex`. Templated. |
| `providerConfig.location` | no | GCP region. Required for `vertex`. Default: `"us-central1"`. Templated. |

All string fields are templatable.

## Providers

| Provider | Value | Auth | Default env var |
|----------|-------|------|-----------------|
| OpenAI | `openai` | API key | `OPENAI_API_KEY` |
| Anthropic | `anthropic` | API key | `ANTHROPIC_API_KEY` |
| Google Gemini | `gemini` | API key | `GEMINI_API_KEY` |
| Google Vertex AI | `vertex` | Application Default Credentials | — |
| OpenRouter | `openrouter` | API key | `OPENROUTER_API_KEY` |
| AWS Bedrock | `bedrock` | AWS credential chain | — |

Set `providerConfig.tokenEnv` to override the default env var for key-based providers. `vertex` and `bedrock` do not use API keys.

## Modes

### generate (default)

Sends the rendered prompt to the model and writes the response to `target`. **Fails if the file already exists.** Use this to create new files.

### modify

Reads the existing file at `target`, prepends it to the prompt, then calls the model and overwrites `target` with the response. Fails if the file does not exist.

The prompt sent to the model is composed as:

````text
Here is the existing file content:

```
<existing content>
```

<your prompt>
````

## Retry

When `retries > 0`, failed attempts are retried with exponential backoff. The delay doubles on each attempt:

| Attempt | Delay |
|---------|-------|
| 1st retry | `retryDelay` |
| 2nd retry | `retryDelay × 2` |
| 3rd retry | `retryDelay × 4` |

Retries trigger on inference errors and on empty responses. If the context is cancelled during a wait, the operation fails immediately.

## Dry Run

In dry-run mode, the model is not called and no file is written. A log entry records the provider, model, target, and prompt length.

## Examples

### Generate a new file

```yaml
- name: create-service-config
  llm:
    provider: openai
    model: gpt-4o
    prompt: "Generate a Kubernetes Service for {{ .serviceName }} on port 8080."
    target: "k8s/{{ .serviceName }}-svc.yaml"
    providerConfig:
      tokenEnv: OPENAI_API_KEY
```

### Modify an existing file

```yaml
- name: update-readme
  llm:
    provider: anthropic
    model: claude-sonnet-4-20250514
    prompt: "Add a ## {{ .serviceName }} section describing this service."
    target: README.md
    mode: modify
    providerConfig:
      tokenEnv: ANTHROPIC_API_KEY
```

### Vertex AI (GCP)

```yaml
- name: generate-policy
  llm:
    provider: vertex
    model: gemini-2.5-flash
    prompt: "Generate a network policy for {{ .serviceName }}."
    target: "policies/{{ .serviceName }}.yaml"
    providerConfig:
      project: "{{ .gcpProject }}"
      location: us-central1
```
