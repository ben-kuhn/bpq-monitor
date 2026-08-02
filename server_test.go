package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, *Hub) {
	t.Helper()
	cfg := Config{
		Server: ServerConfig{
			Listen:         "127.0.0.1:0",
			ActionPassword: "testpass",
		},
		Ports: []PortConfig{
			{Num: 6, Label: "VARA FM", Service: "vara-fm"},
			{Num: 4, Label: "AXIP"},
		},
	}
	hub := NewHub()
	sysd := NewSystemdController()
	return NewServer(cfg, hub, sysd), hub
}

func TestServer_IndexHTML(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "html") {
		t.Error("response does not look like HTML")
	}
}

func TestServer_SSEStream(t *testing.T) {
	srv, hub := testServer(t)

	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/events", nil)
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Handler().ServeHTTP(rec, req)
	}()

	time.Sleep(20 * time.Millisecond)
	hub.Publish(Event{Type: "status", Port: 6, State: "connected"})
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"status"`) {
		t.Errorf("SSE body missing status event: %q", body)
	}
}

func TestServer_ActionAuth_Valid(t *testing.T) {
	srv, _ := testServer(t)

	// Use a fake systemctl that always succeeds via PATH override
	t.Setenv("PATH", makeFakeSystemctlPath(t, 0))

	body := `{"port":6,"action":"restart"}`
	req := httptest.NewRequest("POST", "/action", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestServer_ActionAuth_WrongPassword(t *testing.T) {
	srv, _ := testServer(t)

	body := `{"port":6,"action":"restart"}`
	req := httptest.NewRequest("POST", "/action", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrongpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("status: got %d, want 401", rec.Code)
	}
}

func TestServer_ActionAuth_NoServicePort(t *testing.T) {
	srv, _ := testServer(t)

	// Port 4 (AXIP) has no service — action should be rejected
	body := `{"port":4,"action":"restart"}`
	req := httptest.NewRequest("POST", "/action", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("status: got %d, want 400 (no service configured)", rec.Code)
	}
}

func TestServer_Config(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/config", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("status: %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	ports := resp["ports"].([]interface{})
	if len(ports) != 2 {
		t.Errorf("ports: got %d, want 2", len(ports))
	}
}

// makeFakeSystemctlPath creates a fake systemctl in a temp dir and returns the augmented PATH string.
func makeFakeSystemctlPath(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0755)
	return dir + ":" + os.Getenv("PATH")
}
