//go:build !unix

package proc

import (
	"os/exec"
	"time"
)

// isolate is a no-op beyond what Command already sets (prompt-suppressing
// environment, WaitDelay): there are no sessions or POSIX process groups
// here, so cancellation reaches only the direct child, as with
// exec.CommandContext. WaitDelay still keeps an abandoned grandchild from
// blocking CombinedOutput. `loom serve` targets unix hosts.
func isolate(*exec.Cmd, time.Duration) {}
