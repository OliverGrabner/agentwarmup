package provider

import "strings"

func codexAuthArgs(i Installation) []string {
	store := i.AuthStore
	if store == "" {
		store = "file"
	}
	return []string{"-c", `model_provider="openai"`, "-c", `cli_auth_credentials_store="` + store + `"`}
}
func codexRequestArgs(i Installation) []string {
	args := []string{"exec", "--ignore-user-config", "--ephemeral", "--json", "--skip-git-repo-check", "--sandbox", "read-only", "--color", "never", "--model", CodexModel}
	args = append(args, codexAuthArgs(i)...)
	args = append(args, "-c", `model_reasoning_effort="low"`, "-c", "project_doc_max_bytes=0", "-c", "skills.include_instructions=false", "-c", `web_search="disabled"`, "-c", `approval_policy="never"`, "-c", `history.persistence="none"`, "-c", "analytics.enabled=false")
	for _, feature := range []string{"hooks", "plugins", "apps", "remote_plugin", "memories", "multi_agent", "shell_tool", "shell_snapshot", "unified_exec", "code_mode", "code_mode_host", "skill_mcp_dependency_install", "skill_search", "image_generation", "goals"} {
		args = append(args, "--disable", feature)
	}
	args = append(args, "--enable", "skip_host_skill_discovery", Prompt)
	return args
}

func parseCodexAuth(code int, out, stderr string) AuthStatus {
	text := strings.TrimSpace(out + "\n" + stderr)
	if code == 0 {
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == "Logged in using ChatGPT" {
				return AuthStatus{"subscription", "ChatGPT subscription login"}
			}
		}
		if strings.Contains(text, "Logged in using an API key") || strings.Contains(text, "Logged in using API key") {
			return AuthStatus{"api", "API-key billing is unsupported"}
		}
	}
	if code != 0 && (text == "Not logged in" || strings.HasSuffix(text, "\nNot logged in")) {
		return AuthStatus{"signed_out", "Run codex login"}
	}
	return AuthStatus{"unknown", "Codex did not confirm subscription authentication"}
}
