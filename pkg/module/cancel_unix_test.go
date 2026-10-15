//go:build unix

package module

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rickliujh/loom/internal/proc"
)

// isolatedEnv makes TestMain switch the test binary into server mode
// (proc.EnableIsolation) before running tests. Isolation is process-global and
// cannot be switched off again, so the isolated cases run in a child copy of
// this binary rather than flipping it for every test here.
const isolatedEnv = "LOOM_MODULE_TEST_ISOLATED"

func TestMain(m *testing.M) {
	if os.Getenv(isolatedEnv) == "1" {
		proc.EnableIsolation()
	}
	os.Exit(m.Run())
}

// pidReport is a FIFO a test command writes its pids into. A FIFO rather than
// a file: opening it blocks until the command reaches that point, so the test
// learns the command is running without polling or sleeping.
type pidReport struct {
	path  string
	lines chan string
}

func newPidReport(t *testing.T) *pidReport {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pids")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	r := &pidReport{path: path, lines: make(chan string, 1)}
	go func() {
		f, err := os.Open(path)
		if err != nil {
			r.lines <- ""
			return
		}
		defer f.Close()
		line, _ := bufio.NewReader(f).ReadString('\n')
		r.lines <- line
	}()
	return r
}

// await returns the pids the command reported, failing the test if the
// command finished (done fired) or went quiet first. Every reported pid is
// SIGKILLed at cleanup if it is somehow still alive, so a failing test leaves
// no sleep behind.
func (r *pidReport) await(t *testing.T, done <-chan error) []int {
	t.Helper()
	var line string
	select {
	case line = <-r.lines:
	case err := <-done:
		t.Fatalf("finished before reporting pids: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the command to report its pids")
	}
	var pids []int
	for _, f := range strings.Fields(line) {
		pid, err := strconv.Atoi(f)
		if err != nil || pid <= 1 {
			t.Fatalf("unexpected pid report %q", line)
		}
		pids = append(pids, pid)
	}
	if len(pids) == 0 {
		t.Fatalf("unexpected pid report %q", line)
	}
	t.Cleanup(func() {
		for _, pid := range pids {
			if !pidGone(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	return pids
}

// pidGone reports whether pid has exited; a zombie awaiting its reaper counts.
func pidGone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	return i >= 0 && i+2 < len(s) && s[i+2] == 'Z'
}

// waitPidsGone polls until every pid has exited, failing after timeout.
func waitPidsGone(t *testing.T, pids []int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for _, pid := range pids {
		for !pidGone(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("pid %d still alive %v after cancellation", pid, timeout)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// awaitErr waits for done, failing the test if it takes longer than limit.
func awaitErr(t *testing.T, done <-chan error, limit time.Duration) (error, time.Duration) {
	t.Helper()
	start := time.Now()
	select {
	case err := <-done:
		return err, time.Since(start)
	case <-time.After(limit):
		t.Fatalf("did not return within %v of cancellation", limit)
		return nil, 0
	}
}

// Scripts. The plain form execs into sleep, so the reported pid is the one
// process there is: killing the direct child — all a CLI run can do — stops it.
// The tree form backgrounds a sleep that holds the output pipe, which only
// isolation's process-group kill reaches.
func execScript(fifo string) string {
	return fmt.Sprintf(`echo $$ > '%s'; exec sleep 60`, fifo)
}

func treeScript(fifo string) string {
	return fmt.Sprintf(`sleep 60 & echo "$$ $!" > '%s'; wait`, fifo)
}

// cancelCase drives one kind of subprocess through its real entry point.
type cancelCase struct {
	name string
	// start begins the work in the background with script as the command;
	// marker is a file that a step after the cancelled one would create.
	start func(t *testing.T, ctx context.Context, script, marker string) <-chan error
}

func cancelCases() []cancelCase {
	return []cancelCase{
		{
			// A dynamic param with a default: the default must not rescue a
			// cancelled command, or loading would carry on as if it failed.
			name: "dynamic param",
			start: func(t *testing.T, ctx context.Context, script, _ string) <-chan error {
				dir := t.TempDir()
				writeLoomYAML(t, dir, fmt.Sprintf(`
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: cancel-dp
spec:
  dynamicParams:
    - name: slow
      command: %q
      default: fallback
  operations: []
`, script))
				done := make(chan error, 1)
				go func() {
					_, err := LoadContext(ctx, dir, nil, testLogger())
					done <- err
				}()
				return done
			},
		},
		{
			// An op's if predicate: a killed predicate must fail the run, not
			// read as "false" and skip on to the next step.
			name: "if condition",
			start: func(t *testing.T, ctx context.Context, script, marker string) <-chan error {
				return executeOps(t, ctx, fmt.Sprintf(`
    - name: guarded
      if: %q
      shell:
        command: "touch '%s'"
    - name: after
      shell:
        command: "touch '%s'"
`, script, marker, marker))
			},
		},
		{
			name: "shell step",
			start: func(t *testing.T, ctx context.Context, script, marker string) <-chan error {
				return executeOps(t, ctx, fmt.Sprintf(`
    - name: slow
      shell:
        command: %q
    - name: after
      shell:
        command: "touch '%s'"
`, script, marker))
			},
		},
	}
}

// executeOps loads a module with the given operations and runs it in the
// background.
func executeOps(t *testing.T, ctx context.Context, ops string) <-chan error {
	t.Helper()
	dir := t.TempDir()
	writeLoomYAML(t, dir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: cancel-ops
spec:
  operations:`+ops)
	mod, err := Load(dir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	done := make(chan error, 1)
	go func() { done <- Execute(ctx, mod, target, RunOptions{}) }()
	return done
}

// A cancelled context stops a running dynamic-param command, if predicate and
// shell step mid-run — the process is gone and the call returns the
// cancellation — and nothing after the cancelled step runs.
func TestCancelStopsRunningCommand(t *testing.T) {
	for _, tc := range cancelCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			report := newPidReport(t)
			marker := filepath.Join(t.TempDir(), "ran")

			done := tc.start(t, ctx, execScript(report.path), marker)
			pids := report.await(t, done)
			cancel()

			err, _ := awaitErr(t, done, 10*time.Second)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want one wrapping context.Canceled", err)
			}
			waitPidsGone(t, pids, 10*time.Second)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Error("a step after the cancelled one ran")
			}
		})
	}
}

// hookHandler hands every record to fn and writes nothing, so a test can act
// at an exact point in a run — the executor logs at each step boundary.
type hookHandler struct{ fn func(slog.Record) }

func (h hookHandler) Enabled(context.Context, slog.Level) bool      { return true }
func (h hookHandler) Handle(_ context.Context, r slog.Record) error { h.fn(r); return nil }
func (h hookHandler) WithAttrs([]slog.Attr) slog.Handler            { return h }
func (h hookHandler) WithGroup(string) slog.Handler                 { return h }

// Once ctx is done the executor starts nothing new: an operation that already
// finished stands, the next operation — or the next child module — is never
// begun, and the run reports the cancellation.
func TestCancelStopsBeforeNextStep(t *testing.T) {
	t.Run("operation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		marker := filepath.Join(t.TempDir(), "ran")
		var msgs []string
		logger := slog.New(hookHandler{fn: func(r slog.Record) {
			msgs = append(msgs, r.Message)
			// Logged by the first op after its command has finished.
			if r.Message == "shell output" {
				cancel()
			}
		}})

		dir := t.TempDir()
		writeLoomYAML(t, dir, fmt.Sprintf(`
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: cancel-next
spec:
  operations:
    - name: first
      shell:
        command: "echo done"
    - name: second
      shell:
        command: "touch '%s'"
`, marker))
		mod, err := Load(dir, nil, logger)
		if err != nil {
			t.Fatal(err)
		}
		err = Execute(ctx, mod, t.TempDir(), RunOptions{})
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), `stopped before operation "second"`) {
			t.Fatalf("err = %v, want a cancellation before operation \"second\"", err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Error("the operation after the cancellation ran")
		}
		for _, m := range msgs {
			if strings.HasPrefix(m, "operation second") {
				t.Errorf("the next operation was announced: %q", m)
			}
		}
	})

	t.Run("child module", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		marker := filepath.Join(t.TempDir(), "ran")
		var msgs []string
		logger := slog.New(hookHandler{fn: func(r slog.Record) {
			msgs = append(msgs, r.Message)
			if r.Message == "shell output" {
				cancel()
			}
		}})

		parent := t.TempDir()
		for name, cmd := range map[string]string{"one": "echo done", "two": "touch '" + marker + "'"} {
			dir := filepath.Join(parent, name)
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeLoomYAML(t, dir, fmt.Sprintf(`
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: %s
spec:
  operations:
    - name: work
      shell:
        command: %q
`, name, cmd))
		}
		writeLoomYAML(t, parent, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: cancel-parent
spec:
  modules:
    - name: one
      source: ./one
    - name: two
      source: ./two
  operations: []
`)
		mod, err := Load(parent, nil, logger)
		if err != nil {
			t.Fatal(err)
		}
		err = Execute(ctx, mod, t.TempDir(), RunOptions{})
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), `stopped before child module "two"`) {
			t.Fatalf("err = %v, want a cancellation before child module \"two\"", err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Error("the child module after the cancellation ran")
		}
		for _, m := range msgs {
			if strings.HasPrefix(m, "two (") {
				t.Errorf("the next child was dispatched: %q", m)
			}
		}
	})
}

// With isolation on — as under `loom serve` — cancelling reaches the whole
// process tree: a backgrounded grandchild holding the output pipe dies with
// the shell, and the call returns at once instead of after the WaitDelay
// grace period (or, for the CLI, whenever the grandchild exits by itself).
// The assertions run in a child copy of the test binary with isolation
// enabled; this test only launches it.
func TestCancelIsolationKillsProcessTree(t *testing.T) {
	if os.Getenv(isolatedEnv) == "1" {
		t.Skip("already the isolated child")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCancelIsolatedTree$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), isolatedEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated run failed: %v\n%s", err, out)
	}
	// Guard against the child silently running nothing.
	for _, tc := range cancelCases() {
		if want := "--- PASS: TestCancelIsolatedTree/" + strings.ReplaceAll(tc.name, " ", "_"); !strings.Contains(string(out), want) {
			t.Errorf("isolated run lacks %q:\n%s", want, out)
		}
	}
}

// TestCancelIsolatedTree is the body of TestCancelIsolationKillsProcessTree;
// it only runs in the isolated child.
func TestCancelIsolatedTree(t *testing.T) {
	if os.Getenv(isolatedEnv) != "1" {
		t.Skip("runs in the isolated child of TestCancelIsolationKillsProcessTree")
	}
	for _, tc := range cancelCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			report := newPidReport(t)
			marker := filepath.Join(t.TempDir(), "ran")

			done := tc.start(t, ctx, treeScript(report.path), marker)
			pids := report.await(t, done)
			cancel()

			err, elapsed := awaitErr(t, done, 20*time.Second)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want one wrapping context.Canceled", err)
			}
			// The group SIGTERM ends it; reaching the 5s WaitDelay would mean
			// the grandchild outlived the shell and had to be escalated.
			if elapsed >= 4*time.Second {
				t.Errorf("returned after %v, want well under the isolation grace period", elapsed)
			}
			waitPidsGone(t, pids, 10*time.Second)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Error("a step after the cancelled one ran")
			}
		})
	}
}
