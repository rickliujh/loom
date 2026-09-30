package proc

import "syscall"

// sessionIDs returns the calling process's session and process group ids.
// Package syscall has no Getsid wrapper on Linux, only the syscall number.
func sessionIDs() (sid, pgrp int, ok bool) {
	r, _, errno := syscall.RawSyscall(syscall.SYS_GETSID, 0, 0, 0)
	return int(r), syscall.Getpgrp(), errno == 0
}
