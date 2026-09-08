//go:build !windows && !linux && !darwin

package job

import "fmt"

func (m Manager) Check() error             { return fmt.Errorf("this operating system is unsupported") }
func (m Manager) Install(string) error     { return m.Check() }
func (m Manager) Inspect() (Status, error) { return Status{}, m.Check() }
func (m Manager) Uninstall() error         { return m.Check() }
func (m Manager) SetEnabled(bool) error    { return m.Check() }
