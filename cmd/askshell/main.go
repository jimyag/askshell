package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jimyag/askshell/internal/assistant"
	"github.com/jimyag/askshell/internal/config"
	"github.com/jimyag/askshell/internal/control"
	"github.com/jimyag/askshell/internal/remote"
	"github.com/jimyag/askshell/internal/session"
	"github.com/jimyag/askshell/internal/shell"
	"github.com/jimyag/askshell/internal/supervisor"
	"github.com/jimyag/askshell/internal/ui"
)

var version = "0.1.0-dev"

const eventSyncTimeout = 5 * time.Second

func main() {
	if err := run(os.Args[1:]); err != nil {
		if exitError, ok := errors.AsType[*exec.ExitError](err); ok {
			os.Exit(exitError.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "askshell:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runShell(nil)
	}

	switch args[0] {
	case "shell":
		return runShell(args[1:])
	case "ask":
		return runAsk(args[1:])
	case "fix":
		return runFix(args[1:])
	case "record":
		return runRecord(args[1:])
	case "event":
		return runEvent(args[1:])
	case "ssh":
		return runSSH(args[1:])
	case "init":
		return runInit(args[1:])
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		return printHelp(os.Stdout)
	default:
		return fmt.Errorf("unknown command %q; run 'askshell help'", args[0])
	}
}

func runSSH(args []string) (returnErr error) {
	activeSession, err := supervisor.StartSession()
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, activeSession.Close())
	}()
	return remote.RunSSH(args, activeSession)
}

func runShell(args []string) (returnErr error) {
	var backendArg string
	if len(args) > 1 {
		return fmt.Errorf("usage: askshell shell [zsh|bash|fish]")
	}
	if len(args) == 1 {
		backendArg = args[0]
	}
	backend, err := shell.ResolveBackend(backendArg)
	if err != nil {
		return err
	}
	command, err := shell.Command(backend)
	if err != nil {
		return err
	}
	activeSession, err := supervisor.StartSession()
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, activeSession.Close())
	}()
	return activeSession.Run(command)
}

func runAsk(args []string) error {
	flags := flag.NewFlagSet("ask", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	resultDir := flags.String("result-dir", "", "directory used to return the selected action")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *resultDir == "" {
		return fmt.Errorf("ask is an internal command; start 'askshell shell' and use '? question'")
	}
	query := strings.Join(flags.Args(), " ")
	if strings.TrimSpace(query) == "" {
		query = "Look at the recent commands in this terminal session. " +
			"If any of them failed or produced an obvious error, briefly explain why and suggest the corrected command. " +
			"Otherwise summarize what the recent commands accomplished and suggest one or two concrete next steps the user likely wants."
	}
	return runPrompt(*resultDir, query, false)
}

func runFix(args []string) error {
	flags := flag.NewFlagSet("fix", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	resultDir := flags.String("result-dir", "", "directory used to return the selected action")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *resultDir == "" || len(flags.Args()) != 0 {
		return fmt.Errorf("fix is an internal command")
	}
	return runPrompt(
		*resultDir,
		"Diagnose the most recent failed command. Identify the exact erroneous fragment "+
			"when possible, briefly explain the cause, and suggest the corrected command.",
		true,
	)
}

func runPrompt(resultDir, query string, confirmFailure bool) error {
	result := ui.Result{Action: ui.ActionReject}
	if err := ui.WriteResult(resultDir, result); err != nil {
		return err
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	if _, err := fmt.Fprint(tty, "\x1b[?2004l"); err != nil {
		_ = tty.Close()
		return fmt.Errorf("disable terminal bracketed paste: %w", err)
	}
	defer func() {
		_, _ = fmt.Fprint(tty, "\x1b[?2004h")
		_ = tty.Close()
	}()
	if confirmFailure {
		confirmed, confirmErr := ui.ConfirmFailure(tty, os.Getenv("ASKSHELL_LANG"))
		if confirmErr != nil {
			return fmt.Errorf("read failure action: %w", confirmErr)
		}
		if !confirmed {
			return nil
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	var service ui.Analyzer
	var store *session.Store
	if socket := os.Getenv("ASKSHELL_SOCKET"); socket != "" {
		service = control.NewClient(socket)
	} else {
		cfg, loadErr := config.Load()
		if loadErr != nil {
			return loadErr
		}
		localService, serviceErr := assistant.New(cfg)
		if serviceErr != nil {
			return serviceErr
		}
		store, err = session.Open(os.Getenv("ASKSHELL_SESSION_ID"))
		if err != nil {
			return err
		}
		conversation, loadErr := store.Load()
		if loadErr != nil {
			return loadErr
		}
		localService.SetConversation(conversation)
		service = localService
	}
	activeShell := os.Getenv("ASKSHELL_BACKEND")
	if activeShell == "" {
		activeShell = filepath.Base(os.Getenv("SHELL"))
	}
	result, err = ui.Prompt(
		context.Background(),
		service,
		query,
		activeShell,
		cwd,
		tty,
		tty,
	)
	if err != nil {
		return err
	}
	if localService, ok := service.(*assistant.Service); ok {
		if err := store.Save(localService.Conversation()); err != nil {
			return err
		}
	}
	return ui.WriteResult(resultDir, result)
}

func runEvent(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("event is an internal command")
	}
	socket := os.Getenv("ASKSHELL_SOCKET")
	if socket == "" {
		return fmt.Errorf("ASKSHELL_SOCKET is not set")
	}
	client := control.NewClient(socket)
	switch args[0] {
	case "start":
		flags := flag.NewFlagSet("event start", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := client.Event(context.Background(), control.Request{
			Type:    "start",
			Command: strings.Join(flags.Args(), " "),
		}); err != nil {
			return err
		}
		return writeEventMarker("start")
	case "cancel":
		if len(args) != 1 {
			return fmt.Errorf("usage: askshell event cancel")
		}
		return client.Event(context.Background(), control.Request{Type: "cancel"})
	case "finish":
		flags := flag.NewFlagSet("event finish", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		exitCode := flags.Int("exit-code", 0, "command exit code")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := client.Event(context.Background(), control.Request{
			Type:     "finish",
			ExitCode: *exitCode,
		}); err != nil {
			return err
		}
		if err := writeEventMarker("finish"); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), eventSyncTimeout)
		defer cancel()
		return client.Event(ctx, control.Request{Type: "sync"})
	default:
		return fmt.Errorf("unknown event %q", args[0])
	}
}

func writeEventMarker(event string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open terminal for event marker: %w", err)
	}
	defer func() {
		_ = tty.Close()
	}()
	if _, err := tty.Write(control.Marker(event)); err != nil {
		return fmt.Errorf("write terminal event marker: %w", err)
	}
	return nil
}

func runRecord(args []string) error {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputFile := flags.String("output-file", "", "file containing command output")
	exitCode := flags.Int("exit-code", 0, "command exit code")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) == 0 {
		return fmt.Errorf("record is an internal command")
	}

	var output []byte
	if *outputFile != "" {
		var err error
		output, err = os.ReadFile(*outputFile)
		if err != nil {
			return fmt.Errorf("read command output: %w", err)
		}
		output = limitOutput(output, 64*1024)
	}

	store, err := session.Open(os.Getenv("ASKSHELL_SESSION_ID"))
	if err != nil {
		return err
	}
	conversation, err := store.Load()
	if err != nil {
		return err
	}
	execution := assistant.Execution{
		Command:  strings.Join(flags.Args(), " "),
		Output:   strings.TrimSpace(string(output)),
		ExitCode: *exitCode,
	}
	if len(conversation.Turns) == 0 {
		conversation.PendingExecutions = append(conversation.PendingExecutions, execution)
	} else {
		last := len(conversation.Turns) - 1
		conversation.Turns[last].Executions = append(conversation.Turns[last].Executions, execution)
	}
	return store.Save(conversation)
}

func limitOutput(output []byte, limit int) []byte {
	if len(output) <= limit {
		return output
	}
	half := limit / 2
	truncated := []byte("\n... askshell: output truncated ...\n")
	result := make([]byte, 0, limit+len(truncated))
	result = append(result, output[:half]...)
	result = append(result, truncated...)
	result = append(result, output[len(output)-half:]...)
	return result
}

func runInit(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: askshell init <zsh|bash|fish>")
	}
	backend, err := shell.ResolveBackend(args[0])
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}
	integration, err := shell.Integration(binary, backend)
	if err != nil {
		return err
	}
	fmt.Print(integration)
	return nil
}

func printHelp(out io.Writer) error {
	_, err := fmt.Fprintln(out, `askshell - a normal shell with explicit AI assistance

Usage:
  askshell                    Start $SHELL with askshell enabled
  askshell shell [zsh|bash|fish]
                              Start the selected shell with askshell enabled
  askshell ssh [options] host Connect through SSH with local askshell context
  askshell init <zsh|bash|fish>
                              Print a shell integration script
  askshell version      Print the version

Inside askshell:
  git status        Runs directly in the current shell
  ? explain this    Sends the text after ? to the AI

AI command suggestions are never run automatically. Choose:
  y / yes  put the command into the terminal
  n / no   reject the command`)
	return err
}
