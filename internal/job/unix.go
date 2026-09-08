//go:build !windows

package job

import (
	"os"
	"os/exec"
)

func hide(c *exec.Cmd)                  {}
func replaceFile(from, to string) error { return os.Rename(from, to) }
