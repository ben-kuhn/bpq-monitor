package main

import (
	"bufio"
	"context"
	"log"
	"os/exec"
	"time"
)

// JournalClient streams the systemd journal for a single user service and
// publishes each line as an Event{Type:"log"} to the Hub.
type JournalClient struct {
	port    int
	service string
	hub     *Hub
}

func NewJournalClient(port int, service string, hub *Hub) *JournalClient {
	return &JournalClient{port: port, service: service, hub: hub}
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
		j.hub.Publish(Event{
			Type: "log",
			Port: j.port,
			Line: scanner.Text(),
		})
	}

	return cmd.Wait()
}
