package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/jimyag/askshell/internal/config"
	"github.com/jimyag/askshell/internal/provider"
)

type Command struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Risk        Risk   `json:"risk,omitempty"`
}

type Risk string

const (
	RiskNone        Risk = ""
	RiskDestructive Risk = "destructive"
	RiskPrivileged  Risk = "privileged"
)

type Answer struct {
	ReasoningSummary string    `json:"reasoning_summary,omitempty"`
	Answer           string    `json:"answer"`
	Commands         []Command `json:"commands"`
}

type Turn struct {
	Query      string      `json:"query"`
	Answer     Answer      `json:"answer"`
	Execution  *Execution  `json:"execution,omitempty"`
	Executions []Execution `json:"executions,omitempty"`
}

type Execution struct {
	Command  string `json:"command"`
	Output   string `json:"output,omitempty"`
	ExitCode int    `json:"exit_code"`
}

type Conversation struct {
	Summary           string      `json:"summary,omitempty"`
	PendingExecutions []Execution `json:"pending_executions,omitempty"`
	Turns             []Turn      `json:"turns,omitempty"`
}

type Service struct {
	generator    provider.Generator
	conversation Conversation
}

func New(cfg config.Config) (*Service, error) {
	generator, err := provider.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Service{generator: generator}, nil
}

func (s *Service) SetConversation(conversation Conversation) {
	s.conversation = Conversation{
		Summary:           conversation.Summary,
		PendingExecutions: append([]Execution(nil), conversation.PendingExecutions...),
		Turns:             append([]Turn(nil), conversation.Turns...),
	}
}

func (s *Service) Conversation() Conversation {
	return Conversation{
		Summary:           s.conversation.Summary,
		PendingExecutions: append([]Execution(nil), s.conversation.PendingExecutions...),
		Turns:             append([]Turn(nil), s.conversation.Turns...),
	}
}

func (s *Service) Analyze(ctx context.Context, query, shell, cwd string) (Answer, error) {
	return s.AnalyzeEnvironment(ctx, query, Environment{
		OS:    runtime.GOOS,
		Arch:  runtime.GOARCH,
		Shell: shell,
		CWD:   cwd,
	})
}

type Environment struct {
	OS    string
	Arch  string
	Shell string
	CWD   string
}

func (s *Service) AnalyzeEnvironment(
	ctx context.Context,
	query string,
	environment Environment,
) (Answer, error) {
	if err := s.compactConversation(ctx); err != nil {
		return Answer{}, err
	}
	environmentText := fmt.Sprintf(
		"OS: %s/%s\nShell: %s\nWorking directory: %s",
		environment.OS,
		environment.Arch,
		environment.Shell,
		environment.CWD,
	)
	generated, err := s.generator.Generate(ctx, provider.Request{
		Instructions: `You are a concise terminal assistant.
Answer the request using the supplied environment context.
Provide a short reasoning_summary in the user's language. It should state the relevant evidence and why the answer or command follows, without exposing hidden chain-of-thought or exhaustive internal reasoning.
When commands would help, return exact shell commands as separate candidates.
Never claim a command was executed. Never place commentary inside a command.
Prefer one simple command over a script.
Every command will be reviewed by the user before execution.
Return only the requested JSON.`,
		Input:            buildInput(environmentText, s.conversation, query),
		Schema:           answerSchema(),
		ReasoningSummary: true,
	})
	if err != nil {
		return Answer{}, err
	}

	var result Answer
	if err := json.Unmarshal([]byte(generated.Text), &result); err != nil {
		return Answer{}, fmt.Errorf("decode assistant output: %w", err)
	}
	result.ReasoningSummary = strings.TrimSpace(result.ReasoningSummary)
	if summary := strings.TrimSpace(generated.ReasoningSummary); summary != "" {
		result.ReasoningSummary = summary
	}
	result.Answer = strings.TrimSpace(result.Answer)
	filtered := result.Commands[:0]
	for _, candidate := range result.Commands {
		candidate.Command = strings.TrimSpace(candidate.Command)
		candidate.Description = strings.TrimSpace(candidate.Description)
		if candidate.Command != "" {
			candidate.Risk = ClassifyRisk(candidate.Command)
			filtered = append(filtered, candidate)
		}
	}
	result.Commands = filtered
	if result.Answer == "" && len(result.Commands) == 0 {
		return Answer{}, fmt.Errorf("assistant returned no answer or command")
	}
	s.conversation.Turns = append(s.conversation.Turns, Turn{Query: query, Answer: result})
	return result, nil
}

func (s *Service) compactConversation(ctx context.Context) error {
	const compactAtChars = 12000
	if conversationSize(s.conversation) <= compactAtChars ||
		(len(s.conversation.Turns) == 0 && len(s.conversation.PendingExecutions) == 0) {
		return nil
	}

	generated, err := s.generator.Generate(ctx, provider.Request{
		Instructions: `Compress the supplied terminal-assistant conversation.
Preserve user goals, decisions, constraints, paths, identifiers, errors, and commands that may matter later.
Remove repetition and transient wording.
Return only the requested JSON.`,
		Input:  buildCompactionInput(s.conversation),
		Schema: summarySchema(),
	})
	if err != nil {
		return fmt.Errorf("compact conversation: %w", err)
	}
	var compacted struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(generated.Text), &compacted); err != nil {
		return fmt.Errorf("decode compacted conversation: %w", err)
	}
	compacted.Summary = strings.TrimSpace(compacted.Summary)
	if compacted.Summary == "" {
		return fmt.Errorf("conversation compaction returned an empty summary")
	}
	s.conversation.Summary = compacted.Summary
	s.conversation.PendingExecutions = nil
	s.conversation.Turns = nil
	return nil
}

func buildInput(environment string, conversation Conversation, query string) string {
	var transcript strings.Builder
	if conversation.Summary != "" {
		fmt.Fprintf(&transcript, "Compacted earlier context:\n%s\n\n", conversation.Summary)
	}
	for index := range conversation.PendingExecutions {
		writeExecution(&transcript, &conversation.PendingExecutions[index])
	}
	if len(conversation.PendingExecutions) > 0 {
		transcript.WriteString("\n")
	}
	for _, turn := range conversation.Turns {
		fmt.Fprintf(&transcript, "User: %s\nAssistant: %s\n", turn.Query, turn.Answer.Answer)
		if turn.Answer.ReasoningSummary != "" {
			fmt.Fprintf(&transcript, "Assistant reasoning summary: %s\n", turn.Answer.ReasoningSummary)
		}
		for _, command := range turn.Answer.Commands {
			fmt.Fprintf(&transcript, "Suggested command: %s\n", command.Command)
		}
		writeExecution(&transcript, turn.Execution)
		for index := range turn.Executions {
			writeExecution(&transcript, &turn.Executions[index])
		}
		transcript.WriteString("\n")
	}

	var input strings.Builder
	input.WriteString(environment)
	if transcript.Len() > 0 {
		input.WriteString("\n\nConversation history, oldest first:\n")
		input.WriteString(transcript.String())
	}
	input.WriteString("\n\nCurrent request:\n")
	input.WriteString(query)
	return input.String()
}

func buildCompactionInput(conversation Conversation) string {
	var input strings.Builder
	if conversation.Summary != "" {
		fmt.Fprintf(&input, "Existing summary:\n%s\n\n", conversation.Summary)
	}
	input.WriteString("Events to compact:\n")
	for index := range conversation.PendingExecutions {
		writeExecution(&input, &conversation.PendingExecutions[index])
	}
	for _, turn := range conversation.Turns {
		fmt.Fprintf(&input, "User: %s\nAssistant: %s\n", turn.Query, turn.Answer.Answer)
		if turn.Answer.ReasoningSummary != "" {
			fmt.Fprintf(&input, "Assistant reasoning summary: %s\n", turn.Answer.ReasoningSummary)
		}
		for _, command := range turn.Answer.Commands {
			fmt.Fprintf(&input, "Suggested command: %s\n", command.Command)
		}
		writeExecution(&input, turn.Execution)
		for index := range turn.Executions {
			writeExecution(&input, &turn.Executions[index])
		}
		input.WriteString("\n")
	}
	return input.String()
}

func writeExecution(out *strings.Builder, execution *Execution) {
	if execution == nil {
		return
	}
	fmt.Fprintf(out, "Executed command: %s\nExit code: %d\n", execution.Command, execution.ExitCode)
	if execution.Output != "" {
		fmt.Fprintf(out, "Command output:\n%s\n", execution.Output)
	}
}

func conversationSize(conversation Conversation) int {
	size := len(conversation.Summary)
	for _, execution := range conversation.PendingExecutions {
		size += len(execution.Command) + len(execution.Output)
	}
	for _, turn := range conversation.Turns {
		size += len(turn.Query) + len(turn.Answer.Answer) + len(turn.Answer.ReasoningSummary)
		for _, command := range turn.Answer.Commands {
			size += len(command.Command) + len(command.Description)
		}
		if turn.Execution != nil {
			size += len(turn.Execution.Command) + len(turn.Execution.Output)
		}
		for _, execution := range turn.Executions {
			size += len(execution.Command) + len(execution.Output)
		}
	}
	return size
}

func summarySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{"type": "string"},
		},
		"required":             []string{"summary"},
		"additionalProperties": false,
	}
}

func answerSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"reasoning_summary": map[string]any{"type": "string"},
			"answer":            map[string]any{"type": "string"},
			"commands": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command":     map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
					},
					"required":             []string{"command", "description"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"reasoning_summary", "answer", "commands"},
		"additionalProperties": false,
	}
}
