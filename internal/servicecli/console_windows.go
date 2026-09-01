package servicecli

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW, which the syscall package does not name.
const createNoWindow = 0x08000000

// hideConsole keeps a spawned CLI from opening a console window of its own.
//
// The app is a GUI process, so every Bun it runs would otherwise get a fresh
// console for the second it lives — saving a connection flashed three of them
// across the screen. macOS has no such concept, which is why nothing ever
// showed there.
func hideConsole(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
