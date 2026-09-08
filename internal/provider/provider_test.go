package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// All subprocess tests execute this Go test binary. None detects or invokes an
// installed provider, reads credentials, or creates a real schedule.
func helperInstallation(t *testing.T, id, scenario string) Installation {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	version := "0.153.0"
	if id == "claude" {
		version = "2.1.169"
	}
	return Installation{ID: id, Executable: exe, PrefixArgs: []string{"-test.run=^TestProviderHelper$", "--", scenario}, ConfigRoot: t.TempDir(), AuthStore: "file", Version: version}
}
func TestProviderHelper(t *testing.T) {
	index := -1
	for n, a := range os.Args {
		if a == "--" {
			index = n
			break
		}
	}
	if index < 0 {
		return
	}
	args := os.Args[index+1:]
	scenario := args[0]
	args = args[1:]
	if scenario == "sleep" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if scenario == "large" {
		fmt.Print(strings.Repeat("x", probeOutputLimit+1))
		os.Exit(0)
	}
	if data, err := io.ReadAll(os.Stdin); err != nil || len(data) != 0 {
		os.Exit(20)
	}
	if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
		os.Exit(21)
	}
	if len(args) == 1 && args[0] == "--version" {
		if scenario == "claude" {
			fmt.Println("2.1.169 (Claude Code)")
		} else if scenario == "old" {
			fmt.Println("codex-cli 0.152.0")
		} else {
			fmt.Println("codex-cli 0.153.0")
		}
		os.Exit(0)
	}
	if args[len(args)-1] == "--help" {
		if scenario == "bad-help" {
			fmt.Println("--model")
			os.Exit(0)
		}
		fmt.Println("--ignore-user-config --ephemeral --json --sandbox --skip-git-repo-check --safe-mode --setting-sources --settings --strict-mcp-config --mcp-config --tools --no-session-persistence --output-format --effort")
		os.Exit(0)
	}
	if scenario == "codex" {
		fmt.Fprintln(os.Stderr, "Logged in using ChatGPT")
		os.Exit(0)
	}
	if scenario == "claude" {
		fmt.Println(`{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro","email":"unused@example.invalid"}`)
		os.Exit(0)
	}
	os.Exit(3)
}

func TestProbeUsesIsolatedDirectoryClosedInputAndSanitizedEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fake-token")
	for _, id := range IDs() {
		t.Run(id, func(t *testing.T) {
			i := helperInstallation(t, id, id)
			got, err := CheckAuth(context.Background(), i)
			if err != nil || got.Kind != "subscription" {
				t.Fatalf("got %+v, %v", got, err)
			}
			if strings.Contains(got.Detail, "unused") {
				t.Fatal("account identifier escaped")
			}
		})
	}
}
func TestVerifyChecksCurrentVersionAndCapabilities(t *testing.T) {
	for _, tc := range []struct {
		scenario string
		ok       bool
	}{{"codex", true}, {"old", false}, {"bad-help", false}} {
		t.Run(tc.scenario, func(t *testing.T) {
			i := helperInstallation(t, "codex", tc.scenario)
			got, err := Verify(context.Background(), i)
			if (err == nil) != tc.ok {
				t.Fatalf("%+v %v", got, err)
			}
			if got.ConfigRoot != i.ConfigRoot || got.AuthStore != i.AuthStore {
				t.Fatal("auth metadata changed")
			}
		})
	}
}
func TestProbeBoundsTimeAndOutput(t *testing.T) {
	for _, scenario := range []string{"sleep", "large"} {
		t.Run(scenario, func(t *testing.T) {
			i := helperInstallation(t, "codex", scenario)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			start := time.Now()
			_, _, _, err := probe(ctx, i, []string{"--version"})
			if err == nil {
				t.Fatal("expected bounded probe failure")
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("probe failed to terminate promptly")
			}
		})
	}
}
func TestReleaseProviderSelection(t *testing.T) {
	original := ReleaseProviders
	t.Cleanup(func() { ReleaseProviders = original })
	for _, tc := range []struct {
		list string
		want []string
	}{
		{"codex,claude", []string{"codex", "claude"}},
		{"codex", []string{"codex"}},
		{" claude,unknown,claude,codex ", []string{"claude", "codex"}},
		{"", []string{}},
		{"unknown", []string{}},
	} {
		ReleaseProviders = tc.list
		if got := IDs(); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("IDs(%q) = %v, want %v", tc.list, got, tc.want)
		}
		if got := AllIDs(); !reflect.DeepEqual(got, []string{"codex", "claude"}) {
			t.Fatalf("teardown providers changed: %v", got)
		}
	}
}

func TestRequestRejectsProviderExcludedFromRelease(t *testing.T) {
	original := ReleaseProviders
	t.Cleanup(func() { ReleaseProviders = original })
	ReleaseProviders = "codex"
	i := helperInstallation(t, "claude", "claude")
	if _, err := Request(i, t.TempDir()); err == nil || !strings.Contains(err.Error(), "not included in this release") {
		t.Fatalf("excluded Claude request was not rejected: %v", err)
	}
	status, err := CheckAuth(context.Background(), i)
	if err != nil || status.Kind != "unsupported" {
		t.Fatalf("excluded Claude auth status: %+v, %v", status, err)
	}
}

func TestEnvironmentAllowlistAndCaseInsensitiveOverrides(t *testing.T) {
	input := []string{"Path=bin", "OPENAI_API_KEY=secret", "openai_api_key=secret", "ANTHROPIC_BASE_URL=https://wrong.invalid", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_OAUTH_TOKEN=secret", "CODEX_HOME=original", "HTTPS_PROXY=http://proxy.invalid", "NODE_EXTRA_CA_CERTS=cert.pem", "USERPROFILE=user", "HOME=home", "NODE_OPTIONS=--require injected.js", "OTEL_EXPORTER_OTLP_ENDPOINT=https://wrong.invalid"}
	got := sanitizeEnvironment(input)
	want := []string{"Path=bin", "HTTPS_PROXY=http://proxy.invalid", "NODE_EXTRA_CA_CERTS=cert.pem", "USERPROFILE=user", "HOME=home"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	if !reflect.DeepEqual(input[0:2], []string{"Path=bin", "OPENAI_API_KEY=secret"}) {
		t.Fatal("input environment mutated")
	}
	root := t.TempDir()
	got = withConfigRoot([]string{"codex_home=old", "PATH=bin"}, Installation{ID: "codex", ConfigRoot: root})
	if len(got) != 2 || got[1] != "CODEX_HOME="+root {
		t.Fatalf("%q", got)
	}
}
func TestRequestArguments(t *testing.T) {
	for _, id := range IDs() {
		t.Run(id, func(t *testing.T) {
			i := helperInstallation(t, id, id)
			dir := t.TempDir()
			inv, err := Request(i, dir)
			if err != nil {
				t.Fatal(err)
			}
			if inv.Executable != i.Executable || inv.Dir != dir || !reflect.DeepEqual(inv.Args[:len(i.PrefixArgs)], i.PrefixArgs) {
				t.Fatal("launcher or directory lost")
			}
			args := inv.Args[len(i.PrefixArgs):]
			if args[len(args)-1] != Prompt {
				t.Fatal("unexpected provider prompt")
			}
			for _, forbidden := range []string{"--bare", "--dangerously-skip-permissions", "--dangerously-bypass-approvals-and-sandbox", "--fallback-model", "--resume", "--continue"} {
				for _, a := range args {
					if a == forbidden {
						t.Fatalf("forbidden flag %s", a)
					}
				}
			}
			if id == "codex" {
				for _, required := range []string{"--ignore-user-config", "--ephemeral", "read-only", CodexModel, "project_doc_max_bytes=0", `model_reasoning_effort="low"`, `cli_auth_credentials_store="file"`, `approval_policy="never"`} {
					if !contains(args, required) {
						t.Fatalf("missing %s", required)
					}
				}
			} else {
				if !contains(args, "--safe-mode") || !contains(args, ClaudeModel) {
					t.Fatal("missing Claude isolation/model")
				}
				for n, a := range args {
					if a == "--tools" && args[n+1] != "" {
						t.Fatal("tools not disabled")
					}
				}
			}
		})
	}
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func TestRejectUnsupportedInstallation(t *testing.T) {
	i := helperInstallation(t, "claude", "claude")
	i.Version = "2.1.92"
	if _, err := Request(i, t.TempDir()); err == nil {
		t.Fatal("old CLI accepted")
	}
	auth, err := CheckAuth(context.Background(), i)
	if err != nil || auth.Kind != "unsupported" {
		t.Fatalf("%+v %v", auth, err)
	}
	i.ID = "other"
	if _, err := Request(i, t.TempDir()); err == nil {
		t.Fatal("unknown provider accepted")
	}
}
func TestCodexAuthStoreOnlyReadsConfiguration(t *testing.T) {
	for _, tc := range []struct {
		content, want string
		bad           bool
	}{{"", "file", false}, {"cli_auth_credentials_store = 'keyring' # comment\n[other]\ncli_auth_credentials_store='file'", "keyring", false}, {"cli_auth_credentials_store=\"auto\"", "auto", false}, {"cli_auth_credentials_store='unsupported'", "", true}, {"cli_auth_credentials_store='file'\ncli_auth_credentials_store='auto'", "", true}} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := codexAuthStore(root)
		if (err != nil) != tc.bad || got != tc.want {
			t.Fatalf("%q: %q %v", tc.content, got, err)
		}
	}
}

func TestAuthClassification(t *testing.T) {
	for _, tc := range []struct {
		code           int
		out, err, kind string
	}{{0, "", "Logged in using ChatGPT", "subscription"}, {0, "Logged in using an API key", "", "api"}, {1, "", "Not logged in", "signed_out"}, {0, "not sure about auth", "", "unknown"}, {1, "Logged in using ChatGPT", "", "unknown"}} {
		got := parseCodexAuth(tc.code, tc.out, tc.err)
		if got.Kind != tc.kind {
			t.Fatalf("%+v -> %+v", tc, got)
		}
	}
	for _, tc := range []struct{ out, kind string }{{`{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro"}`, "subscription"}, {`{"loggedIn":false}`, "signed_out"}, {`{"loggedIn":true,"authMethod":"api_key"}`, "api"}, {`{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro","apiProvider":"bedrock"}`, "api"}, {`{"loggedIn":true,"authMethod":"claude.ai"}`, "unknown"}, {`{}`, "unknown"}, {`bad json`, "unknown"}} {
		got := parseClaudeAuth(0, tc.out)
		if got.Kind != tc.kind {
			t.Fatalf("%s -> %+v", tc.out, got)
		}
	}
}

func TestResultClassification(t *testing.T) {
	ok := `{"type":"item.completed","item":{"type":"agent_message","text":"OK"}}` + "\n" + `{"type":"turn.completed"}`
	for _, tc := range []struct {
		name           string
		code           int
		out, err, want string
	}{
		{"codex OK", 0, ok, "", "succeeded"},
		{"claude OK", 0, `{"type":"result","subtype":"success","is_error":false,"result":"OK","num_turns":1,"modelUsage":{"claude-sonnet-4-6":{}}}`, "", "succeeded"},
		{"not JSON", 0, "OK", "", "failed"},
		{"nonzero", 1, ok, "", "failed"},
		{"incomplete", 0, `{"type":"item.completed","item":{"type":"agent_message","text":"OK"}}`, "", "failed"},
		{"wrong answer", 0, strings.Replace(ok, `"OK"`, `"Fine"`, 1), "", "failed"},
		{"tool call", 0, `{"type":"item.completed","item":{"type":"command_execution"}}` + "\n" + ok, "", "failed"},
		{"structured error exit zero", 0, `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"authentication_error"}`, "", "auth_required"},
		{"rate limited", 1, `{"type":"error","message":"rate_limit_exceeded"}`, "", "rate_limited"},
		{"model unavailable", 1, `{"type":"turn.failed","error":{"code":"model_not_found"}}`, "", "model_unavailable"},
		{"auth unrelated", 1, "", "author field missing", "failed"},
		{"invalid JSON suffix", 0, ok + "\n{", "", "failed"},
		{"fallback model", 0, `{"type":"result","subtype":"success","result":"OK","modelUsage":{"other-model":{}}}`, "", "failed"},
		{"multiple turns", 0, `{"type":"result","subtype":"success","result":"OK","num_turns":2}`, "", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.code, tc.out, tc.err)
			if got.Outcome != tc.want {
				t.Fatalf("%+v, want %s", got, tc.want)
			}
		})
	}
}
func TestScrub(t *testing.T) {
	for _, secret := range []string{"sk-abcdefgh123456", "Bearer secret-value", "eyJabcdef.eyJdefgh.signature", "person@example.invalid", `access_token="opaque-secret"`, `account_id: account-secret`} {
		got := Scrub("Error " + secret + "\n done")
		if !strings.Contains(got, "[redacted]") {
			t.Fatalf("not scrubbed: %q", got)
		}
	}
	got := Scrub(strings.Repeat("界", 250))
	if len([]rune(got)) != 200 {
		t.Fatalf("rune limit %d", len([]rune(got)))
	}
}
