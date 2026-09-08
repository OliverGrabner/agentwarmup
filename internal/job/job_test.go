package job

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootIsolatesJobIdentity(t *testing.T) {
	a, b := New(t.TempDir()), New(t.TempDir())
	if a.Name == b.Name {
		t.Fatal("isolated roots share a job")
	}
	if a != New(a.Root) {
		t.Fatal("identity is unstable")
	}
	if e := a.validate(); e != nil {
		t.Fatal(e)
	}
	a.Name = "AgentWarmup"
	if a.validate() == nil {
		t.Fatal("legacy/user job name accepted")
	}
}
func TestRejectInjection(t *testing.T) {
	m := New(t.TempDir())
	for _, bad := range []string{"../agentwarmup", "agentwarmup-x;evil", "agentwarmup-x\nX", ""} {
		m.Name = bad
		if m.validate() == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	m = New(t.TempDir())
	m.Root = "relative"
	if m.validate() == nil {
		t.Fatal("relative root")
	}
}
func TestXMLEscape(t *testing.T) {
	s := `C:\A & B\雪 "quote" <file>`
	encoded := escapeXML(s)
	var got string
	if e := xml.Unmarshal([]byte("<s>"+encoded+"</s>"), &got); e != nil || got != s {
		t.Fatalf("roundtrip %q %v", got, e)
	}
}
func TestAtomicDefinition(t *testing.T) {
	p := filepath.Join(t.TempDir(), "job")
	for _, s := range []string{"first", "second"} {
		if e := writeFile(p, []byte(s)); e != nil {
			t.Fatal(e)
		}
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != "second" {
		t.Fatalf("%q %v", b, e)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentwarmup-") {
			t.Fatal("temporary file leaked")
		}
	}
}
