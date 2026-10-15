//go:build unix

package proc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperEnv makes the test binary act as a child process instead of running
// tests, so a child can report facts about itself (session, terminal, env)
// without depending on ps or a particular shell.
const helperEnv = "LOOM_PROC_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "session" {
		_, ttyErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		sid, pgrp := "unknown", "unknown"
		if s, p, ok := sessionIDs(); ok {
			sid, pgrp = strconv.Itoa(s), strconv.Itoa(p)
		}
		fmt.Printf("pid=%d\nsid=%s\npgrp=%s\ntty=%v\nGIT_TERMINAL_PROMPT=%s\nGCM_INTERACTIVE=%s\nSSH_ASKPASS_REQUIRE=%s\n",
			os.Getpid(), sid, pgrp, ttyErr == nil,
			os.Getenv("GIT_TERMINAL_PROMPT"), os.Getenv("GCM_INTERACTIVE"), os.Getenv("SSH_ASKPASS_REQUIRE"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// treeScript starts a grandchild (`sleep`) that inherits the combined output
// pipe — the shape of a shell step that backgrounds a process — and reports
// "<sh pid> <sleep pid>" on fd 3. The sleep closes its copy of fd 3 so the
// report pipe's lifetime is not tied to it.
const treeScript = `sleep 60 3>&- & echo "$$ $!" >&3; wait`

// startTree runs script via Command(...).CombinedOutput in the background and
// returns the grandchild pid the script reported on fd 3 plus a channel
// carrying CombinedOutput's error. configure, if non-nil, adjusts the command before
// it starts. Any process left alive at the end of the test is killed.
func startTree(t *testing.T, ctx context.Context, script string, configure func(*exec.Cmd)) (grandchild int, done <-chan error) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })

	cmd := Command(ctx, "sh", "-c", script)
	cmd.ExtraFiles = []*os.File{w}
	if configure != nil {
		configure(cmd)
	}

	errc := make(chan error, 1)
	go func() {
		_, err := cmd.CombinedOutput()
		errc <- err
	}()

	linec := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(r).ReadString('\n')
		linec <- line
	}()

	var line string
	select {
	case line = <-linec:
	case err := <-errc:
		t.Fatalf("command exited before reporting pids: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the script to report pids")
	}

	fields := strings.Fields(line)
	if len(fields) != 2 {
		t.Fatalf("unexpected pid report %q", line)
	}
	leader, err1 := strconv.Atoi(fields[0])
	grandchild, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil || leader <= 1 || grandchild <= 1 {
		t.Fatalf("unexpected pid report %q", line)
	}

	// Never leave a stray sleep behind, whatever the test concluded. Pids
	// are killed individually, never as a group: with isolation off the
	// group would be the test binary's own.
	t.Cleanup(func() {
		for _, pid := range []int{grandchild, leader} {
			if !gone(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	return grandchild, errc
}

// gone reports whether pid has exited. A zombie counts as gone: it is dead
// and only awaits reaping by its (possibly new) parent.
func gone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	// Linux only; elsewhere the read fails and we rely on ESRCH alone.
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// The state follows the parenthesised command name, which may itself
	// contain spaces or parentheses.
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	return i >= 0 && i+2 < len(s) && s[i+2] == 'Z'
}

// waitGone polls until pid has exited, failing the test after timeout.
func waitGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !gone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still alive %v after the command returned", pid, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// awaitDone waits for the command to return, failing the test if it takes
// longer than limit, and reports how long it took since start. Callers take
// start before cancelling, so elapsed never undercounts the time since the
// cancellation took effect.
func awaitDone(t *testing.T, done <-chan error, start time.Time, limit time.Duration) (time.Duration, error) {
	t.Helper()
	select {
	case err := <-done:
		return time.Since(start), err
	case <-time.After(limit):
		t.Fatalf("command did not return within %v of cancellation", time.Since(start))
		return 0, nil
	}
}

// Cancelling an isolated command SIGTERMs its whole process group: the
// backgrounded grandchild dies with the shell, so CombinedOutput returns
// promptly instead of waiting for the grandchild to release the pipe.
func TestCommandIsolationCancelKillsGrandchild(t *testing.T) {
	setIsolation(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	grandchild, done := startTree(t, ctx, treeScript, nil)
	start := time.Now()
	cancel()

	elapsed, err := awaitDone(t, done, start, waitDelay+10*time.Second)
	if err == nil {
		t.Error("expected an error from a cancelled command")
	}
	// SIGTERM alone must have ended it, not the WaitDelay fallback.
	if elapsed >= waitDelay {
		t.Errorf("returned after %v, want well under WaitDelay (%v)", elapsed, waitDelay)
	}
	waitGone(t, grandchild, 10*time.Second)
}

// A group member that ignores SIGTERM is SIGKILLed once the grace period
// expires. os/exec's own WaitDelay escalation kills only the direct child, so
// without the group SIGKILL the grandchild would survive the job.
func TestCommandIsolationEscalatesToSIGKILL(t *testing.T) {
	setIsolation(t, true)
	setWaitDelay(t, 300*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The ignored disposition is inherited by the backgrounded sleep.
	grandchild, done := startTree(t, ctx, `trap '' TERM; `+treeScript, nil)
	start := time.Now()
	cancel()

	elapsed, err := awaitDone(t, done, start, waitDelay+10*time.Second)
	if err == nil {
		t.Error("expected an error from a cancelled command")
	}
	// Returning before the grace period would mean SIGTERM was not ignored
	// and this test is not exercising the escalation.
	if elapsed < waitDelay {
		t.Errorf("returned after %v, before the %v grace period", elapsed, waitDelay)
	}
	waitGone(t, grandchild, 10*time.Second)
}

// Documents what isolation fixes: without it, cancellation kills only the
// shell. The grandchild survives and keeps the output pipe open; this test
// bounds the wait with its own WaitDelay, but the CLI sets none, so there
// CombinedOutput would block until the grandchild exits by itself.
func TestCommandNoIsolationCancelLeavesGrandchild(t *testing.T) {
	setIsolation(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	grandchild, done := startTree(t, ctx, treeScript, func(cmd *exec.Cmd) {
		cmd.WaitDelay = 300 * time.Millisecond
	})
	start := time.Now()
	cancel()

	if _, err := awaitDone(t, done, start, 10*time.Second); err == nil {
		t.Error("expected an error from a cancelled command")
	}
	if gone(grandchild) {
		t.Fatal("grandchild exited; expected it to survive a non-isolated cancel")
	}
}

// An isolated child leads its own session: it has no controlling terminal
// to prompt on, and sees the prompt-suppressing environment.
func TestCommandIsolationSessionAndEnv(t *testing.T) {
	setIsolation(t, true)

	cmd := Command(context.Background(), os.Args[0])
	cmd.Env = append(cmd.Environ(), helperEnv+"=session")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}

	facts := map[string]string{}
	for line := range strings.Lines(string(out)) {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		facts[k] = v
	}
	// Package syscall cannot report the ids on every unix; where it cannot,
	// the terminal and environment checks below still apply.
	if facts["sid"] != "unknown" {
		if facts["sid"] != facts["pid"] {
			t.Errorf("sid = %s, want the child's pid %s (session leader)", facts["sid"], facts["pid"])
		}
		if facts["pgrp"] != facts["pid"] {
			t.Errorf("pgrp = %s, want the child's pid %s (group leader)", facts["pgrp"], facts["pid"])
		}
	}
	if facts["tty"] != "false" {
		t.Error("child could open /dev/tty; want no controlling terminal")
	}
	if facts["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT = %q, want 0", facts["GIT_TERMINAL_PROMPT"])
	}
	if facts["GCM_INTERACTIVE"] != "never" {
		t.Errorf("GCM_INTERACTIVE = %q, want never", facts["GCM_INTERACTIVE"])
	}
	if facts["SSH_ASKPASS_REQUIRE"] != "never" {
		t.Errorf("SSH_ASKPASS_REQUIRE = %q, want never", facts["SSH_ASKPASS_REQUIRE"])
	}
}
