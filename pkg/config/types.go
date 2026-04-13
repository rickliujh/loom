package config

// LoomFile represents the top-level loom.yaml structure.
type LoomFile struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type Spec struct {
	Params        []ParamDef        `yaml:"params,omitempty"`
	DynamicParams []DynamicParamDef `yaml:"dynamicParams,omitempty"`
	Excludes      []string          `yaml:"excludes,omitempty"`
	Includes      []string          `yaml:"includes,omitempty"`
	Target        *TargetSpec       `yaml:"target,omitempty"`
	Modules       []ModuleRef       `yaml:"modules,omitempty"`
	Operations    []Operation       `yaml:"operations,omitempty"`
}

type ParamDef struct {
	Name     string `yaml:"name"`
	Required bool   `yaml:"required,omitempty"`
	Default  string `yaml:"default,omitempty"`
}

// DynamicParamDef defines a parameter whose value comes from a shell command.
// The command is templated with all resolved params before execution.
// Dynamic params are always evaluated after all regular params are resolved.
type DynamicParamDef struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
	Default string `yaml:"default,omitempty"` // Fallback if not needed; command takes priority.
}

type TargetSpec struct {
	URL           string `yaml:"url"`
	Branch        string `yaml:"branch,omitempty"`
	FeatureBranch string `yaml:"featureBranch,omitempty"`
}

type ModuleRef struct {
	Name   string            `yaml:"name"`
	Source string            `yaml:"source"`
	Params map[string]string `yaml:"params,omitempty"`
}

type Operation struct {
	Name       string      `yaml:"name"`
	NewFiles   *NewFiles   `yaml:"newFiles,omitempty"`
	Patch      *Patch      `yaml:"patch,omitempty"`
	Shell      *Shell      `yaml:"shell,omitempty"`
	CommitPush *CommitPush `yaml:"commitPush,omitempty"`
	PR         *PR         `yaml:"pr,omitempty"`
	LLM        *LLM        `yaml:"llm,omitempty"`
}

type NewFiles struct {
	Source string `yaml:"source"`
	Dest   string `yaml:"dest,omitempty"`
}

type Patch struct {
	Engine string `yaml:"engine,omitempty"` // "smp" (default, strategic merge patch) or "json6902"
	Path   string `yaml:"path"`
	Target string `yaml:"target"`
}

type Shell struct {
	Command string `yaml:"command"`
	Timeout string `yaml:"timeout,omitempty"`
	// Pure marks this command as having no external side effects,
	// making it safe to run in --local-run mode.
	// Shell commands are skipped by default when --local-run is set.
	Pure bool `yaml:"pure,omitempty"`
}

type CommitPush struct {
	Message string `yaml:"message"`
	Author  string `yaml:"author,omitempty"`
	Email   string `yaml:"email,omitempty"`
}

type PR struct {
	Provider   string   `yaml:"provider"`
	Title      string   `yaml:"title"`
	Body       string   `yaml:"body,omitempty"`
	BaseBranch string   `yaml:"baseBranch,omitempty"`
	Labels     []string `yaml:"labels,omitempty"`
	TokenEnv   string   `yaml:"tokenEnv,omitempty"`
}

// LLM defines an operation that uses LLM inference to generate or modify a file.
type LLM struct {
	Provider       string             `yaml:"provider"`                 // "openai", "anthropic", "vertex", "gemini", "openrouter", "bedrock"
	Model          string             `yaml:"model"`                    // e.g. "gpt-4o", "claude-sonnet-4-20250514", "gemini-2.5-flash"
	Prompt         string             `yaml:"prompt"`                   // Templated prompt text
	SystemPrompt   string             `yaml:"systemPrompt,omitempty"`   // Optional system prompt
	Target         string             `yaml:"target"`                   // File path relative to target dir to write/modify
	Mode           string             `yaml:"mode,omitempty"`           // "generate" (default) or "modify"
	MaxTokens      int                `yaml:"maxTokens,omitempty"`      // Max output tokens (optional)
	Retries        int                `yaml:"retries,omitempty"`        // Max retry attempts on failure (default: 0, no retry)
	RetryDelay     string             `yaml:"retryDelay,omitempty"`     // Initial delay between retries (default: "2s"), doubles each attempt
	ProviderConfig *LLMProviderConfig `yaml:"providerConfig,omitempty"` // Provider-specific configuration
}

// LLMProviderConfig holds provider-specific settings for the LLM operation.
// All auth credentials are referenced by env var name — secrets must never appear in loom.yaml.
type LLMProviderConfig struct {
	// Auth — env var name holding the API key (openai, anthropic, gemini, openrouter).
	// Not used by vertex (ADC) or bedrock (AWS credential chain).
	TokenEnv string `yaml:"tokenEnv,omitempty"`

	// Google Cloud (vertex only)
	Project  string `yaml:"project,omitempty"`  // GCP project ID (required for vertex)
	Location string `yaml:"location,omitempty"` // GCP region (default: "us-central1")
}
