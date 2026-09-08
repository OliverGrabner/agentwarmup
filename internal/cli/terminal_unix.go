//go:build !windows

package cli

import "os"

func terminalOutput(*os.File) (func() error, error) { return func() error { return nil }, nil }
