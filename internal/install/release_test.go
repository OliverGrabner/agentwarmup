package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Substitution must affect only the assignment. Replacing a sentinel comparison
// too would make a release-pinned installer resolve a newer release again.
func TestReleasePinnedInstallers(t *testing.T) {
	for _, name := range []string{"install.sh", "install.ps1"} {
		data, e := os.ReadFile(filepath.Join("..", "..", name))
		if e != nil {
			t.Fatal(e)
		}
		s := string(data)
		if strings.Count(s, "@RELEASE_TAG@") != 1 {
			t.Fatalf("%s must have one assignment token, never a comparison token", name)
		}
		pinned := strings.ReplaceAll(s, "@RELEASE_TAG@", "v1.2.3-rc.1")
		if strings.Contains(pinned, "@RELEASE_TAG@") {
			t.Fatal("unresolved release token")
		}
		if name == "install.sh" {
			if !strings.Contains(pinned, `if [ "${release_tag#@}" != "$release_tag" ]; then`) {
				t.Fatal("shell unresolved-tag test must be independent of replacement tag")
			}
		} else if !strings.Contains(pinned, `if ($Version.StartsWith('@'))`) {
			t.Fatal("PowerShell unresolved-tag test must be independent of replacement tag")
		}
	}
}
func TestReleaseManifestTargetsAndGates(t *testing.T) {
	data, e := os.ReadFile(filepath.Join("..", "..", ".github", "release.json"))
	if e != nil {
		t.Fatal(e)
	}
	var m struct {
		Repository   string `json:"repository"`
		ReleaseReady bool   `json:"releaseReady"`
		Targets      []struct{ OS, Arch, Extension string }
		Providers    map[string]struct {
			Validation, MinimumCLIVersion string
			ValidatedModel                *string
		}
	}
	if e = json.Unmarshal(data, &m); e != nil {
		t.Fatal(e)
	}
	if m.Repository != "OliverGrabner/agentwarmup" || len(m.Targets) != 6 {
		t.Fatal("wrong repository or target set")
	}
	seen := map[string]bool{}
	for _, target := range m.Targets {
		k := target.OS + "/" + target.Arch
		if seen[k] {
			t.Fatal("duplicate target")
		}
		seen[k] = true
		if target.Arch != "amd64" && target.Arch != "arm64" {
			t.Fatal("unsupported arch")
		}
		if target.OS == "windows" && target.Extension != "zip" {
			t.Fatal("Windows must use ZIP")
		}
		if target.OS != "windows" && target.Extension != "tar.gz" {
			t.Fatal("POSIX must use tar.gz")
		}
	}
	eligible := 0
	for _, id := range []string{"codex", "claude"} {
		p, ok := m.Providers[id]
		if !ok || p.MinimumCLIVersion == "" {
			t.Fatalf("missing version requirement for %s", id)
		}
		if p.Validation == "passed" && p.ValidatedModel != nil {
			eligible++
		}
	}
	if m.ReleaseReady && eligible == 0 {
		t.Fatal("stable readiness requires at least one validated provider")
	}
	script, e := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "release.ps1"))
	if e != nil {
		t.Fatal(e)
	}
	for _, required := range []string{`$validated.Count -eq 0`, `$includedProviders = @($validated | ForEach-Object { $_.Name } | Sort-Object)`, `internal/provider.ReleaseProviders=$($includedProviders -join ',')`, `-NotePropertyName includedProviders`} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("stable gate must compile only validated providers: missing %s", required)
		}
	}
}
