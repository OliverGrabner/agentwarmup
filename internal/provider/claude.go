package provider

import "encoding/json"

func claudeIsolationArgs() []string {
	return []string{"--safe-mode", "--setting-sources", "", "--settings", `{"disableAllHooks":true,"autoMemoryEnabled":false}`, "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--tools", "", "--disallowedTools", "mcp__*", "--no-chrome"}
}
func parseClaudeAuth(code int, out string) AuthStatus {
	var status struct {
		LoggedIn         *bool  `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		SubscriptionType string `json:"subscriptionType"`
		APIProvider      string `json:"apiProvider"`
		APIKeySource     string `json:"apiKeySource"`
	}
	if json.Unmarshal([]byte(out), &status) != nil || status.LoggedIn == nil {
		return AuthStatus{"unknown", "Claude did not return recognized authentication status"}
	}
	if !*status.LoggedIn {
		return AuthStatus{"signed_out", "Run claude auth login"}
	}
	if status.AuthMethod == "api_key" || status.AuthMethod == "apiKey" || status.AuthMethod == "console" || status.APIKeySource != "" {
		return AuthStatus{"api", "API or cloud-provider billing is unsupported"}
	}
	if status.APIProvider != "" && status.APIProvider != "firstParty" && status.APIProvider != "anthropic" {
		return AuthStatus{"api", "API or cloud-provider billing is unsupported"}
	}
	if code == 0 && status.AuthMethod == "claude.ai" {
		switch status.SubscriptionType {
		case "pro", "max", "team", "enterprise":
			return AuthStatus{"subscription", "Claude subscription login"}
		}
	}
	return AuthStatus{"unknown", "Claude did not confirm a supported subscription login"}
}
