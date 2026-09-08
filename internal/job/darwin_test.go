//go:build darwin

package job

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestLaunchAgentDefinition(t *testing.T) {
	m := New(`/tmp/雪 & 50% "quote"`)
	s := m.plist("/tmp/bin & app/agentwarmup")
	var v any
	if e := xml.Unmarshal([]byte(s), &v); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"<integer>60</integer>", "<key>RunAtLoad</key><true/>", "<string>--home</string>", "&amp;"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}
}
