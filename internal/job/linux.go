//go:build linux

package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m Manager) unitDir() (string, error) {
	h, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "systemd", "user"), nil
	}
	return filepath.Join(h, ".config", "systemd", "user"), nil
}
func (m Manager) Check() error {
	if e := m.validate(); e != nil {
		return e
	}
	_, e := command("systemctl", "--user", "show-environment")
	if e != nil {
		return fmt.Errorf("a running systemd user manager is required; sign in to a systemd desktop session: %w", e)
	}
	return nil
}

// systemd expands percent specifiers and dollar variables even inside quotes.
func systemdArg(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "$", "$$")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return "\"" + s + "\""
}
func (m Manager) service(exe string) string {
	return "[Unit]\nDescription=AgentWarmup guarded minute check\n\n[Service]\nType=oneshot\nExecStart=" + systemdArg(exe) + " check --home " + systemdArg(m.Root) + "\nTimeoutStartSec=5min\nKillMode=control-group\n"
}
func (m Manager) timer() string {
	return "[Unit]\nDescription=AgentWarmup minute schedule\n\n[Timer]\nOnCalendar=*-*-* *:*:00\nOnStartupSec=1s\nAccuracySec=1s\nRandomizedDelaySec=0\nPersistent=false\nUnit=" + m.Name + ".service\n\n[Install]\nWantedBy=timers.target\n"
}
func (m Manager) Install(exe string) error {
	if e := m.validateExe(exe); e != nil {
		return e
	}
	if e := m.Check(); e != nil {
		return e
	}
	dir, e := m.unitDir()
	if e != nil {
		return e
	}
	if e = writeFile(filepath.Join(dir, m.Name+".service"), []byte(m.service(exe))); e != nil {
		return e
	}
	if e = writeFile(filepath.Join(dir, m.Name+".timer"), []byte(m.timer())); e != nil {
		return e
	}
	if _, e = command("systemctl", "--user", "daemon-reload"); e != nil {
		return e
	}
	if _, e = command("systemctl", "--user", "enable", m.Name+".timer"); e != nil {
		return e
	}
	_, e = command("systemctl", "--user", "restart", m.Name+".timer")
	return e
}
func (m Manager) Inspect() (Status, error) {
	if e := m.validate(); e != nil {
		return Status{}, e
	}
	out, e := command("systemctl", "--user", "show", m.Name+".timer", "--property=LoadState,ActiveState,UnitFileState")
	if e != nil {
		return Status{}, e
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			fields[k] = v
		}
	}
	if fields["LoadState"] == "" {
		return Status{}, fmt.Errorf("systemd returned no job state")
	}
	return Status{Installed: fields["LoadState"] != "not-found", Enabled: fields["ActiveState"] == "active" && fields["UnitFileState"] == "enabled", Detail: "systemd user timer"}, nil
}
func (m Manager) SetEnabled(enabled bool) error {
	if e := m.validate(); e != nil {
		return e
	}
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	_, e := command("systemctl", "--user", verb, "--now", m.Name+".timer")
	return e
}
func (m Manager) Uninstall() error {
	s, e := m.Inspect()
	if e != nil {
		return e
	}
	dir, e := m.unitDir()
	if e != nil {
		return e
	}
	if s.Installed {
		if _, e = command("systemctl", "--user", "disable", "--now", m.Name+".timer"); e != nil {
			return e
		}
		if _, e = command("systemctl", "--user", "stop", m.Name+".service"); e != nil {
			return e
		}
	}
	for _, ext := range []string{".service", ".timer"} {
		if e = os.Remove(filepath.Join(dir, m.Name+ext)); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	_, e = command("systemctl", "--user", "daemon-reload")
	return e
}
