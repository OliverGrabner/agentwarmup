// fakeprovider is an offline test fixture. It never imports a provider SDK or
// accesses credentials. Options can be passed as --fake-* flags or placed in a
// fake-options.json file alongside its executable for native scheduler tests.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type options struct {
	Mode   string `json:"mode"`
	Record string `json:"record"`
	Delay  string `json:"delay"`
}

func main() {
	exe, _ := os.Executable()
	opts := options{Mode: "ok", Delay: "10s"}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "fake-options.json")); err == nil {
		_ = json.Unmarshal(data, &opts)
	}
	args := os.Args[1:]
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--fake-mode":
			opts.Mode = args[i+1]
			i++
		case "--fake-record":
			opts.Record = args[i+1]
			i++
		case "--fake-delay":
			opts.Delay = args[i+1]
			i++
		}
	}
	delay, _ := time.ParseDuration(opts.Delay)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--version") {
		fmt.Println("codex-cli 0.153.0")
		return
	}
	if strings.Contains(joined, "--help") {
		fmt.Println("--ignore-user-config --ephemeral --json --sandbox --skip-git-repo-check --safe-mode --setting-sources --settings --strict-mcp-config --mcp-config --tools --no-session-persistence --output-format")
		return
	}
	if strings.Contains(joined, "login status") {
		if opts.Mode == "auth" {
			fmt.Fprintln(os.Stderr, "Not logged in")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Logged in using ChatGPT")
		return
	}
	if strings.Contains(joined, "auth status") {
		fmt.Println(`{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro"}`)
		return
	}
	if opts.Record != "" {
		cwd, _ := os.Getwd()
		input, _ := io.ReadAll(os.Stdin)
		data, _ := json.Marshal(struct {
			Args       []string `json:"args"`
			Dir        string   `json:"dir"`
			StdinBytes int      `json:"stdinBytes"`
			PID        int      `json:"pid"`
		}{args, cwd, len(input), os.Getpid()})
		if f, err := os.OpenFile(opts.Record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600); err == nil {
			_, _ = f.Write(append(data, '\n'))
			_ = f.Close()
		} else {
			os.Exit(2)
		}
	}
	switch opts.Mode {
	case "sleep":
		time.Sleep(delay)
	case "child":
		child := exec.Command(exe, "--fake-mode", "delayed-write", "--fake-delay", opts.Delay, "--fake-record", opts.Record+".child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		time.Sleep(delay * 2)
		_ = child.Wait()
	case "delayed-write":
		time.Sleep(delay)
		_ = os.WriteFile(opts.Record+".survived", []byte("alive"), 0600)
	case "large":
		fmt.Print(strings.Repeat("x", 1024*1024))
		return
	case "auth":
		fmt.Fprintln(os.Stderr, "token_expired")
		os.Exit(1)
	case "rate":
		fmt.Fprintln(os.Stderr, "rate_limit_exceeded")
		os.Exit(1)
	case "model":
		fmt.Fprintln(os.Stderr, "model_not_found")
		os.Exit(1)
	case "structured-error":
		fmt.Println(`{"type":"result","is_error":true,"subtype":"error_during_execution","result":"rate_limit_exceeded"}`)
		return
	}
	if strings.Contains(joined, "--output-format") {
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"OK"}`)
		return
	}
	fmt.Println(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}`)
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1}}`)
}
