// Package proc is the single place Loom creates subprocesses (git, gh, glab,
// shell steps). Routing every exec through here lets `loom serve` change how
// children are started — so a cancelled job really stops and nothing can
// prompt on the server's terminal — without touching each call site.
package proc

import (
	"context"
	"os"
	"os/exec"
	"sync/atomic"
	"time"
)

// isolated is process-global on purpose: server mode is a property of the
// whole process, decided once at startup. Atomic because Command is called
// from many goroutines (concurrent jobs, bulk runs).
var isolated atomic.Bool

// waitDelay bounds how long a cancelled isolated command may linger: after
// it, the process group is SIGKILLed and the I/O pipes are closed. A variable
// only so tests can shorten it; production never changes it.
var waitDelay = 5 * time.Second

// promptEnv disables every credential prompt git and its helpers know of.
// GIT_TERMINAL_PROMPT covers git's own username/password prompt;
// GCM_INTERACTIVE covers Git Credential Manager's UI; SSH_ASKPASS_REQUIRE
// stops ssh from falling back to an askpass program (a GUI passphrase dialog
// on a desktop session), which setsid alone leaves open: with no terminal,
// ssh reaches for askpass precisely when DISPLAY is set.
var promptEnv = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GCM_INTERACTIVE=never",
	"SSH_ASKPASS_REQUIRE=never",
}

// EnableIsolation switches the process into server mode. Called once by
// `loom serve` before it starts listening; never by the CLI.
//
// It is opt-in because isolation is wrong for the CLI: a child in its own
// session no longer receives the terminal's Ctrl-C, so an interrupted CLI run
// would leave shell steps orphaned, and git could no longer ask the user for
// credentials they are sitting there to type.
func EnableIsolation() { isolated.Store(true) }

// Command returns an *exec.Cmd bound to ctx. With isolation off (the default,
// and always the case for the CLI) it behaves exactly like exec.CommandContext.
//
// With isolation on, the child additionally:
//   - runs in its own session and process group, with no controlling
//     terminal, so git/ssh cannot prompt on the server's terminal (unix only);
//   - is stopped as a whole tree when ctx is done: SIGTERM to the process
//     group, then SIGKILL to the group and closed pipes after a grace period,
//     so a grandchild holding stdout cannot keep CombinedOutput blocked (the
//     same grace period also bounds the wait for pipes after a child exits
//     normally but leaves such a grandchild behind: Wait returns
//     exec.ErrWaitDelay rather than hanging);
//   - has GIT_TERMINAL_PROMPT=0, GCM_INTERACTIVE=never and
//     SSH_ASKPASS_REQUIRE=never appended to the inherited environment.
//
// Environment contract: isolation pre-populates cmd.Env. To add variables,
// build on cmd.Environ() — `cmd.Env = append(cmd.Environ(), "K=V")` — which
// is correct in both modes and keeps the prompt suppression. Assigning a
// fresh slice to cmd.Env replaces the inherited environment, the prompt
// suppression included, exactly as it would with exec.Command. Because Env is
// non-nil, os/exec no longer derives PWD from cmd.Dir for isolated children;
// shells and git validate PWD against the real working directory, so this
// only matters to a program that trusts $PWD blindly.
//
// Callers must not replace cmd.SysProcAttr, cmd.Cancel or cmd.WaitDelay on an
// isolated command; doing so silently undoes the isolation.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	if isolated.Load() {
		cmd.Env = append(os.Environ(), promptEnv...)
		cmd.WaitDelay = waitDelay
		isolate(cmd, waitDelay)
	}
	return cmd
}
