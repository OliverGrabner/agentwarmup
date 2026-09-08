//go:build !windows

package schedule

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func DetectTimezone() (string, error) {
	candidates := []string{os.Getenv("TZ")}
	if b, e := os.ReadFile("/etc/timezone"); e == nil {
		candidates = append(candidates, strings.TrimSpace(string(b)))
	}
	if p, e := filepath.EvalSymlinks("/etc/localtime"); e == nil {
		if _, tail, ok := strings.Cut(p, "/zoneinfo/"); ok {
			candidates = append(candidates, tail)
		}
	}
	for _, z := range candidates {
		z = strings.TrimPrefix(z, ":")
		if z != "UTC" && !strings.Contains(z, "/") {
			continue
		}
		if _, err := time.LoadLocation(z); err == nil {
			return z, nil
		}
	}
	return "", fmt.Errorf("cannot detect local IANA timezone; choose an IANA timezone")
}
