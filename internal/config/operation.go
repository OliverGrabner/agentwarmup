package config

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// OperationLock survives root removal, so uninstall cannot race a new setup.
// It is intentionally retained after uninstall; deleting a held lock would
// allow another process to lock a different file at the same pathname.
func (p Paths) OperationLock() string {
	root := filepath.Clean(p.Root)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(filepath.Dir(p.Root), fmt.Sprintf(".agentwarmup-%x.lock", sum[:16]))
}
