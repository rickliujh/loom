package server

import (
	"net/http"
	"sort"
)

// CLICommand returns the `loom` command line that does what req asks. Parsed
// by the real flag set, the command yields req back (params included, with
// ParamsFile's content as the params file).
func CLICommand(req JobRequest) (CLI, error) {
	return cliFor(req, "")
}

// cliFor is CLICommand for a job, whose server-managed workspace, when it has
// one, is what its command line names.
func cliFor(req JobRequest, workspace string) (CLI, error) {
	var c CLI
	var args []string
	switch req.Kind {
	case KindRun:
		args = []string{"loom", "run", req.Source}
		var err error
		if args, c.ParamsFile, err = appendParams(args, req.Params); err != nil {
			return c, err
		}
		switch req.Mode {
		case ModeDryRun:
			args = append(args, "--dry-run")
		case ModeLocal:
			args = append(args, "--local-run")
		}
		tp := req.TargetPath
		if tp == "" && req.Mode == ModeLocal {
			tp = cliLocalOut
			if workspace != "" {
				tp = workspace
			}
		}
		args = appendFlag(args, "--target-path", tp)
		args = appendFlag(args, "--author", req.Author)
		args = appendFlag(args, "--email", req.Email)
	case KindDiff:
		args = []string{"loom", "diff", req.Source}
		var err error
		if args, c.ParamsFile, err = appendParams(args, req.Params); err != nil {
			return c, err
		}
		if req.Quick {
			args = append(args, "--quick")
		} else if req.KeepWorkspace {
			tp := cliDiffKeep
			if workspace != "" {
				tp = workspace
			}
			args = appendFlag(args, "--target-path", tp)
		}
		args = appendFlag(args, "--author", req.Author)
		args = appendFlag(args, "--email", req.Email)
		// The server always collects a failed run's diffs.
		args = append(args, "--partial")
	case KindGenerate:
		ref := ""
		if len(req.Refs) > 0 {
			ref = req.Refs[0]
		}
		args = []string{"loom", "generate", ref}
		keys := make([]string, 0, len(req.Values))
		for k := range req.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-p", k+"="+req.Values[k])
		}
		args = appendFlag(args, "-o", req.Output)
		args = appendFlag(args, "-n", req.Name)
		args = appendFlag(args, "--token-env", req.TokenEnv)
	case KindBulk:
		args = []string{"loom", "bulk", req.Module}
		args = appendFlag(args, "-o", req.Output)
		args = appendFlag(args, "-n", req.Name)
		if req.Items != nil {
			items := make([]any, len(req.Items))
			for i, it := range req.Items {
				m := map[string]any{}
				for k, v := range it {
					m[k] = v
				}
				items[i] = m
			}
			text, err := yamlText(items)
			if err != nil {
				return c, err
			}
			c.ItemsFile = text
			args = append(args, "--items", cliItemsFile)
		}
		args = appendFlag(args, "--name-param", req.NameParam)
	case "":
		return c, errInvalid("kind", "kind is required: run, diff, generate or bulk")
	default:
		return c, errInvalid("kind", "unknown kind %q: expected run, diff, generate or bulk", req.Kind)
	}
	c.Command = shellJoin(args)
	return c, nil
}

func (s *Server) handleCLI(w http.ResponseWriter, r *http.Request) error {
	var req JobRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	c, err := CLICommand(req)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, c)
	return nil
}
