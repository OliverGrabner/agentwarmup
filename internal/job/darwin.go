//go:build darwin

package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m Manager) label() string  { return "com." + m.Name }
func (m Manager) domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }
func (m Manager) plistPath() (string, error) {
	h, e := os.UserHomeDir()
	return filepath.Join(h, "Library", "LaunchAgents", m.label()+".plist"), e
}
func (m Manager) Check() error {
	if e := m.validate(); e != nil {
		return e
	}
	_, e := command("launchctl", "print", m.domain())
	if e != nil {
		return fmt.Errorf("a current macOS GUI login session is required: %w", e)
	}
	return nil
}
func (m Manager) plist(exe string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>` + escapeXML(m.label()) + `</string><key>ProgramArguments</key><array><string>` + escapeXML(exe) + `</string><string>check</string><string>--home</string><string>` + escapeXML(m.Root) + `</string></array><key>RunAtLoad</key><true/><key>StartInterval</key><integer>60</integer><key>ProcessType</key><string>Background</string><key>LimitLoadToSessionType</key><string>Aqua</string><key>AbandonProcessGroup</key><false/></dict></plist>`
}
func (m Manager) Install(exe string) error {
	if e := m.validateExe(exe); e != nil {
		return e
	}
	if e := m.Check(); e != nil {
		return e
	}
	p, e := m.plistPath()
	if e != nil {
		return e
	}
	s, e := m.Inspect()
	if e != nil {
		return e
	}
	if s.Loaded {
		if _, e = command("launchctl", "bootout", m.domain()+"/"+m.label()); e != nil {
			return e
		}
	}
	if e = writeFile(p, []byte(m.plist(exe))); e != nil {
		return e
	}
	if _, e = command("launchctl", "enable", m.domain()+"/"+m.label()); e != nil {
		return e
	}
	_, e = command("launchctl", "bootstrap", m.domain(), p)
	return e
}
func (m Manager) Inspect() (Status, error) {
	if e := m.validate(); e != nil {
		return Status{}, e
	}
	if e := m.Check(); e != nil {
		return Status{}, e
	}
	out, e := command("launchctl", "print", m.domain()+"/"+m.label())
	if e != nil {
		if strings.Contains(string(out), "Could not find service") {
			p, err := m.plistPath()
			if err != nil {
				return Status{}, err
			}
			_, err = os.Stat(p)
			if os.IsNotExist(err) {
				return Status{}, nil
			}
			if err != nil {
				return Status{}, err
			}
			return Status{Installed: true, Enabled: false, Detail: "macOS LaunchAgent unloaded"}, nil
		}
		return Status{}, e
	}
	disabled, e := command("launchctl", "print-disabled", m.domain())
	if e != nil {
		return Status{}, e
	}
	return Status{Installed: true, Loaded: true, Enabled: !strings.Contains(string(disabled), `"`+m.label()+`" => true`), Detail: "macOS LaunchAgent"}, nil
}
func (m Manager) SetEnabled(enabled bool) error {
	s, e := m.Inspect()
	if e != nil {
		return e
	}
	if !s.Installed {
		return fmt.Errorf("LaunchAgent is not installed")
	}
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	if _, e = command("launchctl", verb, m.domain()+"/"+m.label()); e != nil {
		return e
	}
	if enabled && !s.Loaded {
		p, e := m.plistPath()
		if e != nil {
			return e
		}
		_, e = command("launchctl", "bootstrap", m.domain(), p)
		return e
	}
	if !enabled && s.Loaded {
		_, e = command("launchctl", "bootout", m.domain()+"/"+m.label())
		return e
	}
	return nil
}
func (m Manager) Uninstall() error {
	s, e := m.Inspect()
	if e != nil {
		return e
	}
	if s.Loaded {
		if _, e = command("launchctl", "bootout", m.domain()+"/"+m.label()); e != nil {
			return e
		}
	}
	p, e := m.plistPath()
	if e != nil {
		return e
	}
	e = os.Remove(p)
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
