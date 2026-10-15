package server

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CLI is the command line equivalent to a request, with the content of the
// files it references.
type CLI struct {
	Command string `json:"command"`
	// ParamsFile is the content of params.yaml when the command references
	// one: structured and multi-line values do not survive -p on a shell
	// line gracefully, so they go to a params file instead.
	ParamsFile string `json:"paramsFile,omitempty"`
	// ItemsFile is the content of items.yaml for a bulk command.
	ItemsFile string `json:"itemsFile,omitempty"`
}

// File names the commands reference; the files are written next to where
// the command is run.
const (
	cliParamsFile = "params.yaml"
	cliItemsFile  = "items.yaml"
	// cliLocalOut is the --target-path a local run without one is given on
	// the command line; the server itself uses a managed workspace.
	cliLocalOut = "./out"
	// cliDiffKeep is the --target-path of a diff that keeps its workspace.
	cliDiffKeep = "./loom-diff"
)

// inspectCommand is the `loom inspect` line for an inspect request.
func inspectCommand(req inspectRequest, depth int) (CLI, error) {
	var c CLI
	args := []string{"loom", "inspect", req.Source}
	var err error
	if args, c.ParamsFile, err = appendParams(args, req.Params); err != nil {
		return c, err
	}
	switch {
	case depth == 0:
		args = append(args, "--full")
	case depth > 1:
		args = append(args, "--depth", strconv.Itoa(depth))
	}
	for _, m := range req.Modules {
		args = append(args, "-m", m)
	}
	if req.NoFetch {
		args = append(args, "--no-fetch")
	}
	c.Command = shellJoin(args)
	return c, nil
}

// appendParams adds a -p per single-line text value, in name order, and a
// --params-file for the rest, whose content it returns.
func appendParams(args []string, params Params) ([]string, string, error) {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	file := map[string]any{}
	for _, k := range keys {
		v := params[k]
		if s, ok := v.(string); ok && !strings.ContainsAny(s, "\n\r") {
			args = append(args, "-p", k+"="+s)
			continue
		}
		file[k] = v
	}
	if len(file) == 0 {
		return args, "", nil
	}
	text, err := yamlText(file)
	if err != nil {
		return nil, "", err
	}
	return append(args, "--params-file", cliParamsFile), text, nil
}

func appendFlag(args []string, flag, value string) []string {
	if value == "" {
		return args
	}
	return append(args, flag, value)
}

// shellSafe matches words a POSIX shell reads as themselves.
var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellJoin quotes each argument for a POSIX shell and joins them.
func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return strings.Join(out, " ")
}

func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
