package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/jimyag/askshell/internal/assistant"
	"github.com/jimyag/askshell/internal/session"
)

const (
	maxStoredOutput = 1024 * 1024
	maxModelOutput  = 64 * 1024
)

type Request struct {
	Type     string `json:"type"`
	Query    string `json:"query,omitempty"`
	Shell    string `json:"shell,omitempty"`
	CWD      string `json:"cwd,omitempty"`
	Command  string `json:"command,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}

type Response struct {
	Answer *assistant.Answer `json:"answer,omitempty"`
	Error  string            `json:"error,omitempty"`
}

type Client struct {
	socket string
}

func NewClient(socket string) *Client {
	return &Client{socket: socket}
}

func (c *Client) Analyze(
	ctx context.Context,
	query string,
	activeShell string,
	cwd string,
) (assistant.Answer, error) {
	response, err := c.call(ctx, Request{
		Type:  "analyze",
		Query: query,
		Shell: activeShell,
		CWD:   cwd,
	})
	if err != nil {
		return assistant.Answer{}, err
	}
	if response.Answer == nil {
		return assistant.Answer{}, fmt.Errorf("control server returned no answer")
	}
	return *response.Answer, nil
}

func (c *Client) Event(ctx context.Context, request Request) error {
	_, err := c.call(ctx, request)
	return err
}

func (c *Client) call(ctx context.Context, request Request) (Response, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.socket)
	if err != nil {
		return Response{}, fmt.Errorf("connect to askshell session: %w", err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return Response{}, fmt.Errorf("set control deadline: %w", err)
		}
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, fmt.Errorf("send control request: %w", err)
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, fmt.Errorf("read control response: %w", err)
	}
	if response.Error != "" {
		return Response{}, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}

type Server struct {
	socket  string
	service *assistant.Service
	store   *session.Store

	mu      sync.Mutex
	active  *capture
	start   string
	finish  *int
	pending []byte
}

type capture struct {
	command     string
	output      boundedOutput
	interactive bool
}

func NewServer(socket string, service *assistant.Service, store *session.Store) *Server {
	return &Server{socket: socket, service: service, store: store}
}

func (s *Server) Serve(ctx context.Context, ready chan<- error) error {
	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		ready <- err
		return fmt.Errorf("listen on control socket: %w", err)
	}
	defer listener.Close()
	if err := os.Chmod(s.socket, 0o600); err != nil {
		ready <- err
		return fmt.Errorf("protect control socket: %w", err)
	}
	ready <- nil

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept control connection: %w", acceptErr)
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	var request Request
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: fmt.Sprintf("decode request: %v", err)})
		return
	}
	response := s.dispatch(request)
	_ = json.NewEncoder(conn).Encode(response)
}

func (s *Server) dispatch(request Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch request.Type {
	case "analyze":
		answer, err := s.service.Analyze(
			context.Background(),
			request.Query,
			request.Shell,
			request.CWD,
		)
		if err != nil {
			return Response{Error: err.Error()}
		}
		if err := s.store.Save(s.service.Conversation()); err != nil {
			return Response{Error: err.Error()}
		}
		return Response{Answer: &answer}
	case "start":
		s.start = request.Command
		return Response{}
	case "cancel":
		s.active = nil
		s.start = ""
		s.finish = nil
		s.pending = nil
		return Response{}
	case "finish":
		s.finish = &request.ExitCode
		return Response{}
	default:
		return Response{Error: fmt.Sprintf("unknown control request %q", request.Type)}
	}
}

func (s *Server) StartMarker() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.start == "" {
		return
	}
	s.active = &capture{command: s.start}
	s.start = ""
	s.finish = nil
	s.pending = nil
}

func (s *Server) FinishMarker() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finish == nil {
		return nil
	}
	exitCode := *s.finish
	s.finish = nil
	return s.finishCapture(exitCode)
}

func (s *Server) CaptureOutput(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || s.active.interactive {
		return
	}
	s.active.output.Write(data)
	combined := append(append(s.pending[:0], s.pending...), data...)
	if hasAlternateScreen(combined) {
		s.active.interactive = true
		s.active.output.Reset()
	}
	if len(combined) > 32 {
		combined = combined[len(combined)-32:]
	}
	s.pending = append(s.pending[:0], combined...)
}

func (s *Server) ObserveInput() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		s.active.interactive = true
		s.active.output.Reset()
	}
}

func (s *Server) finishCapture(exitCode int) error {
	if s.active == nil {
		return nil
	}
	current := s.active
	s.active = nil
	s.pending = nil

	output := ""
	if current.interactive {
		output = "[interactive output omitted]"
	} else {
		output = strings.TrimSpace(cleanTerminalOutput(limitBytes(
			current.output.Bytes(),
			maxModelOutput,
		)))
	}
	conversation := s.service.Conversation()
	execution := assistant.Execution{
		Command:  current.command,
		Output:   output,
		ExitCode: exitCode,
	}
	if len(conversation.Turns) == 0 {
		conversation.PendingExecutions = append(conversation.PendingExecutions, execution)
	} else {
		last := len(conversation.Turns) - 1
		conversation.Turns[last].Executions = append(
			conversation.Turns[last].Executions,
			execution,
		)
	}
	s.service.SetConversation(conversation)
	return s.store.Save(conversation)
}

func Marker(sessionID, event string) []byte {
	return []byte("\x1b]777;askshell;" + sessionID + ";" + event + "\x07")
}

type boundedOutput struct {
	head      []byte
	tail      []byte
	truncated bool
}

func (b *boundedOutput) Write(data []byte) {
	headLimit := maxStoredOutput / 4
	tailLimit := maxStoredOutput - headLimit
	if len(b.head) < headLimit {
		count := min(len(data), headLimit-len(b.head))
		b.head = append(b.head, data[:count]...)
		data = data[count:]
	}
	if len(data) == 0 {
		return
	}
	b.tail = append(b.tail, data...)
	if len(b.tail) > tailLimit {
		b.tail = append(b.tail[:0], b.tail[len(b.tail)-tailLimit:]...)
		b.truncated = true
	}
}

func (b *boundedOutput) Bytes() []byte {
	result := append([]byte(nil), b.head...)
	if b.truncated {
		result = append(result, []byte("\n... askshell: output truncated ...\n")...)
	}
	result = append(result, b.tail...)
	return result
}

func (b *boundedOutput) Reset() {
	b.head = nil
	b.tail = nil
	b.truncated = false
}

func limitBytes(data []byte, limit int) []byte {
	if len(data) <= limit {
		return data
	}
	head := limit / 4
	tail := limit - head
	result := append([]byte(nil), data[:head]...)
	result = append(result, []byte("\n... askshell: output truncated for model ...\n")...)
	result = append(result, data[len(data)-tail:]...)
	return result
}

func hasAlternateScreen(data []byte) bool {
	text := string(data)
	return strings.Contains(text, "\x1b[?1049h") ||
		strings.Contains(text, "\x1b[?1047h") ||
		strings.Contains(text, "\x1b[?47h")
}

func cleanTerminalOutput(data []byte) string {
	var result []byte
	for index := 0; index < len(data); {
		if data[index] != 0x1b {
			if data[index] != '\r' {
				result = append(result, data[index])
			}
			index++
			continue
		}
		index++
		if index >= len(data) {
			break
		}
		switch data[index] {
		case '[':
			index++
			for index < len(data) {
				value := data[index]
				index++
				if value >= 0x40 && value <= 0x7e {
					break
				}
			}
		case ']':
			index++
			for index < len(data) {
				if data[index] == 0x07 {
					index++
					break
				}
				if data[index] == 0x1b && index+1 < len(data) && data[index+1] == '\\' {
					index += 2
					break
				}
				index++
			}
		default:
			index++
		}
	}
	lines := strings.Split(string(result), "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != "%" {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}
