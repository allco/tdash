package main

import (
	"errors"
	"os"
	"os/exec"
)

// restart runs exe in this console with the same arguments, and exits with its code once it
// ends: Windows has no exec that replaces the process.
func restart(exe string) error {
	c := exec.Command(exe, os.Args[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := c.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
