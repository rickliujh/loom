//go:build unix && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package proc

// sessionIDs is unavailable: package syscall lacks the wrappers here, and the
// module avoids a direct golang.org/x/sys dependency for one test assertion.
func sessionIDs() (sid, pgrp int, ok bool) { return 0, 0, false }
