package proc

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"
)

// setIsolation sets the process-global isolation state for the rest of t and
// restores it afterwards.
//
// Isolation is global, so isolation-on and isolation-off cases must never
// overlap. Go runs t.Parallel tests only once every sequential test has
// finished, so it is enough that a test using this helper is not parallel —
// and t.Setenv enforces exactly that: it panics if t (or an ancestor) calls
// t.Parallel.
func setIsolation(t *testing.T, on bool) {
	t.Helper()
	t.Setenv("LOOM_PROC_TEST_SEQUENTIAL", "1")
	prev := isolated.Load()
	isolated.Store(on)
	t.Cleanup(func() { isolated.Store(prev) })
}

// setWaitDelay shortens the isolation grace period for the rest of t, so the
// SIGKILL escalation can be tested without waiting the production 5s.
func setWaitDelay(t *testing.T, d time.Duration) {
	t.Helper()
	t.Setenv("LOOM_PROC_TEST_SEQUENTIAL", "1")
	prev := waitDelay
	waitDelay = d
	t.Cleanup(func() { waitDelay = prev })
}

// With isolation off, Command must be indistinguishable from
// exec.CommandContext — the CLI relies on children sharing its process group
// (terminal Ctrl-C) and terminal (credential prompts).
func TestCommandIsolationOffMatchesCommandContext(t *testing.T) {
	setIsolation(t, false)
	ctx := context.Background()

	got := Command(ctx, "git", "status", "--short")
	want := exec.CommandContext(ctx, "git", "status", "--short")

	if got.Path != want.Path {
		t.Errorf("Path = %q, want %q", got.Path, want.Path)
	}
	if !slices.Equal(got.Args, want.Args) {
		t.Errorf("Args = %q, want %q", got.Args, want.Args)
	}
	if got.Env != nil {
		t.Errorf("Env = %q, want nil (inherit the parent environment)", got.Env)
	}
	if got.SysProcAttr != nil {
		t.Errorf("SysProcAttr = %+v, want nil", got.SysProcAttr)
	}
	if got.WaitDelay != want.WaitDelay {
		t.Errorf("WaitDelay = %v, want %v", got.WaitDelay, want.WaitDelay)
	}
	if (got.Cancel == nil) != (want.Cancel == nil) {
		t.Errorf("Cancel set = %v, want %v", got.Cancel != nil, want.Cancel != nil)
	}
	if (got.Err == nil) != (want.Err == nil) {
		t.Errorf("Err = %v, want %v", got.Err, want.Err)
	}
}

// With isolation on, the prompt-suppressing variables are appended to the
// inherited environment, and building on cmd.Environ() — the documented way
// to add variables — keeps them.
func TestCommandIsolationOnEnvironment(t *testing.T) {
	setIsolation(t, true)

	cmd := Command(context.Background(), "git", "version")

	inherited := os.Environ()
	if !slices.Equal(cmd.Env[:len(inherited)], inherited) {
		t.Errorf("Env does not start with the inherited environment")
	}
	if tail := cmd.Env[len(inherited):]; !slices.Equal(tail, promptEnv) {
		t.Errorf("appended env = %q, want %q", tail, promptEnv)
	}
	if cmd.WaitDelay != waitDelay {
		t.Errorf("WaitDelay = %v, want %v", cmd.WaitDelay, waitDelay)
	}
	if cmd.Cancel == nil {
		t.Error("Cancel is nil")
	}

	cmd.Env = append(cmd.Environ(), "LOOM_EXTRA=1")
	for _, kv := range append(slices.Clone(promptEnv), "LOOM_EXTRA=1") {
		if !slices.Contains(cmd.Env, kv) {
			t.Errorf("Env built on Environ() lacks %q", kv)
		}
	}
}

// Command reads the isolation flag on every call, possibly from many
// goroutines at once; run under -race this proves the flag is safe to flip
// while commands are being built.
func TestCommandConcurrentWithToggle(t *testing.T) {
	setIsolation(t, false)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				_ = Command(context.Background(), "true")
			}
		})
	}
	wg.Go(func() {
		for i := range 100 {
			isolated.Store(i%2 == 0)
		}
	})
	wg.Wait()
}
