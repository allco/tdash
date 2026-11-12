//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restart replaces this process with exe, same arguments and environment: the updated binary
// takes over the terminal.
func restart(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ())
}
