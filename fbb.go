package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// monitorFrame is a single decoded monitor frame received from BPQ.
type monitorFrame struct {
	port int    // BPQ port number, parsed from frame text
	dir  string // "rx" or "tx"
	line string // decoded frame text
}

var rePortNum = regexp.MustCompile(`\bPort=(\d+)`)

// parseMonitorFrames extracts monitor frames from a raw byte slice.
//
// Frame format observed from LinBPQ TelnetV6.c (DoMonitor / HOSTTRACEQ loop):
//
//	Monitor frame:    0xFF 0x1B <colour> <text...> 0xFE
//	  colour 17 (0x11) = RX
//	  colour 91 (0x5B) = TX
//	Port-definition: 0xFF 0xFF <count>|<name>|<name>|...  (NO 0xFE terminator)
//
// The port-definition frame has no 0xFE.  Searching for 0xFE to bound it
// would consume the terminator of the next real monitor frame.  Instead we
// skip past 0xFF 0xFF and the following non-0xFF bytes to reach the next frame.
func parseMonitorFrames(data []byte) []monitorFrame {
	var frames []monitorFrame
	for {
		start := bytes.IndexByte(data, 0xFF)
		if start < 0 {
			break
		}
		if start+1 >= len(data) {
			break // need at least 2 bytes to identify frame type
		}

		// Port-definition frame: 0xFF 0xFF … (no 0xFE terminator).
		// Skip past the two-byte header and all following non-0xFF bytes.
		if data[start+1] == 0xFF {
			data = data[start+2:]
			next := bytes.IndexByte(data, 0xFF)
			if next < 0 {
				break // rest is port-def text, no monitor frames follow
			}
			data = data[next:]
			continue
		}

		// Monitor frame: 0xFF 0x1B <colour> <text...> 0xFE
		end := bytes.IndexByte(data[start:], 0xFE)
		if end < 0 {
			break // incomplete frame — wait for more data
		}
		frame := data[start : start+end+1]
		data = data[start+end+1:]

		// Minimum: 0xFF 0x1B <colour> <1-char> 0xFE = 5 bytes.
		if len(frame) < 5 {
			continue
		}
		if frame[1] != 0x1B {
			log.Printf("fbb: unexpected frame byte[1]=0x%02x len=%d bytes=%x", frame[1], len(frame), frame)
			continue
		}

		colourCode := frame[2]
		text := strings.TrimSpace(string(frame[3 : len(frame)-1]))

		dir := "rx"
		if colourCode == 91 { // 0x5B = TX
			dir = "tx"
		}

		// Parse "Port=N" from the frame text to route to the correct pane.
		port := 0
		if m := rePortNum.FindStringSubmatch(text); m != nil {
			port, _ = strconv.Atoi(m[1])
		}

		log.Printf("fbb: colour=%d dir=%s port=%d: %s", colourCode, dir, port, text)
		frames = append(frames, monitorFrame{port: port, dir: dir, line: text})
	}
	return frames
}

// FBBClient connects to BPQ's QtTermTCP monitor port and publishes
// decoded monitor frames to the Hub as Event{Type:"monitor"} events.
// A single connection with an all-ports portmask is used so that BPQ
// delivers frames for every port type (including VARA).
type FBBClient struct {
	cfg BPQConfig
	hub *Hub
}

// NewFBBClient creates a new FBBClient.
func NewFBBClient(cfg BPQConfig, hub *Hub) *FBBClient {
	return &FBBClient{cfg: cfg, hub: hub}
}

// Run connects to BPQ, logs in, and streams monitor frames.
// On disconnect it waits 5 s then reconnects.  Run returns only when ctx
// is cancelled.
//
// BPQ's monitor session silently stops delivering frames after ~20-30 minutes
// without closing the TCP connection.  To guarantee freshness, each connection
// is wrapped in a 15-minute deadline; when it fires we reconnect.
func (f *FBBClient) Run(ctx context.Context) {
	for {
		connCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		err := f.connect(connCtx)
		periodic := connCtx.Err() == context.DeadlineExceeded
		cancel()

		if ctx.Err() != nil {
			return
		}
		if periodic {
			log.Printf("fbb: 15-minute session limit reached, reconnecting")
		} else if err != nil {
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
	// TCP keepalives detect silently broken connections (e.g. BPQ restarted
	// while our socket lingered).
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(60 * time.Second)
	}
	log.Printf("fbb: connected to %s", addr)

	if err := f.login(conn); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	log.Printf("fbb: logged in as %s", f.cfg.Username)

	return f.readFrames(ctx, conn)
}

// login performs the QtTermTCP handshake confirmed from LinBPQ TelnetV6.c:
//
//  1. Send "<username>\r<password>\rBPQTERMTCP\r" in one write — BPQ
//     processes each CR-delimited token: username selects account,
//     password authenticates, "BPQTERMTCP" enters BPQTermMode.
//
//  2. Send the monitor-control string "\\<portmask_hex> <mtx> <mcom> <nodes>
//     <colour> <ui> <utf8> <P8>\r".  All-ports portmask (ffffffffffffffff)
//     so BPQ delivers frames for every port type including VARA.
func (f *FBBClient) login(conn net.Conn) error {
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	// Step 1: credentials + BPQTermTCP in one write.
	signon := fmt.Sprintf("%s\r%s\rBPQTERMTCP\r", f.cfg.Username, f.cfg.Password)
	if _, err := fmt.Fprint(conn, signon); err != nil {
		return fmt.Errorf("write signon: %w", err)
	}

	// Step 2: monitor-control command.
	// All-ports portmask: ffffffffffffffff monitors every BPQ port.
	// mtx=1 (TX on), mcom=1 (connected on), nodes=0, colour=1, ui=1, utf8=0, P8=1.
	// LinBPQ TelnetV6.c requires exactly four backslash bytes before the hex portmask.
	monctl := "\\\\\\\\ffffffffffffffff 1 1 0 1 1 0 1\r"
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
				if fr.port == 0 {
					continue // skip frames we can't route to a pane
				}
				f.hub.Publish(Event{
					Type: "monitor",
					Port: fr.port,
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
