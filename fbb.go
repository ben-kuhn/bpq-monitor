package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

// monitorFrame is a single decoded monitor frame received from BPQ.
type monitorFrame struct {
	dir  string // "rx" or "tx"
	line string // decoded frame text
}

// parseMonitorFrames extracts monitor frames from a raw byte slice.
//
// Frame format confirmed from LinBPQ TelnetV6.c (DoMonitor / HOSTTRACEQ loop):
//
//	Monitor frame:       0xFF 0x1B <colour> <text...> 0xFE
//	  colour 17  (0x11) = RX (buffer initialised to "\xff\x1b\xb"; TX branch sets buffer[2]=91)
//	  colour 91  (0x5B) = TX
//	Port-definition:    0xFF 0xFF <pipe-delimited names> 0xFE  — silently skipped
//
// Incomplete frames (no closing 0xFE) are silently ignored.
func parseMonitorFrames(data []byte) []monitorFrame {
	var frames []monitorFrame
	for {
		start := bytes.IndexByte(data, 0xFF)
		if start < 0 {
			break
		}
		end := bytes.IndexByte(data[start:], 0xFE)
		if end < 0 {
			// Incomplete frame — stop; caller may append more data later.
			break
		}
		frame := data[start : start+end+1]
		data = data[start+end+1:]

		// Port-definition frame: 0xFF 0xFF … 0xFE — skip.
		if len(frame) >= 2 && frame[1] == 0xFF {
			continue
		}

		// Minimum valid monitor frame: 0xFF 0x1B <colour> <1-byte text> 0xFE = 5 bytes.
		if len(frame) < 5 {
			continue
		}

		// Expect ESC byte at position 1.
		if frame[1] != 0x1B {
			continue
		}

		colourCode := frame[2]
		text := strings.TrimSpace(string(frame[3 : len(frame)-1]))

		dir := "rx"
		if colourCode == 91 { // 0x5B = TX
			dir = "tx"
		}

		frames = append(frames, monitorFrame{dir: dir, line: text})
	}
	return frames
}

// FBBClient connects to BPQ's BPQTermTCP monitor port and publishes
// decoded monitor frames to the Hub as Event{Type:"monitor"} events.
type FBBClient struct {
	cfg  BPQConfig
	hub  *Hub
	port int // BPQ port number (1-64); portmask = 1<<(port-1)
}

// NewFBBClient creates a new FBBClient that monitors a single BPQ port.
// One FBBClient per configured port; BPQ filters server-side by portmask.
func NewFBBClient(cfg BPQConfig, hub *Hub, port int) *FBBClient {
	return &FBBClient{cfg: cfg, hub: hub, port: port}
}

// Run connects to BPQ, logs in, and streams monitor frames.
// On disconnect it waits 5 s then reconnects.  Run returns only when ctx
// is cancelled.
func (f *FBBClient) Run(ctx context.Context) {
	for {
		if err := f.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("fbb: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (f *FBBClient) connect(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", f.cfg.FBBPort)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()
	log.Printf("fbb: connected to %s", addr)

	if err := f.login(conn); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	log.Printf("fbb: logged in as %s", f.cfg.Username)

	return f.readFrames(ctx, conn)
}

// login performs the BPQTermTCP handshake confirmed from LinBPQ TelnetV6.c
// and BPQTermTCP.c:
//
//  1. Send "<username>\r<password>\rBPQTERMTCP\r" in one write — BPQ
//     processes each CR-delimited token: username selects account,
//     password authenticates, "BPQTERMTCP" enters BPQTermMode.
//
//  2. Send the monitor-control string "\\<portmask_hex> <mtx> <mcom> <nodes>
//     <colour> <ui> <utf8> <P8>\r".  portmask has one bit set for f.port
//     so BPQ filters server-side and sends only that port's frames.
//     mtx=1 (TX on), mcom=1 (connected on), nodes=0, colour=1,
//     ui=0, utf8=0, P8=1 (request port-definition list on login).
func (f *FBBClient) login(conn net.Conn) error {
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	// Step 1: credentials + BPQTermTCP in one write.
	signon := fmt.Sprintf("%s\r%s\rBPQTERMTCP\r", f.cfg.Username, f.cfg.Password)
	if _, err := fmt.Fprint(conn, signon); err != nil {
		return fmt.Errorf("write signon: %w", err)
	}

	// Step 2: monitor-control command.
	// portmask: bit (port-1) enables BPQ-side filtering to this port only.
	portMask := uint64(1) << uint(f.port-1)
	monctl := fmt.Sprintf("\\\\%016x 1 1 0 1 0 0 1\r", portMask)
	if _, err := fmt.Fprint(conn, monctl); err != nil {
		return fmt.Errorf("write monitor control: %w", err)
	}

	return nil
}

// readFrames reads raw bytes from conn, parses monitor frames, and
// publishes each as an Event.  It returns when conn is closed or ctx is
// cancelled.
func (f *FBBClient) readFrames(ctx context.Context, conn net.Conn) error {
	// Close the connection when ctx is cancelled so the blocking Read unblocks.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	var buf []byte
	tmp := make([]byte, 4096)

	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			frames := parseMonitorFrames(buf)

			// Discard all bytes up to and including the last 0xFE we
			// consumed.  This is safe because parseMonitorFrames walks
			// forward and stops at the first incomplete frame.
			if last := bytes.LastIndexByte(buf, 0xFE); last >= 0 {
				buf = buf[last+1:]
			}

			for _, fr := range frames {
				f.hub.Publish(Event{
					Type: "monitor",
					Port: f.port,
					Dir:  fr.dir,
					Line: fr.line,
				})
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			return fmt.Errorf("read: %w", err)
		}
	}
}
