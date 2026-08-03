package main

import (
	"bytes"
	"fmt"
	"os/exec"
)

var validActions = map[string]bool{
	"start":   true,
	"stop":    true,
	"restart": true,
}

type SystemdController struct{}

func NewSystemdController() *SystemdController {
	return &SystemdController{}
}

func (s *SystemdController) IsActive(service string) bool {
	cmd := exec.Command("systemctl", "--user", "is-active", "--quiet", service+".service")
	return cmd.Run() == nil
}

func (s *SystemdController) Action(service, action string) error {
	if !validActions[action] {
		return fmt.Errorf("invalid action %q: must be start, stop, or restart", action)
	}
	var out bytes.Buffer
	cmd := exec.Command("systemctl", "--user", action, service+".service")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl --user %s %s: %v\n%s", action, service, err, out.String())
	}
	return nil
}
