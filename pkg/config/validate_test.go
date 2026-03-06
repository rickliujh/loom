package config

import (
	"strings"
	"testing"
)

func validLoomFile() *LoomFile {
	return &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "op1", Shell: &Shell{Command: "echo hi"}},
			},
		},
	}
}

func TestValidate_CommandAndRequiredMutuallyExclusive(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{
		{Name: "foo", Dynamic: "echo val", Required: true},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for dynamic + required")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_CommandParamIsValid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{
		{Name: "foo", Dynamic: "echo val"},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
