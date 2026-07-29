package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jimyag/askshell/internal/assistant"
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

func TestHandleCancelsAnalyzeWhenClientDisconnects(t *testing.T) {
	service := &blockingService{
		started:  make(chan assistant.Environment, 1),
		canceled: make(chan struct{}),
	}
	server := NewServer("", service, nil)
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handle(context.Background(), serverConn)
		close(done)
	}()

	if err := json.NewEncoder(clientConn).Encode(Request{
		Type:  "analyze",
		Query: "inspect memory",
		Shell: "zsh",
		CWD:   "/srv/app",
		OS:    "linux",
		Arch:  "x86_64",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case environment := <-service.started:
		if environment.OS != "linux" || environment.Arch != "x86_64" {
			t.Fatalf("environment = %#v", environment)
		}
	case <-time.After(time.Second):
		t.Fatal("analyze did not start")
	}
	_ = clientConn.Close()

	select {
	case <-service.canceled:
	case <-time.After(time.Second):
		t.Fatal("analyze was not canceled after disconnect")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("control handler did not exit")
	}
}

func TestControlEventsAreNotBlockedByAnalyze(t *testing.T) {
	service := &blockingService{
		started:  make(chan assistant.Environment, 1),
		canceled: make(chan struct{}),
	}
	server := NewServer("", service, nil)
	ctx, cancel := context.WithCancel(context.Background())
	analyzeDone := make(chan struct{})
	go func() {
		server.dispatch(ctx, Request{Type: "analyze"})
		close(analyzeDone)
	}()
	<-service.started

	eventDone := make(chan struct{})
	go func() {
		server.dispatch(context.Background(), Request{Type: "start", Command: "ls"})
		close(eventDone)
	}()
	select {
	case <-eventDone:
	case <-time.After(time.Second):
		t.Fatal("start event was blocked by analyze")
	}

	cancel()
	<-analyzeDone
}

func TestClientCancellationClosesPendingCall(t *testing.T) {
	//nolint:usetesting // t.TempDir is too long for a Unix socket path on macOS.
	dir, err := os.MkdirTemp("/tmp", "askshell-control-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	socket := filepath.Join(dir, "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
	}()
	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		var request Request
		_ = json.NewDecoder(conn).Decode(&request)
		close(accepted)
		var buffer [1]byte
		_, _ = conn.Read(buffer[:])
	}()

	ctx, cancel := context.WithCancel(context.Background())
	callDone := make(chan error, 1)
	go func() {
		_, callErr := NewClient(socket).call(ctx, Request{Type: "analyze"})
		callDone <- callErr
	}()
	<-accepted
	cancel()

	select {
	case callErr := <-callDone:
		if callErr == nil {
			t.Fatal("canceled call returned no error")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled client call did not return")
	}
}

type blockingService struct {
	started      chan assistant.Environment
	canceled     chan struct{}
	conversation assistant.Conversation
}

func (s *blockingService) AnalyzeEnvironment(
	ctx context.Context,
	_ string,
	environment assistant.Environment,
) (assistant.Answer, error) {
	s.started <- environment
	<-ctx.Done()
	close(s.canceled)
	return assistant.Answer{}, ctx.Err()
}

func (s *blockingService) Conversation() assistant.Conversation {
	return s.conversation
}

func (s *blockingService) SetConversation(conversation assistant.Conversation) {
	s.conversation = conversation
}
