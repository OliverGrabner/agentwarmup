package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{SchemaVersion: 1, ResetTime: "10:00", ResetDays: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}, Timezone: "America/Chicago", Enabled: true, Providers: []ProviderConfig{{ID: "codex", Executable: filepath.Join(t.TempDir(), "codex.exe")}}, EffectiveFrom: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)}
}
func TestRoundtripAndAtomicReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := testConfig(t)
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	c.Enabled = false
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	d, err := Load(path)
	if err != nil || d.Current == nil || d.Current.Enabled {
		t.Fatalf("load %+v %v", d, err)
	}
	if !d.Current.EffectiveFrom.Equal(c.EffectiveFrom) {
		t.Fatal("activation changed")
	}
}
func TestMigrationReadHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	old := LegacyConfig{ResetTime: "02:00", Days: []string{"Mon", "Tue"}, Enabled: false, CatchUp: true, CodexPath: filepath.Join(dir, "codex.exe")}
	data, _ := json.Marshal(old)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Load(path)
	if err != nil || d.Legacy == nil {
		t.Fatalf("load %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("load rewrote file")
	}
	now := time.Now().UTC()
	c, err := Migrate(*d.Legacy, "America/Chicago", now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled || c.ResetTime != old.ResetTime || c.Providers[0].Executable != old.CodexPath || !c.EffectiveFrom.Equal(now) {
		t.Fatal("migration lost intent")
	}
	after, _ = os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("migration wrote file")
	}
}
func TestRejectInvalidConfig(t *testing.T) {
	c := testConfig(t)
	data, _ := json.Marshal(c)
	for _, input := range []string{"null", "{}", `{"schemaVersion":2}`, `{"schemaVersion":null}`, strings.Replace(string(data), `"enabled":true,`, "", 1), strings.Replace(string(data), `"schemaVersion":1`, `"schemaVersion":0`, 1), strings.Replace(string(data), `"timezone":"America/Chicago"`, `"timezone":"Local"`, 1), string(data) + ` {}`, strings.Replace(string(data), `"resetTime":"10:00"`, `"resetTime":"10:00","secret":"no"`, 1)} {
		path := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(path, []byte(input), 0600)
		if _, err := Load(path); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}
func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) { defer wg.Done(); errs <- WriteJSON(path, map[string]int{"value": n}) }(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]int
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("temporary files retained")
	}
}
func TestUnsafeRoots(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{home, filepath.VolumeName(home) + string(os.PathSeparator)} {
		t.Setenv("AGENTWARMUP_HOME", root)
		if _, err := DefaultPaths(); err == nil {
			t.Fatalf("accepted root %s", root)
		}
	}
	t.Setenv("AGENTWARMUP_HOME", filepath.Join(t.TempDir(), "space & unicode-\u00e9"))
	p, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(p.Root) || p.Lock("lifecycle") == p.Lock("codex") {
		t.Fatal("invalid paths")
	}
}
func TestMissingLoadDoesNotCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "config.json")
	if _, err := Load(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("load created directory")
	}
}

func TestDuplicateJSONFieldsRejected(t *testing.T) {
	c := testConfig(t)
	data, _ := json.Marshal(c)
	for _, input := range []string{strings.Replace(string(data), `"schemaVersion":1`, `"schemaVersion":2,"schemaVersion":1`, 1), strings.Replace(string(data), `"id":"codex"`, `"id":"claude","id":"codex"`, 1)} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("duplicate JSON field accepted")
		}
	}
}
func TestMigrationCanonicalizesLegacyWeekends(t *testing.T) {
	c, err := Migrate(LegacyConfig{ResetTime: "10:00", Days: []string{"Sat", "Sun"}, CodexPath: filepath.Join(t.TempDir(), "codex.exe")}, "UTC", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.ResetDays, ",") != "Sun,Sat" {
		t.Fatal(c.ResetDays)
	}
}
