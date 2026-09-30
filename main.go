package main

import (
	"embed"

	"github.com/rickliujh/loom/cmd"
)

// docs is the documentation `loom skill` serves. It is embedded here because
// go:embed only reaches files beneath the declaring package, and these live at
// the repository root.
//
//go:embed docs/guide/*.md docs/reference/*.md specs/*.md
var docs embed.FS

func main() {
	cmd.Docs = docs
	cmd.Execute()
}
