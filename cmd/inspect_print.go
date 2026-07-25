package cmd

import (
	"fmt"
	"io"
	"strings"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/module"
)

// inspectPrinter renders an inspected module tree as indented text.
//
// The layout is one tree, not a report per module: a module's parameters,
// target, operations, and submodules all hang off it as branches, so nesting is
// read from the indentation alone. It borrows the run log's vocabulary — the
// "≡ … ≡" root chip and the "▸" hand-off marker — so the same module is
// recognizable whether you are inspecting it or watching it run.
type inspectPrinter struct {
	w     io.Writer
	style prettylog.Style
}

// branch is one child line of a module: a label, and optionally the body lines
// printed beneath it at the deeper indentation.
type branch struct {
	label string
	body  func(prefix string)
}

func (p *inspectPrinter) root(n *module.Inspection) {
	fmt.Fprintln(p.w)
	line := p.style.RootChip(n.Instance)
	if n.Dir != "" {
		line += " " + p.style.Muted(n.Dir)
	}
	fmt.Fprintln(p.w, line)
	p.branches(n, "")
}

// branches prints every branch of one module beneath prefix, connecting each
// with the usual box-drawing elbows so the last one closes its subtree.
func (p *inspectPrinter) branches(n *module.Inspection, prefix string) {
	bs := p.buildBranches(n)
	for i, b := range bs {
		connector, childPrefix := "├─ ", prefix+"│  "
		if i == len(bs)-1 {
			connector, childPrefix = "└─ ", prefix+"   "
		}
		fmt.Fprintf(p.w, "%s%s%s\n", prefix, p.style.Muted(connector), b.label)
		if b.body != nil {
			b.body(childPrefix)
		}
	}
}

func (p *inspectPrinter) buildBranches(n *module.Inspection) []branch {
	var bs []branch

	// Why this module could not be described comes first: everything below it
	// is missing as a consequence, and the reader should know that up front.
	if n.Error != "" {
		bs = append(bs, branch{label: p.style.Error("✖ " + n.Error)})
	}
	if n.Cycle {
		bs = append(bs, branch{label: p.style.Warn("↺ already inspected further up this branch — not expanded")})
	}
	if n.Unfetched {
		bs = append(bs, branch{label: p.style.Muted("⤓ remote module not fetched (--no-fetch)")})
	}
	for _, w := range n.Warnings {
		bs = append(bs, branch{label: p.style.Warn("⚠ " + w)})
	}

	if len(n.Params) > 0 {
		params := n.Params
		bs = append(bs, branch{
			label: p.style.Bold("params"),
			body:  func(prefix string) { p.printParams(params, prefix) },
		})
	}
	if n.Target != nil {
		bs = append(bs, branch{label: p.style.Bold("target") + "  " + p.targetLine(n.Target)})
	}
	if len(n.Excludes) > 0 {
		bs = append(bs, branch{label: p.style.Bold("excludes") + "  " + p.style.Muted(strings.Join(n.Excludes, ", "))})
	}
	if len(n.Includes) > 0 {
		bs = append(bs, branch{label: p.style.Bold("includes") + "  " + p.style.Muted(strings.Join(n.Includes, ", "))})
	}
	if len(n.Operations) > 0 {
		ops := n.Operations
		bs = append(bs, branch{
			label: p.style.Bold("operations") + p.style.Muted(fmt.Sprintf(" (%d, in order)", len(ops))),
			body:  func(prefix string) { p.printOperations(ops, prefix) },
		})
	}

	// Submodules hang off the parent directly rather than under a "modules"
	// heading: a run dispatches them as steps of the parent, and one less level
	// of indentation per generation keeps a deep tree readable.
	for _, child := range n.Children {
		child := child
		bs = append(bs, branch{
			label: p.childLabel(child),
			body:  func(prefix string) { p.branches(child, prefix) },
		})
	}
	if n.Truncated {
		bs = append(bs, branch{label: p.style.Muted("… submodules not expanded (--depth limit)")})
	}
	return bs
}

// childLabel names one submodule: the instance name a run logs it under, the
// source it came from, the metadata name when it differs from the instance, and
// the condition gating it.
func (p *inspectPrinter) childLabel(n *module.Inspection) string {
	var b strings.Builder
	b.WriteString(p.style.Module("▸ "))
	b.WriteString(p.style.Bold(n.Instance))
	if n.Name != "" && n.Name != n.Instance {
		b.WriteString(p.style.Muted(" (" + n.Name + ")"))
	}
	if n.Source != "" {
		b.WriteString("  " + p.style.Muted(n.Source))
		if n.SourceTemplate != "" {
			b.WriteString(p.style.Muted(" ← " + n.SourceTemplate))
		}
	}
	if n.If != "" {
		b.WriteString("  " + p.style.Warn("if: "+n.If))
	}
	return b.String()
}

func (p *inspectPrinter) targetLine(t *module.Target) string {
	line := t.URL
	if t.Branch != "" {
		line += " (" + t.Branch + ")"
	}
	if t.FeatureBranch != "" {
		line += " → " + t.FeatureBranch
	}
	return line
}

func (p *inspectPrinter) printParams(params []module.Param, prefix string) {
	nameW, stateW := 0, 0
	for _, prm := range params {
		nameW = max(nameW, len(prm.Name))
		stateW = max(stateW, len(paramStateLabel(prm)))
	}
	for _, prm := range params {
		label := paramStateLabel(prm)
		// Pad before coloring: escape codes have width on the wire but not on
		// screen, so padding a colored string would misalign the column.
		state := pad(label, stateW)
		switch prm.State {
		case module.ParamMissing:
			state = p.style.Error(state)
		case module.ParamProvided, module.ParamDefault:
			state = p.style.Success(state)
		default:
			state = p.style.Muted(state)
		}
		fmt.Fprintf(p.w, "%s  %s  %s  %s\n", prefix, pad(prm.Name, nameW), state, p.paramDetail(prm))
	}
}

// paramStateLabel names where a parameter's value comes from. A missing one is
// labelled "required" rather than "missing" — that is the fact the reader acts
// on, and the detail column says it is unsatisfied.
func paramStateLabel(prm module.Param) string {
	switch prm.State {
	case module.ParamMissing:
		return "required"
	case module.ParamUnset:
		return "optional"
	default:
		return string(prm.State)
	}
}

func (p *inspectPrinter) paramDetail(prm module.Param) string {
	var b strings.Builder
	switch prm.State {
	case module.ParamMissing:
		b.WriteString(p.style.Error("must be supplied"))
	case module.ParamProvided, module.ParamDefault:
		b.WriteString(fmt.Sprintf("= %q", prm.Value))
	case module.ParamDynamic:
		b.WriteString(p.style.Muted("$ " + prm.Command))
		if prm.Default != "" {
			b.WriteString(p.style.Muted(fmt.Sprintf(" (falls back to %q)", prm.Default)))
		}
	case module.ParamUnresolved:
		b.WriteString(p.style.Muted("resolved at run time"))
	case module.ParamUnset:
		b.WriteString(p.style.Muted(`= ""`))
	}
	// Where a parent-supplied value came from, so a templated hand-off stays
	// traceable back to the expression that produced it.
	if prm.From != "" {
		b.WriteString(p.style.Muted(" ← " + prm.From))
	}
	return b.String()
}

func (p *inspectPrinter) printOperations(ops []module.OpSummary, prefix string) {
	nameW, kindW := 0, 0
	for _, op := range ops {
		nameW = max(nameW, len(op.Name))
		kindW = max(kindW, len(op.Kind))
	}
	for _, op := range ops {
		detail := op.Detail
		if op.Error != "" {
			detail = p.style.Error("✖ " + op.Error)
		}
		line := fmt.Sprintf("%s  %s  %s  %s", prefix, pad(op.Name, nameW), p.style.Module(pad(op.Kind, kindW)), detail)
		if op.If != "" {
			line += "  " + p.style.Warn("if: "+op.If)
		}
		fmt.Fprintln(p.w, strings.TrimRight(line, " "))
	}
}

// summary closes the report with what a run would still need from the caller.
// Parameters are listed with the breadcrumb of the module that declares them,
// because in a composed tree the module that needs a value is often not the one
// you invoke.
func (p *inspectPrinter) summary(tree *module.Inspection) {
	fmt.Fprintln(p.w)
	missing := tree.MissingParams()
	if len(missing) == 0 {
		prettylog.Successf(p.w, "every required parameter is satisfied")
		return
	}
	prettylog.Warningf(p.w, "%d required parameter(s) not supplied — a run would fail:", len(missing))
	nameW := 0
	for _, m := range missing {
		nameW = max(nameW, len(m.Name))
	}
	for _, m := range missing {
		fmt.Fprintf(p.w, "  %s  %s\n", pad(m.Name, nameW), p.style.Muted(strings.Join(m.Path, " › ")))
	}
	fmt.Fprintf(p.w, "\n  %s\n", p.style.Muted("supply them with -p name=value"))
}

// pad right-aligns a column to width, on the uncolored text.
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
