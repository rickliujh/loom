package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rickliujh/loom/internal/server"
	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/params"
	"gopkg.in/yaml.v3"
)

// shellSplit splits a command line the way a POSIX shell would, for the
// quoting server.CLICommand produces: single quotes, backslash escapes,
// whitespace between words.
func shellSplit(t *testing.T, line string) []string {
	t.Helper()
	var words []string
	var cur strings.Builder
	inWord, quoted := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quoted:
			if c == '\'' {
				quoted = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			quoted, inWord = true, true
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
			inWord = true
		case c == ' ':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quoted {
		t.Fatalf("unterminated quote in %q", line)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

func resetServeCLIFlags() {
	resetFlags()
	gitAuthor, gitEmail = "", ""
	genParams, genOutput, genName, genTokenEnv = nil, "", "", ""
	bulkOutput, bulkName, bulkItems, bulkNameParam = "", "", "", ""
}

// parseBack runs the command's arguments through the real flag set and
// rebuilds the request they describe, reading the params and items files
// from the CLI result.
func parseBack(t *testing.T, c server.CLI) server.JobRequest {
	t.Helper()
	resetServeCLIFlags()
	words := shellSplit(t, c.Command)
	if len(words) < 2 || words[0] != "loom" {
		t.Fatalf("command %q does not start with loom", c.Command)
	}
	cmd, rest, err := rootCmd.Find(words[1:])
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.ParseFlags(rest); err != nil {
		t.Fatalf("parsing %q: %v", c.Command, err)
	}
	args := cmd.Flags().Args()
	if len(args) != 1 {
		t.Fatalf("%q: positional args %v", c.Command, args)
	}

	dir := t.TempDir()
	file := func(name, content, flag string) string {
		if flag == "" {
			if content != "" {
				t.Errorf("%q: a %s was returned but not referenced", c.Command, name)
			}
			return ""
		}
		if flag != name || content == "" {
			t.Fatalf("%q references %q, returned content %q", c.Command, flag, content)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	req := server.JobRequest{Kind: cmd.Name()}
	switch cmd.Name() {
	case "run":
		req.Source = args[0]
		p, err := params.Parse(runParams, file("params.yaml", c.ParamsFile, paramsFile))
		if err != nil {
			t.Fatal(err)
		}
		req.Params = p
		switch {
		case dryRun:
			req.Mode = server.ModeDryRun
		case localRun:
			req.Mode = server.ModeLocal
		default:
			req.Mode = server.ModeExecute
		}
		req.TargetPath, req.Author, req.Email = targetPath, gitAuthor, gitEmail
	case "diff":
		req.Source = args[0]
		p, err := params.Parse(diffParams, file("params.yaml", c.ParamsFile, diffParamsFile))
		if err != nil {
			t.Fatal(err)
		}
		req.Params = p
		req.Quick = diffQuick
		req.KeepWorkspace = diffTargetPath != ""
		req.Author, req.Email = diffAuthor, diffEmail
		if !diffPartial {
			t.Errorf("%q: the server always behaves as --partial", c.Command)
		}
	case "generate":
		req.Refs = []string{args[0]}
		v, err := params.ParseCLI(genParams)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) > 0 {
			req.Values = v
		}
		req.Output, req.Name, req.TokenEnv = genOutput, genName, genTokenEnv
	case "bulk":
		req.Module = args[0]
		req.Output, req.Name, req.NameParam = bulkOutput, bulkName, bulkNameParam
		if p := file("items.yaml", c.ItemsFile, bulkItems); p != "" {
			// Items decode as `loom bulk --items` decodes them.
			data, _ := os.ReadFile(p)
			var doc yaml.Node
			if err := yaml.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			for _, n := range doc.Content[0].Content {
				m, err := config.DecodeParamValues(n)
				if err != nil {
					t.Fatal(err)
				}
				req.Items = append(req.Items, m)
			}
		}
	default:
		t.Fatalf("unexpected command %s", cmd.Name())
	}
	return req
}

// normalize makes empty and nil params compare equal and turns Params into
// plain maps.
func normalize(r server.JobRequest) server.JobRequest {
	if len(r.Params) == 0 {
		r.Params = nil
	}
	return r
}

// SV12: the command line shown for a request, parsed by the real flag set,
// is that request again — params, structured values and all.
func TestServe_SV12_CLIRoundTrip(t *testing.T) {
	cases := []string{
		`{"kind":"run","source":"/home/u/gitops/modules/onboard","mode":"execute",
		  "params":{"serviceName":"payments","sources":[{"repoURL":"https://charts.example.com","targetRevision":1.10,"n":3,"f":1.5,"on":true}],"labels":{"team":"core"}},
		  "author":"Jane Doe","email":"jane@example.com"}`,
		`{"kind":"run","source":"https://github.com/o/mods.git//onboard","mode":"dry-run",
		  "params":{"title":"it's a \"test\" $HOME","regions":"- eu\n- us\n","empty":"","eq":"a=b=c"}}`,
		`{"kind":"run","source":"/m","mode":"local","targetPath":"/tmp/out dir","params":{"replicas":3}}`,
		`{"kind":"diff","source":"/m","quick":true,"params":{"a":"b"}}`,
		`{"kind":"diff","source":"file:///repo//m","keepWorkspace":true,"author":"x","email":"y"}`,
		`{"kind":"diff","source":"/m","params":{"list":["a",["b"]]}}`,
		`{"kind":"generate","refs":["https://github.com/o/r/pull/12"],"values":{"svc":"payments","ns":"team a"},
		  "name":"onboard","output":"/home/u/gitops/modules/new","tokenEnv":"GH_TOKEN"}`,
		`{"kind":"generate","refs":["github:o/r#12"],"output":"/out"}`,
		`{"kind":"bulk","module":"/home/u/gitops/modules/onboard","name":"q3","nameParam":"serviceName",
		  "items":[{"serviceName":"a","sources":[{"v":1.10}]},{"serviceName":"b","replicas":"3"}],"output":"/home/u/gitops/batches/q3"}`,
		`{"kind":"bulk","module":"git@github.com:o/r.git//m","output":"/out"}`,
	}
	for _, body := range cases {
		var req server.JobRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		c, err := server.CLICommand(req)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		got := parseBack(t, c)
		if !reflect.DeepEqual(normalize(got), normalize(req)) {
			t.Errorf("round trip of %s\ncommand: %s\nparams file:\n%s\ngot  %#v\nwant %#v", body, c.Command, c.ParamsFile, got, req)
		}
	}
	resetServeCLIFlags()
}
