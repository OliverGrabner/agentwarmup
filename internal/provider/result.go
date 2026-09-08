package provider

import (
	"bufio"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// Classify accepts only recognized structured completion, never an arbitrary
// exit-zero transcript. Persisted diagnostics are fixed messages, not raw JSON.
func Classify(exitCode int, stdout, stderr string) Result {
	failed := Result{"failed", "Provider request did not return a recognized successful result"}
	if len(stdout) > 2*1024*1024 || len(stderr) > 2*1024*1024 {
		return Result{"failed", "Provider output exceeded the safe limit"}
	}
	var finalOK, completed, toolUsed, invalid, structuredError bool
	var observedError Result
	scan := bufio.NewScanner(strings.NewReader(stdout))
	scan.Buffer(make([]byte, 4096), 2*1024*1024)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		var event struct {
			Type              string                     `json:"type"`
			Subtype           string                     `json:"subtype"`
			Result            string                     `json:"result"`
			IsError           bool                       `json:"is_error"`
			Message           json.RawMessage            `json:"message"`
			Error             json.RawMessage            `json:"error"`
			Code              string                     `json:"code"`
			NumTurns          *int                       `json:"num_turns"`
			PermissionDenials []json.RawMessage          `json:"permission_denials"`
			ModelUsage        map[string]json.RawMessage `json:"modelUsage"`
			Item              struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			invalid = true
			continue
		}
		if event.IsError || event.Type == "error" || event.Type == "turn.failed" || (event.Type == "result" && event.Subtype != "success") {
			structuredError = true
			candidate := recognizedError(event.Code + " " + string(event.Error) + " " + string(event.Message) + " " + event.Result)
			if candidate.Outcome != "failed" {
				observedError = candidate
			}
		}
		switch event.Type {
		case "item.started", "item.updated", "item.completed":
			switch event.Item.Type {
			case "agent_message":
				if event.Type == "item.completed" {
					finalOK = strings.TrimSpace(event.Item.Text) == "OK"
				}
			case "reasoning":
			default:
				toolUsed = true
			}
		case "turn.completed":
			completed = true
		case "result":
			completed = true
			finalOK = event.Subtype == "success" && !event.IsError && strings.TrimSpace(event.Result) == "OK"
			if len(event.PermissionDenials) > 0 || (event.NumTurns != nil && *event.NumTurns != 1) {
				toolUsed = true
			}
			for model := range event.ModelUsage {
				if model != ClaudeModel {
					invalid = true
				}
			}
		case "assistant":
			var message struct {
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
			}
			if json.Unmarshal(event.Message, &message) != nil {
				invalid = true
			}
			for _, block := range message.Content {
				if block.Type == "tool_use" {
					toolUsed = true
				}
			}
		case "thread.started", "turn.started", "system", "user", "error", "turn.failed":
		default:
			invalid = true
		}
	}
	if scan.Err() != nil {
		invalid = true
	}
	if toolUsed {
		return Result{"failed", "Provider attempted a tool call; request isolation failed"}
	}
	if observedError.Outcome != "" {
		return observedError
	}
	if exitCode != 0 {
		if result := recognizedError(stderr); result.Outcome != "failed" {
			return result
		}
		return failed
	}
	if structuredError || invalid {
		return failed
	}
	if completed && finalOK {
		return Result{"succeeded", "Request succeeded; reset unverified"}
	}
	return failed
}

func recognizedError(s string) Result {
	s = strings.ToLower(s)
	for _, text := range []string{"rate_limit_exceeded", "rate_limit_error", "usage_limit_reached", "too many requests", "you have hit your usage limit"} {
		if strings.Contains(s, text) {
			return Result{"rate_limited", "Provider usage limit reached"}
		}
	}
	for _, text := range []string{"model_not_found", "model_not_available", "model is not available", "model is not supported", "does not have access to model"} {
		if strings.Contains(s, text) {
			return Result{"model_unavailable", "Configured model is unavailable; no fallback was used"}
		}
	}
	for _, text := range []string{"authentication_error", "invalid_api_key", "token_expired", "refresh_token_reused", "refresh_token_expired", "not logged in", "please run /login", "please run codex login", "please run claude auth login", "401 unauthorized"} {
		if strings.Contains(s, text) {
			return Result{"auth_required", "Provider login is required"}
		}
	}
	return Result{"failed", "Provider request failed"}
}

var (
	keyRE         = regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`)
	bearerRE      = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	jwtRE         = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)?`)
	emailRE       = regexp.MustCompile(`[A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	secretFieldRE = regexp.MustCompile(`(?i)(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|authorization|cookie|account[_-]?id)\s*["']?\s*[:=]\s*["']?[^\s,}"']+`)
)

func Scrub(s string) string {
	s = keyRE.ReplaceAllString(s, "[redacted]")
	s = bearerRE.ReplaceAllString(s, "[redacted]")
	s = jwtRE.ReplaceAllString(s, "[redacted]")
	s = emailRE.ReplaceAllString(s, "[redacted]")
	s = secretFieldRE.ReplaceAllString(s, "[redacted]")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 200 {
		s = string(r[:200])
	}
	return s
}
