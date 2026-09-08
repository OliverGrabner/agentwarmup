//go:build linux

package job

import (
	"strings"
	"testing"
)

func TestSystemdEscaping(t *testing.T) {
	m := New(`/tmp/雪 & 50% $HOME "quote"`)
	s := m.service(`/tmp/a b%$c/agentwarmup`)
	for _, want := range []string{`"/tmp/a b%%$$c/agentwarmup"`, `check --home`, `50%% $$HOME \"quote\"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q from %s", want, s)
		}
	}
	if !strings.Contains(m.timer(), "OnCalendar=*-*-* *:*:00") || !strings.Contains(m.timer(), "Persistent=false") {
		t.Fatal("incorrect timer")
	}
}
