//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package proc

import "syscall"

// sessionIDs returns the calling process's session and process group ids.
func sessionIDs() (sid, pgrp int, ok bool) {
	sid, err := syscall.Getsid(0)
	return sid, syscall.Getpgrp(), err == nil
}
