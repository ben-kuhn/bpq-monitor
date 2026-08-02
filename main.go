package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPath := flag.String("config", "bpq-monitor.toml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	hub := NewHub()
	poller := NewPoller(cfg.BPQ, cfg.Ports, hub)
	sysd := NewSystemdController()
	srv := NewServer(*cfg, hub, sysd)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go poller.Run(ctx)

	// One FBBClient per configured port; BPQ filters server-side by portmask.
	for _, p := range cfg.Ports {
		fbb := NewFBBClient(cfg.BPQ, hub, p.Num)
		go fbb.Run(ctx)
	}

	httpSrv := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: srv.Handler(),
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down")
		cancel()
		httpSrv.Close()
	}()

	log.Printf("listening on %s", cfg.Server.Listen)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http: %v", err)
	}
}
