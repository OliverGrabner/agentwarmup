package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/OliverGrabner/agentwarmup/internal/schedule"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type ProviderConfig struct {
	Version    string   `json:"version,omitempty"`
	ID         string   `json:"id"`
	Executable string   `json:"executable"`
	PrefixArgs []string `json:"prefixArgs,omitempty"`
	ConfigRoot string   `json:"configRoot,omitempty"`
	AuthStore  string   `json:"authStore,omitempty"`
}
type Config struct {
	SchemaVersion int              `json:"schemaVersion"`
	ResetTime     string           `json:"resetTime"`
	ResetDays     []string         `json:"resetDays"`
	Timezone      string           `json:"timezone"`
	Enabled       bool             `json:"enabled"`
	Providers     []ProviderConfig `json:"providers"`
	EffectiveFrom time.Time        `json:"effectiveFrom"`
}
type LegacyConfig struct {
	ResetTime string   `json:"resetTime"`
	Days      []string `json:"days"`
	Enabled   bool     `json:"enabled"`
	CatchUp   bool     `json:"catchUp"`
	CodexPath string   `json:"codexPath"`
}
type Document struct {
	Current *Config
	Legacy  *LegacyConfig
}

func (c Config) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("unsupported configuration schema %d", c.SchemaVersion)
	}
	if _, err := schedule.New(c.ResetTime, c.ResetDays, c.Timezone); err != nil {
		return err
	}
	canonical, _ := schedule.ParseDays(strings.Join(c.ResetDays, ","))
	if strings.Join(canonical, ",") != strings.Join(c.ResetDays, ",") {
		return fmt.Errorf("reset days must be in canonical Sunday-to-Saturday order")
	}
	if c.EffectiveFrom.IsZero() {
		return fmt.Errorf("missing activation time")
	}
	_, offset := c.EffectiveFrom.Zone()
	if offset != 0 {
		return fmt.Errorf("activation time must be UTC")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("no selected providers")
	}
	seen := map[string]bool{}
	for _, p := range c.Providers {
		if (p.ID != "codex" && p.ID != "claude") || seen[p.ID] {
			return fmt.Errorf("unknown or duplicate provider %q", p.ID)
		}
		seen[p.ID] = true
		if !filepath.IsAbs(p.Executable) {
			return fmt.Errorf("provider %s executable must be absolute", p.ID)
		}
		if p.ConfigRoot != "" && !filepath.IsAbs(p.ConfigRoot) {
			return fmt.Errorf("provider config root must be absolute")
		}
	}
	return nil
}
func decode(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected trailing configuration data")
	}
	return nil
}
func Load(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	if err := uniqueJSONFields(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return Document{}, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return Document{}, err
	}
	if fields == nil {
		return Document{}, fmt.Errorf("configuration must be an object")
	}
	if raw, ok := fields["schemaVersion"]; ok {
		var version int
		if string(raw) == "null" {
			return Document{}, fmt.Errorf("null schema version")
		}
		if err = json.Unmarshal(raw, &version); err != nil {
			return Document{}, err
		}
		if version != 1 {
			return Document{}, fmt.Errorf("unsupported configuration schema %d", version)
		}
		for _, key := range []string{"schemaVersion", "resetTime", "resetDays", "timezone", "enabled", "providers", "effectiveFrom"} {
			if b, ok := fields[key]; !ok || string(b) == "null" {
				return Document{}, fmt.Errorf("missing configuration field %s", key)
			}
		}
		var c Config
		if err = decode(data, &c); err != nil {
			return Document{}, err
		}
		if err = c.Validate(); err != nil {
			return Document{}, err
		}
		return Document{Current: &c}, nil
	}
	for _, key := range []string{"resetTime", "days", "enabled", "catchUp", "codexPath"} {
		if b, ok := fields[key]; !ok || string(b) == "null" {
			return Document{}, fmt.Errorf("missing legacy configuration field %s", key)
		}
	}
	var old LegacyConfig
	if err = decode(data, &old); err != nil {
		return Document{}, err
	}
	if _, err = schedule.New(old.ResetTime, old.Days, "UTC"); err != nil {
		return Document{}, err
	}
	if !filepath.IsAbs(old.CodexPath) {
		return Document{}, fmt.Errorf("legacy Codex path must be absolute")
	}
	return Document{Legacy: &old}, nil
}
func Migrate(old LegacyConfig, zone string, effective time.Time) (Config, error) {
	days, err := schedule.ParseDays(strings.Join(old.Days, ","))
	if err != nil {
		return Config{}, err
	}
	c := Config{SchemaVersion: 1, ResetTime: old.ResetTime, ResetDays: days, Timezone: zone, Enabled: old.Enabled, Providers: []ProviderConfig{{ID: "codex", Executable: old.CodexPath}}, EffectiveFrom: effective.UTC()}
	return c, c.Validate()
}

// Duplicate keys make security-relevant intent ambiguous even in valid JSON.
func uniqueJSONFields(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("invalid JSON object key")
			}
			if seen[name] {
				return fmt.Errorf("duplicate configuration field %q", name)
			}
			seen[name] = true
			if err := uniqueJSONFields(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSONFields(d); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, err = d.Token()
	return err
}
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return WriteJSON(path, c)
}

// WriteJSON durably stages a unique same-directory file before atomic replacement.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".agentwarmup-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replaceFile(tmp, path)
}

type Paths struct{ Root string }

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	var root string
	if override := os.Getenv("AGENTWARMUP_HOME"); override != "" {
		root = override
	} else {
		switch runtime.GOOS {
		case "windows":
			base := os.Getenv("LOCALAPPDATA")
			if base == "" {
				return Paths{}, fmt.Errorf("LOCALAPPDATA is missing")
			}
			root = filepath.Join(base, "AgentWarmup")
		case "darwin":
			root = filepath.Join(home, "Library", "Application Support", "AgentWarmup")
		default:
			base := os.Getenv("XDG_CONFIG_HOME")
			if base == "" {
				base = filepath.Join(home, ".config")
			}
			if !filepath.IsAbs(base) {
				return Paths{}, fmt.Errorf("XDG_CONFIG_HOME must be absolute")
			}
			root = filepath.Join(base, "agentwarmup")
		}
	}
	return ResolvePaths(root)
}

func ResolvePaths(root string) (Paths, error) {
	if strings.TrimSpace(root) == "" {
		return Paths{}, fmt.Errorf("application root must not be empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, err
	}
	absolute = filepath.Clean(absolute)
	// Resolve existing ancestors so a symlink to the home or filesystem root is rejected.
	resolved := absolute
	var missing []string
	for {
		p, e := filepath.EvalSymlinks(resolved)
		if e == nil {
			resolved = p
			break
		}
		if !os.IsNotExist(e) {
			return Paths{}, e
		}
		parent := filepath.Dir(resolved)
		if parent == resolved {
			return Paths{}, fmt.Errorf("cannot resolve application root")
		}
		missing = append(missing, filepath.Base(resolved))
		resolved = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, missing[i])
	}
	resolvedHome, e := filepath.EvalSymlinks(home)
	if e != nil {
		resolvedHome = home
	}
	equal := func(a, b string) bool {
		if runtime.GOOS == "windows" {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	if filepath.Dir(resolved) == resolved || equal(resolved, filepath.Clean(resolvedHome)) {
		return Paths{}, fmt.Errorf("unsafe application root %q", absolute)
	}
	return Paths{Root: resolved}, nil
}
func (p Paths) Config() string { return filepath.Join(p.Root, "config.json") }
func (p Paths) Bin() string {
	name := "agentwarmup"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(p.Root, "bin", name)
}
func providerFile(id string) string {
	if id != "codex" && id != "claude" {
		panic("invalid provider path ID")
	}
	return id
}
func (p Paths) State(id string) string { return filepath.Join(p.Root, providerFile(id)+"-state.json") }
func (p Paths) Lock(id string) string {
	if id != "lifecycle" {
		id = providerFile(id)
	}
	return filepath.Join(p.Root, "locks", id+".lock")
}
func (p Paths) RunDir(id string) string { return filepath.Join(p.Root, "run", providerFile(id)) }
