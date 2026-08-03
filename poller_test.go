package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// HTML returned by BPQ for a connected VARA port
const varaConnectedHTML = `<html><body>
<table>
<tr><td width=110px>Comms State</td><td>Connected to VARA TNC</td></tr>
<tr><td>TNC State</td><td></td></tr>
<tr><td>Mode</td><td></td></tr>
<tr><td>Channel State</td><td>Clear</td></tr>
<tr><td>S/N</td><td>18</td></tr>
<tr><td>Traffic</td><td>Sent 4 RXed 2 Queued 0</td></tr>
</table>
<table>
<tr><td width=90px>HAMLIB</td><td width=90px>14.1023</td><td width=90px>PKTUSB/3000</td><td width=90px>S</td></tr>
</table>
</body></html>`

// HTML for a disconnected port
const disconnectedHTML = `<html><body>
<table>
<tr><td width=110px>Comms State</td><td></td></tr>
<tr><td>Channel State</td><td></td></tr>
<tr><td>S/N</td><td></td></tr>
</table>
<table>
<tr><td width=90px>HAMLIB</td><td width=90px>-----------</td><td width=90px>------</td><td width=90px> </td></tr>
</table>
</body></html>`

func TestScrapePortStatus_Connected(t *testing.T) {
	s := scrapePortStatus(varaConnectedHTML)
	if s.state != "Connected to VARA TNC" {
		t.Errorf("state: got %q", s.state)
	}
	if s.channel != "Clear" {
		t.Errorf("channel: got %q", s.channel)
	}
	if s.snr != "18" {
		t.Errorf("snr: got %q", s.snr)
	}
	if s.freq != "14.1023" {
		t.Errorf("freq: got %q", s.freq)
	}
	if !s.valid {
		t.Error("expected valid=true for a port status page")
	}
}

func TestScrapePortStatus_MainPage(t *testing.T) {
	// BPQ returns its main index page for ports without a web driver window.
	mainPage := `<html><head><title>N0CALL-7's BPQ32 Web Server</title></head><body><h1>BPQ32 Node</h1></body></html>`
	s := scrapePortStatus(mainPage)
	if s.valid {
		t.Error("expected valid=false for main BPQ page")
	}
	if s.state != "" {
		t.Errorf("expected empty state for main page, got %q", s.state)
	}
}

func TestScrapePortStatus_Disconnected(t *testing.T) {
	s := scrapePortStatus(disconnectedHTML)
	if s.state != "" {
		t.Errorf("expected empty state, got %q", s.state)
	}
	if !s.valid {
		t.Error("expected valid=true even for a disconnected port status page")
	}
}

func TestPoller_PublishesStatusEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(varaConnectedHTML))
	}))
	defer srv.Close()

	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	cfg := BPQConfig{WebURL: srv.URL, FBBPort: 0}
	ports := []PortConfig{{Num: 6, Label: "VARA FM"}}
	p := NewPoller(cfg, ports, hub, NewSystemdController())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go p.Run(ctx)

	select {
	case e := <-ch:
		if e.Type != "status" {
			t.Errorf("type: got %q, want status", e.Type)
		}
		if e.Port != 6 {
			t.Errorf("port: got %d, want 6", e.Port)
		}
		if e.State != "Connected to VARA TNC" {
			t.Errorf("state: got %q", e.State)
		}
		if e.Freq != "14.1023" {
			t.Errorf("freq: got %q", e.Freq)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for status event")
	}
}

func TestPoller_UnreachableMarksRed(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	cfg := BPQConfig{WebURL: "http://127.0.0.1:1", FBBPort: 0}
	ports := []PortConfig{{Num: 6, Label: "VARA FM"}}
	p := NewPoller(cfg, ports, hub, NewSystemdController())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go p.Run(ctx)

	select {
	case e := <-ch:
		if e.State != "unreachable" {
			t.Errorf("state: got %q, want unreachable", e.State)
		}
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}
