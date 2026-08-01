package main

import "sync"

type Event struct {
	Type    string `json:"type"`
	Port    int    `json:"port"`
	State   string `json:"state,omitempty"`
	Freq    string `json:"freq,omitempty"`
	SNR     string `json:"snr,omitempty"`
	Channel string `json:"channel,omitempty"`
	Line    string `json:"line,omitempty"`
	Dir     string `json:"dir,omitempty"`
}

type Hub struct {
	mu      sync.Mutex
	clients map[<-chan Event]chan Event
}

func NewHub() *Hub {
	return &Hub{clients: make(map[<-chan Event]chan Event)}
}

func (h *Hub) Subscribe() <-chan Event {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.clients[ch] = ch
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch <-chan Event) {
	h.mu.Lock()
	if c, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(c)
	}
	h.mu.Unlock()
}

func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.clients {
		select {
		case ch <- e:
		default:
		}
	}
}
