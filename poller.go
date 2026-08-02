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

var (
	reTDPair = regexp.MustCompile(`<td[^>]*>([^<]*)</td>\s*<td[^>]*>([^<]*)</td>`)
	reHamlib = regexp.MustCompile(`<td[^>]*>HAMLIB</td>\s*<td[^>]*>([^<]*)</td>`)
)

type portStatus struct {
	state   string
	channel string
	snr     string
	freq    string
}

func scrapePortStatus(html string) portStatus {
	var s portStatus
	for _, m := range reTDPair.FindAllStringSubmatch(html, -1) {
		key := strings.TrimSpace(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "Comms State":
			s.state = val
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

type Poller struct {
	bpq   BPQConfig
	ports []PortConfig
	hub   *Hub
}

func NewPoller(bpq BPQConfig, ports []PortConfig, hub *Hub) *Poller {
	return &Poller{bpq: bpq, ports: ports, hub: hub}
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
	return Event{
		Type:    "status",
		Port:    port.Num,
		State:   s.state,
		Channel: s.channel,
		SNR:     s.snr,
		Freq:    s.freq,
	}
}
