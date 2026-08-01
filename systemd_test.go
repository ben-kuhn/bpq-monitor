package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// makeFakeSystemctl writes a shell script at path that prints its args and exits with code.
func makeFakeSystemctl(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "systemctl")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\"\nexit %d\n", exitCode)
	os.WriteFile(path, []byte(script), 0755)
	return dir
}

func TestSystemdController_Success(t *testing.T) {
	dir := makeFakeSystemctl(t, 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	sc := NewSystemdController()
	if err := sc.Action("vara-fm", "restart"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSystemdController_Failure(t *testing.T) {
	dir := makeFakeSystemctl(t, 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	sc := NewSystemdController()
	err := sc.Action("vara-fm", "restart")
	if err == nil {
		t.Fatal("expected error on non-zero exit, got nil")
	}
}

func TestSystemdController_InvalidAction(t *testing.T) {
	sc := NewSystemdController()
	err := sc.Action("vara-fm", "explode")
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}
