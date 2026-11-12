//go:build !linux

package sys

import "os/exec"

func EndWithParent(cmd *exec.Cmd) {}
