package supervisor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"github.com/jimyag/askshell/internal/assistant"
	"github.com/jimyag/askshell/internal/config"
	"github.com/jimyag/askshell/internal/control"
	"github.com/jimyag/askshell/internal/session"
	"golang.org/x/term"
)

type Session struct {
	ID     string
	Socket string
	dir    string
	server *control.Server
	cancel context.CancelFunc
	done   chan error
}

func StartSession() (*Session, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	service, err := assistant.New(cfg)
	if err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	store, err := session.Open(id)
	if err != nil {
		return nil, err
	}
	conversation, err := store.Load()
	if err != nil {
		return nil, err
	}
	service.SetConversation(conversation)

	dir, err := os.MkdirTemp("/tmp", "askshell-")
	if err != nil {
		return nil, fmt.Errorf("create session runtime directory: %w", err)
	}
	//nolint:gosec // A private directory needs the owner execute bit for socket access.
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("protect session runtime directory: %w", err)
	}
	socket := filepath.Join(dir, "control.sock")
	server := control.NewServer(socket, service, store)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ready)
	}()

	if serveErr := <-ready; serveErr != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, serveErr
	}
	return &Session{
		ID:     id,
		Socket: socket,
		dir:    dir,
		server: server,
		cancel: cancel,
		done:   done,
	}, nil
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.cancel()
	err := <-s.done
	removeErr := os.RemoveAll(s.dir)
	return errors.Join(err, removeErr)
}

func (s *Session) Run(command *exec.Cmd) error {
	command.Env = command.Environ()
	command.Env = setEnv(command.Env, "ASKSHELL_ACTIVE", "1")
	command.Env = setEnv(command.Env, "ASKSHELL_SESSION_ID", s.ID)
	command.Env = setEnv(command.Env, "ASKSHELL_SOCKET", s.Socket)
	if binary, err := os.Executable(); err == nil {
		binaryDir := filepath.Dir(binary)
		command.Env = setEnv(
			command.Env,
			"PATH",
			binaryDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
	}

	ptmx, err := pty.Start(command)
	if err != nil {
		return fmt.Errorf("start terminal: %w", err)
	}
	defer func() {
		_ = ptmx.Close()
	}()

	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)
	go func() {
		for range resize {
			_ = pty.InheritSize(os.Stdin, ptmx)
		}
	}()
	resize <- syscall.SIGWINCH

	var previousState *term.State
	if term.IsTerminal(int(os.Stdin.Fd())) {
		previousState, err = term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("enable terminal raw mode: %w", err)
		}
		defer func() {
			_ = term.Restore(int(os.Stdin.Fd()), previousState)
		}()
	}

	inputDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			count, readErr := os.Stdin.Read(buffer)
			if count > 0 {
				s.server.ObserveInput()
				if _, writeErr := ptmx.Write(buffer[:count]); writeErr != nil {
					inputDone <- writeErr
					return
				}
			}
			if readErr != nil {
				inputDone <- readErr
				return
			}
		}
	}()

	outputErr := copyOutput(os.Stdout, ptmx, s.server, s.ID)
	waitErr := command.Wait()
	if outputErr != nil && !errors.Is(outputErr, io.EOF) &&
		!errors.Is(outputErr, syscall.EIO) {
		return fmt.Errorf("read terminal output: %w", outputErr)
	}
	if waitErr != nil {
		var exitError *exec.ExitError
		if errors.As(waitErr, &exitError) {
			return exitError
		}
		return fmt.Errorf("wait for terminal command: %w", waitErr)
	}
	return nil
}

func setEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func copyOutput(out io.Writer, ptmx *os.File, server *control.Server, sessionID string) error {
	filter := markerFilter{
		start:  control.Marker(sessionID, "start"),
		finish: control.Marker(sessionID, "finish"),
	}
	buffer := make([]byte, 32*1024)
	for {
		count, err := ptmx.Read(buffer)
		if count > 0 {
			data, filterErr := filter.Write(buffer[:count], server)
			if filterErr != nil {
				return filterErr
			}
			server.CaptureOutput(data)
			if _, writeErr := out.Write(data); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			data := filter.Flush()
			server.CaptureOutput(data)
			if len(data) > 0 {
				if _, writeErr := out.Write(data); writeErr != nil {
					return writeErr
				}
			}
			return err
		}
	}
}

type markerFilter struct {
	start  []byte
	finish []byte
	buffer []byte
}

func (f *markerFilter) Write(data []byte, server *control.Server) ([]byte, error) {
	f.buffer = append(f.buffer, data...)
	var output []byte
	for {
		startIndex := bytes.Index(f.buffer, f.start)
		finishIndex := bytes.Index(f.buffer, f.finish)
		index, event := firstMarker(startIndex, finishIndex)
		if index < 0 {
			keep := max(markerPrefix(f.buffer, f.start), markerPrefix(f.buffer, f.finish))
			output = append(output, f.buffer[:len(f.buffer)-keep]...)
			f.buffer = append(f.buffer[:0], f.buffer[len(f.buffer)-keep:]...)
			return output, nil
		}
		output = append(output, f.buffer[:index]...)
		switch event {
		case "start":
			server.CaptureOutput(output)
			output = output[:0]
			server.StartMarker()
			f.buffer = f.buffer[index+len(f.start):]
		case "finish":
			server.CaptureOutput(output)
			output = output[:0]
			if err := server.FinishMarker(); err != nil {
				return nil, err
			}
			f.buffer = f.buffer[index+len(f.finish):]
		}
	}
}

func (f *markerFilter) Flush() []byte {
	result := append([]byte(nil), f.buffer...)
	f.buffer = nil
	return result
}

func firstMarker(start, finish int) (int, string) {
	switch {
	case start >= 0 && (finish < 0 || start < finish):
		return start, "start"
	case finish >= 0:
		return finish, "finish"
	default:
		return -1, ""
	}
}

func markerPrefix(data, marker []byte) int {
	limit := min(len(data), len(marker)-1)
	for size := limit; size > 0; size-- {
		if bytes.Equal(data[len(data)-size:], marker[:size]) {
			return size
		}
	}
	return 0
}

func randomID() (string, error) {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return hex.EncodeToString(data), nil
}
