package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTOML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.toml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}

func TestLoadConfig_Valid(t *testing.T) {
	path := writeTOML(t, `
[bpq]
web_url  = "http://127.0.0.1:8080"
fbb_port = 8011
username = "n0call"
password = "secret"

[server]
listen          = "127.0.0.1:9090"
action_password = "hunter2"

[layout]
columns = 3

[[port]]
num     = 6
label   = "VARA FM"
service = "vara-fm"

[[port]]
num   = 4
label = "AXIP"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BPQ.FBBPort != 8011 {
		t.Errorf("FBBPort: got %d, want 8011", cfg.BPQ.FBBPort)
	}
	if len(cfg.Ports) != 2 {
		t.Errorf("Ports: got %d, want 2", len(cfg.Ports))
	}
	if cfg.Ports[1].Service != "" {
		t.Errorf("AXIP port should have no service")
	}
}

func TestLoadConfig_MissingActionPassword(t *testing.T) {
	path := writeTOML(t, `
[bpq]
web_url = "http://127.0.0.1:8080"
fbb_port = 8011
username = "n0call"
password = "secret"

[server]
listen = "127.0.0.1:9090"

[[port]]
num   = 6
label = "VARA FM"
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing action_password, got nil")
	}
}

func TestLoadConfig_NoPorts(t *testing.T) {
	path := writeTOML(t, `
[bpq]
web_url = "http://127.0.0.1:8080"
fbb_port = 8011
username = "n0call"
password = "secret"

[server]
listen          = "127.0.0.1:9090"
action_password = "hunter2"
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for no ports, got nil")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
