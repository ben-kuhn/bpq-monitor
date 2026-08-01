package main

import (
	"testing"
	"time"
)

func TestHub_PublishToSubscriber(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe()
	h.Publish(Event{Type: "status", Port: 6, State: "connected"})
	select {
	case e := <-ch:
		if e.Port != 6 || e.State != "connected" {
			t.Errorf("unexpected event: %+v", e)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
	h.Unsubscribe(ch)
}

func TestHub_MultipleSubscribers(t *testing.T) {
	h := NewHub()
	ch1 := h.Subscribe()
	ch2 := h.Subscribe()
	h.Publish(Event{Type: "monitor", Port: 3, Line: "N0CALL>CMS"})
	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.Line != "N0CALL>CMS" {
				t.Errorf("wrong line: %q", e.Line)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout")
		}
	}
	h.Unsubscribe(ch1)
	h.Unsubscribe(ch2)
}

func TestHub_UnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe()
	h.Unsubscribe(ch)
	// publish after unsubscribe must not panic
	h.Publish(Event{Type: "status", Port: 2})
}

func TestHub_SlowSubscriberDrops(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe()
	// fill channel buffer
	for i := 0; i < 65; i++ {
		h.Publish(Event{Type: "monitor", Port: 1, Line: "x"})
	}
	// must not block or panic; drain what arrived
	drained := 0
	for len(ch) > 0 {
		<-ch
		drained++
	}
	if drained > 64 {
		t.Errorf("received %d events into buffer of 64", drained)
	}
	h.Unsubscribe(ch)
}
