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
	Params     []ParamDef    `yaml:"params,omitempty"`
	Target     *TargetSpec   `yaml:"target,omitempty"`
	Modules    []ModuleRef   `yaml:"modules,omitempty"`
	Operations []Operation   `yaml:"operations,omitempty"`
}

type ParamDef struct {
	Name     string `yaml:"name"`
	Required bool   `yaml:"required,omitempty"`
	Default  string `yaml:"default,omitempty"`
}

type TargetSpec struct {
	URL    string `yaml:"url"`
	Branch string `yaml:"branch,omitempty"`
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
}

type NewFiles struct {
	Source string `yaml:"source"`
	Dest   string `yaml:"dest,omitempty"`
}

type Patch struct {
	Engine string `yaml:"engine"`
	Path   string `yaml:"path"`
	Target string `yaml:"target"`
}

type Shell struct {
	Command string `yaml:"command"`
	Timeout string `yaml:"timeout,omitempty"`
}

type CommitPush struct {
	Message string `yaml:"message"`
	Author  string `yaml:"author,omitempty"`
	Email   string `yaml:"email,omitempty"`
}

type PR struct {
	Provider  string   `yaml:"provider"`
	Title     string   `yaml:"title"`
	Body      string   `yaml:"body,omitempty"`
	BaseBranch string  `yaml:"baseBranch,omitempty"`
	Labels    []string `yaml:"labels,omitempty"`
	TokenEnv  string   `yaml:"tokenEnv,omitempty"`
}
