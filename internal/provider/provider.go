// Package provider describes subscription CLI invocations. It never sends a
// model request itself; the guarded runner owns request execution.
package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	CodexModel = "gpt-6-astra"
	// ClaudeModel is a fixed candidate, not a verified allowance choice.
	ClaudeModel      = "claude-sonnet-4-6"
	Prompt           = "Reply only with OK. Do not use tools."
	probeTimeout     = 15 * time.Second
	probeOutputLimit = 128 * 1024
)

type Installation struct {
	ID, Executable                 string
	PrefixArgs                     []string
	ConfigRoot, AuthStore, Version string
}
type AuthStatus struct{ Kind, Detail string }
type Invocation struct {
	Executable string
	Args, Env  []string
	Dir        string
}
type Result struct{ Outcome, Detail string }

// ReleaseProviders is set through -ldflags -X for stable releases so only
// independently validated providers are exposed. Development builds include both.
var ReleaseProviders = "codex,claude"

// AllIDs includes adapters excluded from this build so uninstall can still
// coordinate with state and locks created by an earlier installation.
func AllIDs() []string { return []string{"codex", "claude"} }

func IDs() []string {
	ids := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, id := range strings.Split(ReleaseProviders, ",") {
		id = strings.TrimSpace(id)
		if known(id) && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids
}

func included(id string) bool {
	for _, candidate := range IDs() {
		if id == candidate {
			return true
		}
	}
	return false
}

func known(id string) bool { return id == "codex" || id == "claude" }

// Detect resolves a native executable or a known npm launcher and verifies
// non-generative CLI capabilities. It neither logs in nor installs anything.
func Detect(ctx context.Context, id string) (Installation, error) {
	if !known(id) {
		return Installation{}, errors.New("unknown provider")
	}
	paths := installationCandidates(id)
	var last error
	for _, path := range paths {
		inst, err := resolveLauncher(id, path)
		if err != nil {
			last = err
			continue
		}
		inst.ConfigRoot, err = configRoot(id)
		if err != nil {
			return inst, err
		}
		if id == "codex" {
			inst.AuthStore, err = codexAuthStore(inst.ConfigRoot)
			if err != nil {
				return inst, err
			}
		}
		version, _, code, err := probe(ctx, inst, []string{"--version"})
		if err != nil || code != 0 {
			last = fmt.Errorf("%s version check failed", id)
			continue
		}
		inst.Version = parseVersion(version)
		if err := supportedVersion(inst); err != nil {
			return inst, err
		}
		helpArgs := []string{"--help"}
		if id == "codex" {
			helpArgs = []string{"exec", "--help"}
		}
		help, _, code, err := probe(ctx, inst, helpArgs)
		if err != nil || code != 0 {
			return inst, fmt.Errorf("%s capability check failed", id)
		}
		if err := checkCapabilities(id, help); err != nil {
			return inst, err
		}
		return inst, nil
	}
	if last != nil {
		return Installation{}, last
	}
	return Installation{}, fmt.Errorf("%s executable not found; install its official CLI first", id)
}

func installationCandidates(id string) []string {
	var paths []string
	if p, err := exec.LookPath(id); err == nil {
		paths = append(paths, p)
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath(id + ".exe"); err == nil {
			paths = append([]string{p}, paths...)
		}
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		name := id
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		paths = append(paths, filepath.Join(home, ".local", "bin", name))
	}
	if runtime.GOOS == "windows" {
		if p := os.Getenv("LOCALAPPDATA"); p != "" {
			paths = append(paths, filepath.Join(p, "Programs", id, id+".exe"))
		}
		if p := os.Getenv("APPDATA"); p != "" {
			paths = append(paths, filepath.Join(p, "npm", id+".cmd"))
		}
	} else {
		paths = append(paths, "/opt/homebrew/bin/"+id, "/usr/local/bin/"+id)
	}
	return paths
}

func resolveLauncher(id, path string) (Installation, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Installation{}, fmt.Errorf("%s executable path is invalid", id)
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return Installation{}, fmt.Errorf("%s executable not found", id)
	}
	i := Installation{ID: id, Executable: abs}
	ext := strings.ToLower(filepath.Ext(abs))
	if runtime.GOOS != "windows" || (ext != ".cmd" && ext != ".bat" && ext != ".ps1") {
		return i, nil
	}
	// Do not run a command string through cmd.exe. Support only the known npm
	// package entrypoints, represented as node plus a separate script argument.
	var entry string
	if id == "codex" {
		entry = filepath.Join(filepath.Dir(abs), "node_modules", "@openai", "codex", "bin", "codex.js")
	} else {
		entry = filepath.Join(filepath.Dir(abs), "node_modules", "@anthropic-ai", "claude-code", "cli.js")
	}
	if st, e := os.Stat(entry); e != nil || st.IsDir() {
		return i, fmt.Errorf("%s launcher is unsupported; use its native CLI installation", id)
	}
	node := filepath.Join(filepath.Dir(abs), "node.exe")
	if st, e := os.Stat(node); e != nil || st.IsDir() {
		node, err = exec.LookPath("node.exe")
	}
	if err != nil {
		return i, fmt.Errorf("%s npm launcher needs Node.js; use its native CLI installation", id)
	}
	i.Executable, _ = filepath.Abs(node)
	i.PrefixArgs = []string{entry}
	return i, nil
}

var versionRE = regexp.MustCompile(`(?:^|\s)([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)

func parseVersion(s string) string {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) == 2 {
		return m[1]
	}
	return ""
}
func atLeast(s, minimum string) bool {
	a, b := strings.Split(s, "."), strings.Split(minimum, ".")
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	for n := range a {
		av, e := strconv.Atoi(a[n])
		if e != nil || av < 0 {
			return false
		}
		bv, _ := strconv.Atoi(b[n])
		if av != bv {
			return av > bv
		}
	}
	return true
}
func supportedVersion(i Installation) error {
	if !known(i.ID) {
		return errors.New("unknown provider")
	}
	if !included(i.ID) {
		return fmt.Errorf("%s is not included in this release", i.ID)
	}
	minimum := "0.153.0"
	if i.ID == "claude" {
		minimum = "2.1.169"
	}
	if !atLeast(i.Version, minimum) {
		return fmt.Errorf("%s CLI %s or later is required for request isolation; update the provider CLI deliberately", i.ID, minimum)
	}
	return nil
}
func checkCapabilities(id, help string) error {
	flags := []string{"--ignore-user-config", "--ephemeral", "--json", "--sandbox", "--skip-git-repo-check"}
	if id == "claude" {
		flags = []string{"--safe-mode", "--setting-sources", "--settings", "--strict-mcp-config", "--mcp-config", "--tools", "--no-session-persistence", "--output-format", "--effort"}
	}
	for _, flag := range flags {
		if !strings.Contains(help, flag) {
			return fmt.Errorf("%s CLI is missing required isolation flag %s", id, flag)
		}
	}
	return nil
}

// Verify checks the executable again after CLI updates without rediscovering
// or changing the saved credential location. It does not read authentication.
func Verify(ctx context.Context, i Installation) (Installation, error) {
	if !known(i.ID) || !filepath.IsAbs(i.Executable) {
		return i, errors.New("invalid provider installation")
	}
	if i.AuthStore != "" && i.AuthStore != "file" && i.AuthStore != "keyring" && i.AuthStore != "auto" {
		return i, errors.New("unsupported authentication store")
	}
	if i.ConfigRoot != "" && !filepath.IsAbs(i.ConfigRoot) {
		return i, errors.New("provider configuration directory must be absolute")
	}
	version, _, code, err := probe(ctx, i, []string{"--version"})
	if err != nil {
		return i, err
	}
	if code != 0 {
		return i, fmt.Errorf("%s version check failed", i.ID)
	}
	i.Version = parseVersion(version)
	if err := supportedVersion(i); err != nil {
		return i, err
	}
	args := []string{"--help"}
	if i.ID == "codex" {
		args = []string{"exec", "--help"}
	}
	help, _, code, err := probe(ctx, i, args)
	if err != nil {
		return i, err
	}
	if code != 0 {
		return i, fmt.Errorf("%s capability check failed", i.ID)
	}
	return i, checkCapabilities(i.ID, help)
}

// Login is the sole interactive operation. The caller must first offer the
// provider's normal login flow to the user. This function is never used in tests.
func Login(ctx context.Context, i Installation) error {
	if !known(i.ID) {
		return errors.New("unknown provider")
	}
	args := []string{"login"}
	if i.ID == "claude" {
		args = []string{"auth", "login"}
	}
	if i.ID == "codex" && i.AuthStore != "" {
		args = append([]string{"-c", `cli_auth_credentials_store="` + i.AuthStore + `"`}, args...)
	}
	cmd := exec.CommandContext(ctx, i.Executable, append(append([]string{}, i.PrefixArgs...), args...)...)
	cmd.Env = withConfigRoot(os.Environ(), i)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s login did not complete", i.ID)
	}
	return nil
}

func CheckAuth(ctx context.Context, i Installation) (AuthStatus, error) {
	if err := supportedVersion(i); err != nil {
		return AuthStatus{"unsupported", err.Error()}, nil
	}
	var args []string
	if i.ID == "codex" {
		args = append(codexAuthArgs(i), "login", "status")
	} else {
		args = append(claudeIsolationArgs(), "auth", "status", "--json")
	}
	out, stderr, code, err := probe(ctx, i, args)
	if err != nil {
		return AuthStatus{"unknown", i.ID + " authentication check failed"}, err
	}
	if i.ID == "codex" {
		return parseCodexAuth(code, out, stderr), nil
	}
	return parseClaudeAuth(code, out), nil
}

// Request only builds an invocation. Its model and isolation strategy remain
// candidates until docs/validation.md records the live release checks.
func Request(i Installation, runDir string) (Invocation, error) {
	if err := supportedVersion(i); err != nil {
		return Invocation{}, err
	}
	if !filepath.IsAbs(i.Executable) || !filepath.IsAbs(runDir) {
		return Invocation{}, errors.New("provider executable and run directory must be absolute")
	}
	if i.AuthStore != "" && i.AuthStore != "file" && i.AuthStore != "keyring" && i.AuthStore != "auto" {
		return Invocation{}, errors.New("unsupported authentication store")
	}
	args := []string{}
	if i.ID == "codex" {
		args = codexRequestArgs(i)
	} else {
		args = append(claudeIsolationArgs(), "--no-session-persistence", "--output-format", "json", "--model", ClaudeModel, "--effort", "low", "--max-turns", "1", "-p", Prompt)
	}
	return Invocation{i.Executable, append(append([]string{}, i.PrefixArgs...), args...), environment(i), runDir}, nil
}

func probe(ctx context.Context, i Installation, args []string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "agentwarmup-provider-probe-")
	if err != nil {
		return "", "", -1, errors.New("cannot create an isolated provider probe directory")
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, i.Executable, append(append([]string{}, i.PrefixArgs...), args...)...)
	cmd.Dir = dir
	cmd.Env = environment(i)
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	hideWindow(cmd)
	var out, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return "", "", -1, errors.New("provider check timed out or was cancelled")
	}
	if out.overflow || stderr.overflow {
		return "", "", -1, errors.New("provider check output exceeded the safe limit")
	}
	if err != nil {
		var e *exec.ExitError
		if !errors.As(err, &e) {
			return "", "", -1, errors.New("provider check could not start or complete")
		}
		return out.String(), stderr.String(), e.ExitCode(), nil
	}
	return out.String(), stderr.String(), 0, nil
}

type limitedBuffer struct {
	data     []byte
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := probeOutputLimit - len(b.data)
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *limitedBuffer) String() string { return string(b.data) }
