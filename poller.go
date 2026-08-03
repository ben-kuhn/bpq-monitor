package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	watchdogUnhealthyThreshold = 15 * time.Minute
	watchdogDeafThreshold      = 1 * time.Hour
)

var (
	reTDPair      = regexp.MustCompile(`<td[^>]*>([^<]*)</td>\s*<td[^>]*>([^<]*)</td>`)
	reHamlib      = regexp.MustCompile(`<td[^>]*>HAMLIB</td>\s*<td[^>]*>([^<]*)</td>`)
	reFramesHeard = regexp.MustCompile(`L2 Frames Heard[^0-9]*([0-9]+)`)
)

type portStatus struct {
	state   string
	channel string
	snr     string
	freq    string
	valid   bool // true only when a recognised port-status page was returned
}

func scrapePortStatus(html string) portStatus {
	var s portStatus
	for _, m := range reTDPair.FindAllStringSubmatch(html, -1) {
		key := strings.TrimSpace(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "Comms State":
			s.state = val
			s.valid = true
		case "Channel State":
			s.channel = val
		case "S/N":
			s.snr = val
		}
	}
	if m := reHamlib.FindStringSubmatch(html); m != nil {
		freq := strings.TrimSpace(m[1])
		if freq != "-----------" && freq != "" {
			s.freq = freq
		}
	}
	return s
}

type watchState struct {
	unhealthySince  time.Time // zero → currently healthy
	lastFrameCount  int
	lastFrameChange time.Time // zero → not yet observed
}

type Poller struct {
	bpq         BPQConfig
	ports       []PortConfig
	hub         *Hub
	sysd        *SystemdController
	watchStates map[int]*watchState
}

func NewPoller(bpq BPQConfig, ports []PortConfig, hub *Hub, sysd *SystemdController) *Poller {
	ws := make(map[int]*watchState, len(ports))
	for _, p := range ports {
		ws[p.Num] = &watchState{}
	}
	return &Poller{bpq: bpq, ports: ports, hub: hub, sysd: sysd, watchStates: ws}
}

func (p *Poller) Run(ctx context.Context) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	p.poll(client)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(client)
		}
	}
}

func (p *Poller) poll(client *http.Client) {
	for _, port := range p.ports {
		e := p.fetchStatus(client, port)
		p.hub.Publish(e)
		p.checkWatchdog(client, port, e)
	}
}

func isHealthy(state string) bool {
	if state == "" {
		return false
	}
	lower := strings.ToLower(state)
	return strings.Contains(lower, "connected") || state == "Running"
}

func (p *Poller) checkWatchdog(client *http.Client, port PortConfig, e Event) {
	if port.Service == "" {
		return
	}
	ws := p.watchStates[port.Num]
	if isHealthy(e.State) {
		ws.unhealthySince = time.Time{}
		if port.DeafCheck {
			p.checkDeaf(client, port, ws)
		}
		return
	}
	if ws.unhealthySince.IsZero() {
		ws.unhealthySince = time.Now()
		return
	}
	if time.Since(ws.unhealthySince) >= watchdogUnhealthyThreshold {
		dur := time.Since(ws.unhealthySince).Round(time.Second)
		log.Printf("watchdog: %s unhealthy for %s — restarting", port.Service, dur)
		if err := p.sysd.Action(port.Service, "restart"); err != nil {
			log.Printf("watchdog: restart %s: %v", port.Service, err)
		}
		ws.unhealthySince = time.Time{}
	}
}

func (p *Poller) checkDeaf(client *http.Client, port PortConfig, ws *watchState) {
	url := fmt.Sprintf("%s/Node/PortStats?%d&ASYNC", p.bpq.WebURL, port.Num)
	resp, err := client.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	m := reFramesHeard.FindSubmatch(body)
	if m == nil {
		return
	}
	count := 0
	for _, b := range m[1] {
		count = count*10 + int(b-'0')
	}
	if ws.lastFrameChange.IsZero() || count != ws.lastFrameCount {
		ws.lastFrameCount = count
		ws.lastFrameChange = time.Now()
		return
	}
	if time.Since(ws.lastFrameChange) >= watchdogDeafThreshold {
		dur := time.Since(ws.lastFrameChange).Round(time.Second)
		log.Printf("watchdog: %s deaf for %s (L2 frames heard stuck at %d) — restarting", port.Service, dur, count)
		if err := p.sysd.Action(port.Service, "restart"); err != nil {
			log.Printf("watchdog: restart %s: %v", port.Service, err)
		}
		ws.lastFrameChange = time.Now()
	}
}

func (p *Poller) fetchStatus(client *http.Client, port PortConfig) Event {
	url := fmt.Sprintf("%s/Node/Port?%d", p.bpq.WebURL, port.Num)
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("poller: port %d: %v", port.Num, err)
		return Event{Type: "status", Port: port.Num, State: "unreachable"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("poller: port %d: read body: %v", port.Num, err)
		return Event{Type: "status", Port: port.Num, State: "unreachable"}
	}
	s := scrapePortStatus(string(body))
	if !s.valid {
		// BPQ returned its main page — this port has no web status.
		// Fall back to systemd service state if a service name is configured.
		if port.Service != "" {
			state := ""
			if p.sysd.IsActive(port.Service) {
				state = "Running"
			}
			return Event{Type: "status", Port: port.Num, State: state}
		}
		// No service and no BPQ page — skip publishing.
		return Event{}
	}
	return Event{
		Type:    "status",
		Port:    port.Num,
		State:   s.state,
		Channel: s.channel,
		SNR:     s.snr,
		Freq:    s.freq,
	}
}
