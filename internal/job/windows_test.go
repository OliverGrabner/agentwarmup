//go:build windows

package job

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestWindowsDefinition(t *testing.T) {
	m := New(`C:\Users\雪 & Co\50% "test"`)
	s := m.taskXML(`C:\bin & x\agentwarmup.exe`, "S-1-5-21-123", time.Date(2026, 9, 8, 10, 0, 0, 0, time.Local))
	// The text is UTF-8 until the final UTF-16 file serialization.
	var root struct{ XMLName xml.Name }
	if e := xml.Unmarshal([]byte(strings.Replace(s, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)), &root); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"<Interval>PT1M</Interval>", "<LogonType>InteractiveToken</LogonType>", "<RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>", "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>", "wscript.exe", "check.vbs", "&amp;"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}
	if !bytes.HasPrefix(utf16LE(s), []byte{255, 254}) {
		t.Fatal("missing UTF16 BOM")
	}
}
