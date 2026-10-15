//go:build unix

package proc

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// isolate starts the child as the leader of a new session. Setsid detaches it
// from the server's controlling terminal (so /dev/tty cannot be opened and
// nothing can prompt there) and makes it the leader of a fresh process group
// whose id equals its pid — which is what lets Cancel reach grandchildren.
//
// The stdlib alone is not enough: exec.CommandContext's default Cancel and
// the WaitDelay escalation both signal only cmd.Process, never its group.
// A `sh -c` step whose background child holds the output pipe therefore
// survives cancellation, and before WaitDelay existed it kept CombinedOutput
// blocked until it exited on its own.
func isolate(cmd *exec.Cmd, delay time.Duration) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		// Cancel only runs after a successful Start, so Process is set.
		pgid := cmd.Process.Pid
		err := syscall.Kill(-pgid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			// Nothing left in the group; tell os/exec so it does not
			// report a cancellation error for a command that finished.
			return os.ErrProcessDone
		}
		if err != nil {
			return err
		}
		// Escalate on the same schedule as os/exec's own WaitDelay kill of
		// the leader, but to the whole group: a member that ignores SIGTERM
		// must not outlive the job. While any member lives the kernel keeps
		// the group id reserved, so this cannot hit an unrelated group. Once
		// the group is gone the kill returns ESRCH, unless the pid space
		// wrapped within the grace period and a new group took the id — an
		// accepted risk, as there is no pidfd for a process group. It fires
		// alongside os/exec's timer, which then closes the pipes.
		time.AfterFunc(delay, func() {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		})
		return nil
	}
}
