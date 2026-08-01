package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	_ "embed"
)

//go:embed web/index.html
var indexHTML []byte

type Server struct {
	cfg  Config
	hub  *Hub
	sysd *SystemdController
	// port number → service name for quick lookup
	services map[int]string
}

func NewServer(cfg Config, hub *Hub, sysd *SystemdController) *Server {
	svc := make(map[int]string, len(cfg.Ports))
	for _, p := range cfg.Ports {
		svc[p.Num] = p.Service
	}
	return &Server{cfg: cfg, hub: hub, sysd: sysd, services: svc}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /events", s.handleSSE)
	mux.HandleFunc("POST /action", s.handleAction)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

type actionRequest struct {
	Port   int    `json:"port"`
	Action string `json:"action"`
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token != s.cfg.Server.ActionPassword {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req actionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	service, ok := s.services[req.Port]
	if !ok || service == "" {
		http.Error(w, "no service configured for port", http.StatusBadRequest)
		return
	}

	if err := s.sysd.Action(service, req.Action); err != nil {
		log.Printf("action %s %s: %v", req.Action, service, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
