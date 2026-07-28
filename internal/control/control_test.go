package control

import (
	"bytes"
	"strings"
	"testing"
)

func TestBoundedOutputKeepsHeadAndTail(t *testing.T) {
	var output boundedOutput
	data := append(
		bytes.Repeat([]byte("h"), maxStoredOutput/4),
		bytes.Repeat([]byte("x"), maxStoredOutput)...,
	)
	data = append(data, bytes.Repeat([]byte("t"), 32)...)
	output.Write(data)

	got := output.Bytes()
	if !bytes.HasPrefix(got, bytes.Repeat([]byte("h"), maxStoredOutput/4)) {
		t.Fatal("bounded output did not preserve its head")
	}
	if !bytes.HasSuffix(got, bytes.Repeat([]byte("t"), 32)) {
		t.Fatal("bounded output did not preserve its tail")
	}
	if !bytes.Contains(got, []byte("output truncated")) {
		t.Fatal("bounded output did not mark truncation")
	}
}

func TestCleanTerminalOutputRemovesControlSequencesAndPromptSpacer(t *testing.T) {
	input := []byte("\x1b[32mresult\x1b[0m\r\n\x1b[7m%   \x1b[0m\r")
	got := strings.TrimSpace(cleanTerminalOutput(input))
	if got != "result" {
		t.Fatalf("cleanTerminalOutput() = %q, want %q", got, "result")
	}
}

func TestMarkerIncludesSessionAndEvent(t *testing.T) {
	got := string(Marker("session-1", "start"))
	want := "\x1b]777;askshell;session-1;start\x07"
	if got != want {
		t.Fatalf("Marker() = %q, want %q", got, want)
	}
}
