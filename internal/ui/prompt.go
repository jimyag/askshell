package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jimyag/askshell/internal/assistant"
)

type Analyzer interface {
	AnalyzeStream(
		ctx context.Context,
		query, shell, cwd string,
		emit func(text string) error,
	) (assistant.Answer, error)
}

type Action string

const (
	ActionExecute Action = "execute"
	ActionEdit    Action = "edit"
	ActionReject  Action = "reject"
)

type Result struct {
	Action  Action
	Command string
}

// Prompt runs the question/candidate flow on the terminal. Shell
// integration points in/out at /dev/tty so the interactive read lives on
// the same descriptor the user is typing on. ASKSHELL_PICK pre-selects a
// command for automation; ASKSHELL_CONFIRM_TIMEOUT (Go duration string,
// independent of ASKSHELL_TIMEOUT which governs the LLM call) bounds the
// wait on each user decision and falls back to a reject on expiry.
func Prompt(
	ctx context.Context,
	service Analyzer,
	query string,
	shell string,
	cwd string,
	in io.Reader,
	out io.Writer,
) (Result, error) {
	reader := bufio.NewReader(in)
	text := messagesFor(os.Getenv("ASKSHELL_LANG"))
	timeout, err := pickConfirmTimeout(os.Getenv("ASKSHELL_CONFIRM_TIMEOUT"))
	if err != nil {
		return Result{Action: ActionReject}, fmt.Errorf("parse ASKSHELL_TIMEOUT: %w", err)
	}

	query = strings.TrimSpace(query)
	if query == "" {
		if err := printTo(out, text.you); err != nil {
			return Result{Action: ActionReject}, err
		}
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return Result{Action: ActionReject}, fmt.Errorf("read question: %w", err)
		}
		query = strings.TrimSpace(line)
		if query == "" {
			return Result{Action: ActionReject}, nil
		}
	}

	if err := printlnTo(out, text.thinking); err != nil {
		return Result{Action: ActionReject}, err
	}
	answer, err := service.AnalyzeStream(ctx, query, shell, cwd, func(chunk string) error {
		return printTo(out, chunk)
	})
	if err != nil {
		return Result{Action: ActionReject}, err
	}

	if answer.ReasoningSummary != "" {
		if err := linesTo(out, "", text.reasoningSummary, answer.ReasoningSummary); err != nil {
			return Result{Action: ActionReject}, err
		}
	}
	if answer.Answer != "" {
		if err := linesTo(out, "", answer.Answer); err != nil {
			return Result{Action: ActionReject}, err
		}
	}
	if len(answer.Commands) == 0 {
		return Result{Action: ActionReject}, nil
	}
	if err := printlnTo(out); err != nil {
		return Result{Action: ActionReject}, err
	}
	for index, candidate := range answer.Commands {
		if err := printfTo(out, "%d. %s\n", index+1, candidate.Command); err != nil {
			return Result{Action: ActionReject}, err
		}
		if candidate.Description != "" {
			if err := printfTo(out, "   %s\n", candidate.Description); err != nil {
				return Result{Action: ActionReject}, err
			}
		}
		if risk := riskText(candidate.Risk, text); risk != "" {
			if err := printfTo(out, "   "+text.risk, risk); err != nil {
				return Result{Action: ActionReject}, err
			}
		}
	}

	selected, action, err := pickCommand(reader, in, out, answer.Commands, text, timeout)
	if err != nil {
		return Result{Action: ActionReject}, err
	}
	return Result{Action: action, Command: selected.Command}, nil
}

func WriteResult(dir string, result Result) error {
	if dir == "" {
		return fmt.Errorf("result directory is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("open result directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("result path is not a directory")
	}
	if err := os.WriteFile(filepath.Join(dir, "action"), []byte(result.Action+"\n"), 0o600); err != nil {
		return fmt.Errorf("write action: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "command"), []byte(result.Command), 0o600); err != nil {
		return fmt.Errorf("write command: %w", err)
	}
	return nil
}

// pickCommand resolves the user's choice. Presets from ASKSHELL_PICK skip
// the prompt entirely; single low-risk candidates use a short prompt (Enter
// accepts, "e" edits); everything else falls into the numbered picker.
func pickCommand(
	reader *bufio.Reader,
	in io.Reader,
	out io.Writer,
	commands []assistant.Command,
	text messages,
	timeout time.Duration,
) (assistant.Command, Action, error) {
	if index, mode, ok := pickFromEnv(os.Getenv("ASKSHELL_PICK"), len(commands)); ok {
		if mode < 0 {
			return assistant.Command{}, ActionReject, nil
		}
		if mode > 0 {
			return commands[index], ActionEdit, nil
		}
		return commands[index], ActionExecute, nil
	}
	if len(commands) == 1 && commands[0].Risk == assistant.RiskNone {
		return askSingle(reader, in, out, commands[0], text, timeout)
	}
	return askChoice(reader, in, out, commands, text, timeout)
}

func askSingle(
	reader *bufio.Reader,
	in io.Reader,
	out io.Writer,
	candidate assistant.Command,
	text messages,
	timeout time.Duration,
) (assistant.Command, Action, error) {
	if err := printTo(out, text.selectSingle); err != nil {
		return assistant.Command{}, ActionReject, err
	}
	value, err := readChoiceTimeout(reader, in, out, text, timeout)
	if err != nil {
		return assistant.Command{}, ActionReject, err
	}
	switch value {
	case "", "y", "yes", "是", "确认", "放行":
		return candidate, ActionExecute, nil
	case "e", "edit", "编辑":
		return candidate, ActionEdit, nil
	default:
		return assistant.Command{}, ActionReject, nil
	}
}

func askChoice(
	reader *bufio.Reader,
	in io.Reader,
	out io.Writer,
	commands []assistant.Command,
	text messages,
	timeout time.Duration,
) (assistant.Command, Action, error) {
	for {
		if err := printfTo(out, text.selectCommand, len(commands)); err != nil {
			return assistant.Command{}, ActionReject, err
		}
		value, err := readChoiceTimeout(reader, in, out, text, timeout)
		if err != nil {
			return assistant.Command{}, ActionReject, err
		}
		if value == "" {
			value = "1"
		}
		mode := ActionExecute
		switch {
		case strings.HasPrefix(value, "e"), strings.HasPrefix(value, "edit"):
			mode = ActionEdit
			value = strings.TrimPrefix(value, "e")
			value = strings.TrimPrefix(value, "edit")
		case strings.HasPrefix(value, "编辑"):
			mode = ActionEdit
			value = strings.TrimPrefix(value, "编辑")
		}
		if isReject(value) {
			return assistant.Command{}, ActionReject, nil
		}
		index, parseErr := strconv.Atoi(value)
		if parseErr == nil && index >= 1 && index <= len(commands) {
			return commands[index-1], mode, nil
		}
		if err := printlnTo(out, text.invalidSelection); err != nil {
			return assistant.Command{}, ActionReject, err
		}
	}
}

// readChoiceTimeout reads one line. When in is an *os.File and timeout is
// positive, an empty result means the user did not respond in time; treat
// that as a reject and surface it in-band so the shell prompt stays in sync.
func readChoiceTimeout(
	reader *bufio.Reader,
	in io.Reader,
	out io.Writer,
	text messages,
	timeout time.Duration,
) (string, error) {
	file, ok := in.(*os.File)
	if !ok || timeout <= 0 {
		return readChoice(reader)
	}
	line, err := readStringTimeout(reader, file, timeout)
	if err != nil {
		return "", err
	}
	if line == "" {
		if err := printlnTo(out, text.timeout); err != nil {
			return "", err
		}
		return "n", nil
	}
	return normalizeChoice(line), nil
}

func normalizeChoice(line string) string {
	line = strings.ReplaceAll(line, "\x1b[200~", "")
	line = strings.ReplaceAll(line, "\x1b[201~", "")
	line = strings.ReplaceAll(line, "^[[200~", "")
	line = strings.ReplaceAll(line, "^[[201~", "")
	return strings.ToLower(strings.TrimSpace(line))
}

func readChoice(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return normalizeChoice(line), nil
}

func isReject(value string) bool {
	switch value {
	case "n", "no", "否", "拒绝":
		return true
	default:
		return false
	}
}

func printTo(out io.Writer, values ...any) error {
	_, err := fmt.Fprint(out, values...)
	return err
}

func printlnTo(out io.Writer, values ...any) error {
	_, err := fmt.Fprintln(out, values...)
	return err
}

func linesTo(out io.Writer, lines ...string) error {
	for _, line := range lines {
		if err := printlnTo(out, line); err != nil {
			return err
		}
	}
	return nil
}

func printfTo(out io.Writer, format string, values ...any) error {
	_, err := fmt.Fprintf(out, format, values...)
	return err
}

func riskText(risk assistant.Risk, text messages) string {
	switch risk {
	case assistant.RiskDestructive:
		return text.riskDestructive
	case assistant.RiskPrivileged:
		return text.riskPrivilegedAction
	default:
		return ""
	}
}
