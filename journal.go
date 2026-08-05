package main

import (
	"bufio"
	"context"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// reDirewolfFrame matches lines like:
//
//	[0] ADDR>DEST:(...)      — received
//	[0.4] ADDR>DEST:(...)   — received (quality suffix)
//	[0L] ADDR>DEST:(...)    — transmitted (loopback from BPQ via KISS)
var reDirewolfFrame = regexp.MustCompile(`^\[([0-9]+)(L|[.][0-9]+)?\] (.+)$`)

// JournalClient streams the systemd journal for a single user service and
// publishes each line as an Event{Type:"log"} to the Hub.
// For Direwolf services it also parses AX.25 frame lines and publishes them
// as Event{Type:"monitor"} so they appear in the traffic pane.
type JournalClient struct {
	port       int
	service    string
	hub        *Hub
	isDirewolf bool
}

func NewJournalClient(port int, service string, hub *Hub) *JournalClient {
	return &JournalClient{
		port:       port,
		service:    service,
		hub:        hub,
		isDirewolf: strings.Contains(strings.ToLower(service), "direwolf"),
	}
}

func (j *JournalClient) Run(ctx context.Context) {
	for {
		if err := j.stream(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("journal %s: %v", j.service, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (j *JournalClient) stream(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "journalctl",
		"--user", "-u", j.service+".service",
		"-n", "50", "-f", "--output=cat")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		j.hub.Publish(Event{
			Type: "log",
			Port: j.port,
			Line: line,
		})
		if j.isDirewolf {
			if m := reDirewolfFrame.FindStringSubmatch(line); m != nil {
				dir := "rx"
				if m[2] == "L" {
					dir = "tx"
				}
				j.hub.Publish(Event{
					Type: "monitor",
					Port: j.port,
					Dir:  dir,
					Line: m[3],
				})
			}
		}
	}

	return cmd.Wait()
}
