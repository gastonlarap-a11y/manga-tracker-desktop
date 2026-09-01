//go:build !windows

package servicecli

import "os/exec"

// hideConsole is a Windows concern: nowhere else does a spawned process bring
// a window with it.
func hideConsole(*exec.Cmd) {}
