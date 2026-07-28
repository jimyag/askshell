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

	"github.com/jimyag/askshell/internal/assistant"
)

type Analyzer interface {
	Analyze(ctx context.Context, query, shell, cwd string) (assistant.Answer, error)
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
	answer, err := service.Analyze(ctx, query, shell, cwd)
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
		if risk := riskLabel(candidate.Command, text); risk != "" {
			if err := printfTo(out, "   "+text.risk, risk); err != nil {
				return Result{Action: ActionReject}, err
			}
		}
	}

	selected, rejected, err := selectCommand(reader, out, answer.Commands, text)
	if err != nil {
		return Result{Action: ActionReject}, err
	}
	if rejected {
		return Result{Action: ActionReject}, nil
	}

	action, err := selectAction(reader, out, text)
	if err != nil {
		return Result{Action: ActionReject}, err
	}
	return Result{
		Action:  action,
		Command: selected.Command,
	}, nil
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

func selectCommand(
	reader *bufio.Reader,
	out io.Writer,
	commands []assistant.Command,
	text messages,
) (assistant.Command, bool, error) {
	if len(commands) == 1 {
		return commands[0], false, nil
	}

	for {
		if err := printfTo(out, text.selectCommand, len(commands)); err != nil {
			return assistant.Command{}, false, err
		}
		value, err := readChoice(reader)
		if err != nil {
			return assistant.Command{}, false, err
		}
		if isReject(value) {
			return assistant.Command{}, true, nil
		}
		index, err := strconv.Atoi(value)
		if err == nil && index >= 1 && index <= len(commands) {
			return commands[index-1], false, nil
		}
		if err := printlnTo(out, text.invalidSelection); err != nil {
			return assistant.Command{}, false, err
		}
	}
}

func selectAction(reader *bufio.Reader, out io.Writer, text messages) (Action, error) {
	for {
		if err := printTo(out, text.selectAction); err != nil {
			return ActionReject, err
		}
		value, err := readChoice(reader)
		if err != nil {
			return ActionReject, err
		}
		switch value {
		case "y", "yes", "是", "确认", "放行":
			return ActionExecute, nil
		case "n", "no", "否", "拒绝":
			return ActionReject, nil
		default:
			if err := printlnTo(out, text.invalidSelection); err != nil {
				return ActionReject, err
			}
		}
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

func readChoice(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	line = strings.ReplaceAll(line, "\x1b[200~", "")
	line = strings.ReplaceAll(line, "\x1b[201~", "")
	line = strings.ReplaceAll(line, "^[[200~", "")
	line = strings.ReplaceAll(line, "^[[201~", "")
	return strings.ToLower(strings.TrimSpace(line)), nil
}

func isReject(value string) bool {
	switch value {
	case "n", "no", "否", "拒绝":
		return true
	default:
		return false
	}
}

func riskLabel(command string, text messages) string {
	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, "rm -rf"),
		strings.Contains(lower, "mkfs"),
		strings.Contains(lower, "diskutil erase"),
		strings.Contains(lower, "> /dev/"):
		return text.riskDestructive
	case strings.Contains(lower, "sudo "),
		strings.Contains(lower, "shutdown"),
		strings.Contains(lower, "reboot"):
		return text.riskPrivilegedAction
	default:
		return ""
	}
}
