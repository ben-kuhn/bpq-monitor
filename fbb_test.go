package main

import (
	"testing"
)

// Frame format observed on wire:
//
//	Monitor frame:    0xFF 0x1B <colour> <decoded_text...> 0xFE
//	  colour 17 (0x11) = RX
//	  colour 91 (0x5B) = TX
//	Port-definition: 0xFF 0xFF <count>|<name>|<name>|... (NO 0xFE terminator)
//
// There is NO port byte; the port is not included in the wire frame.

func TestParseMonitorFrames_SingleFrame(t *testing.T) {
	// 0xFF 0x1B 17=RX "N0CALL>CMS" 0xFE
	data := []byte{0xFF, 0x1B, 17, 'K', 'U', '0', 'H', 'N', '>', 'C', 'M', 'S', 0xFE}
	frames := parseMonitorFrames(data)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].dir != "rx" {
		t.Errorf("dir: got %q, want rx", frames[0].dir)
	}
	if frames[0].line != "N0CALL>CMS" {
		t.Errorf("line: got %q, want \"N0CALL>CMS\"", frames[0].line)
	}
}

func TestParseMonitorFrames_TXFrame(t *testing.T) {
	// 0xFF 0x1B 91=TX "N0CALL" 0xFE
	data := []byte{0xFF, 0x1B, 91, 'K', 'U', '0', 'H', 'N', 0xFE}
	frames := parseMonitorFrames(data)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].dir != "tx" {
		t.Errorf("dir: got %q, want tx", frames[0].dir)
	}
}

func TestParseMonitorFrames_PortDefinitionSkipped(t *testing.T) {
	// BPQ sends port-def as 0xFF 0xFF <text with | separators> — NO 0xFE.
	data := []byte{0xFF, 0xFF, '6', '|', '7', '|'}
	frames := parseMonitorFrames(data)
	if len(frames) != 0 {
		t.Errorf("port definition should produce 0 frames, got %d", len(frames))
	}
}

func TestParseMonitorFrames_MultipleFrames(t *testing.T) {
	data := []byte{
		0xFF, 0x1B, 17, 'A', 0xFE,
		0xFF, 0x1B, 91, 'B', 0xFE,
	}
	frames := parseMonitorFrames(data)
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].dir != "rx" {
		t.Errorf("frames[0].dir: got %q, want rx", frames[0].dir)
	}
	if frames[1].dir != "tx" {
		t.Errorf("frames[1].dir: got %q, want tx", frames[1].dir)
	}
}

func TestParseMonitorFrames_IncompleteFrame(t *testing.T) {
	// Frame without closing 0xFE — should be ignored
	data := []byte{0xFF, 0x1B, 17, 'A'}
	frames := parseMonitorFrames(data)
	if len(frames) != 0 {
		t.Errorf("incomplete frame should produce 0 frames, got %d", len(frames))
	}
}

func TestParseMonitorFrames_UnknownColour(t *testing.T) {
	// Unknown colour byte → still produces a frame, dir defaults to "rx"
	data := []byte{0xFF, 0x1B, 42, 'X', 0xFE}
	frames := parseMonitorFrames(data)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].line != "X" {
		t.Errorf("line: got %q, want \"X\"", frames[0].line)
	}
}

func TestParseMonitorFrames_PortDefMixedWithFrames(t *testing.T) {
	// BPQ sends port-def WITHOUT 0xFE, immediately followed by monitor frames.
	// The old parser consumed the monitor frame's 0xFE as the port-def terminator,
	// silently dropping the frame. Verified against actual BPQ wire capture.
	data := []byte{
		0xFF, 0xFF, '6', '|', '7', '|', // port definition — NO 0xFE
		0xFF, 0x1B, 17, 'Z', 0xFE,      // monitor frame — must be kept
	}
	frames := parseMonitorFrames(data)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1 (port-def without 0xFE was swallowing monitor frame)", len(frames))
	}
	if frames[0].line != "Z" {
		t.Errorf("line: got %q, want \"Z\"", frames[0].line)
	}
}

func TestParseMonitorFrames_PortDefAtStartRealTraffic(t *testing.T) {
	// Simulates the actual BPQ login sequence: greeting text, then port-def,
	// then a monitor frame — matching the wire capture from LinBPQ.
	portDef := append([]byte("Connected to TelnetServer\r"), 0xFF, 0xFF)
	portDef = append(portDef, []byte("9|0 Mail Monitor|1 Telnet|2 VHF|3 HF|")...)
	monFrame := []byte{0xFF, 0x1B, 91, 'K', 'U', '0', 'H', 'N', '>', 'A', 'P', 'R', 'S', 0xFE}
	data := append(portDef, monFrame...)

	frames := parseMonitorFrames(data)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].dir != "tx" {
		t.Errorf("dir: got %q, want tx", frames[0].dir)
	}
	if frames[0].line != "N0CALL>APRS" {
		t.Errorf("line: got %q, want \"N0CALL>APRS\"", frames[0].line)
	}
}
