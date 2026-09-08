package provider

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Use a small environment allowlist: preserve the login location and supported
// network trust/proxy configuration, never inherit billing or provider controls.
func sanitizeEnvironment(environ []string) []string {
	allowed := map[string]bool{}
	for _, key := range strings.Fields("PATH PATHEXT SYSTEMROOT WINDIR COMSPEC USERPROFILE HOME HOMEDRIVE HOMEPATH APPDATA LOCALAPPDATA XDG_CONFIG_HOME XDG_DATA_HOME XDG_CACHE_HOME XDG_RUNTIME_DIR TMP TEMP TMPDIR LANG LANGUAGE LC_ALL LC_CTYPE TERM NO_COLOR HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY SSL_CERT_FILE SSL_CERT_DIR REQUESTS_CA_BUNDLE CURL_CA_BUNDLE NODE_EXTRA_CA_CERTS NODE_USE_SYSTEM_CA DBUS_SESSION_BUS_ADDRESS DISPLAY WAYLAND_DISPLAY") {
		allowed[key] = true
	}
	out := make([]string, 0, len(environ))
	seen := map[string]bool{}
	for _, kv := range environ {
		key, _, ok := strings.Cut(kv, "=")
		canonical := strings.ToUpper(key)
		if ok && allowed[canonical] && !seen[canonical] {
			out = append(out, kv)
			seen[canonical] = true
		}
	}
	return out
}
func withConfigRoot(env []string, i Installation) []string {
	if i.ConfigRoot == "" {
		return env
	}
	key := "CODEX_HOME"
	if i.ID == "claude" {
		key = "CLAUDE_CONFIG_DIR"
	}
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		k, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(k, key) {
			out = append(out, entry)
		}
	}
	return append(out, key+"="+i.ConfigRoot)
}
func environment(i Installation) []string {
	env := withConfigRoot(sanitizeEnvironment(os.Environ()), i)
	if i.ID == "claude" {
		env = append(env, "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1", "CLAUDE_CODE_SKIP_PROMPT_HISTORY=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1")
	}
	return env
}
func configRoot(id string) (string, error) {
	key, folder := "CODEX_HOME", ".codex"
	if id == "claude" {
		key, folder = "CLAUDE_CONFIG_DIR", ".claude"
	}
	root := os.Getenv(key)
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot determine the provider login directory")
		}
		root = filepath.Join(home, folder)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", errors.New("invalid provider configuration directory")
	}
	return abs, nil
}

var authStoreRE = regexp.MustCompile(`^\s*(?:cli_auth_credentials_store|"cli_auth_credentials_store"|'cli_auth_credentials_store')\s*=\s*["'](file|keyring|auto)["']\s*(?:#.*)?$`)

// Read only a non-secret selector from config.toml, never credential files.
// Complex/ambiguous spellings fail closed instead of guessing a different store.
func codexAuthStore(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, "config.toml"))
	if os.IsNotExist(err) {
		return "file", nil
	}
	if err != nil {
		return "", errors.New("cannot read Codex authentication-store configuration")
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1024*1024)
	store := "file"
	inTable := false
	seen := false
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inTable = true
		}
		if inTable {
			continue
		}
		if strings.Contains(line, "cli_auth_credentials_store") {
			m := authStoreRE.FindStringSubmatch(line)
			if len(m) != 2 || seen {
				return "", errors.New("unsupported Codex authentication-store configuration")
			}
			store = m[1]
			seen = true
		}
	}
	if scan.Err() != nil {
		return "", errors.New("cannot read Codex authentication-store configuration")
	}
	return store, nil
}
