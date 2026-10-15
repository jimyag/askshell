// askshell-remote is the temporary helper uploaded to a remote host by
// "askshell ssh". It plays the local askshell binary's role there: start an
// integrated shell, relay command events and ask/fix prompts over the
// reverse-forwarded control socket. Run by the parent supervisor; never
// invoked directly by users.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/term"
)

const eventSyncTimeout = 5 * time.Second

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "askshell:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("missing remote helper command")
	}
	switch args[0] {
	case "shell":
		return runShell(args[1:])
	case "event":
		return runEvent(args[1:])
	case "ask":
		return runAsk(args[1:])
	case "fix":
		return runFix(args[1:])
	default:
		return fmt.Errorf("unknown remote helper command %q", args[0])
	}
}

// rpc sends one control request and waits for the single response line.
func rpc(request map[string]any, timeout time.Duration) (map[string]any, error) {
	path := os.Getenv("ASKSHELL_SOCKET")
	if path == "" {
		return nil, errors.New("ASKSHELL_SOCKET is not set")
	}
	conn, err := net.DialTimeout("unix", path, 10*time.Second)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = conn.Close()
	}()
	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return nil, err
	}
	if message, _ := response["error"].(string); message != "" {
		return nil, errors.New(message)
	}
	return response, nil
}

func eventRequest(kind string, args []string) (map[string]any, error) {
	request := map[string]any{"type": kind}
	switch kind {
	case "start":
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		request["command"] = strings.Join(args, " ")
	case "finish":
		flags := flag.NewFlagSet("event finish", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		exitCode := flags.Int("exit-code", 0, "command exit code")
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		request["exit_code"] = *exitCode
	}
	return request, nil
}

func runEvent(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: askshell-remote event <start|cancel|finish>")
	}
	kind := args[0]
	request, err := eventRequest(kind, args[1:])
	if err != nil {
		return err
	}
	if _, err := rpc(request, 0); err != nil {
		return err
	}
	if kind == "start" || kind == "finish" {
		writeMarker(kind)
	}
	if kind == "finish" {
		if _, err := rpc(map[string]any{"type": "sync"}, eventSyncTimeout); err != nil {
			return err
		}
	}
	return nil
}

// writeMarker emits the start/finish OSC marker the local supervisor's PTY
// filter watches for. The supervisor sees every connection this binary runs
// in, so the marker carries no session routing information.
func writeMarker(event string) {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer func() {
		_ = tty.Close()
	}()
	_, _ = tty.Write([]byte("\x1b]777;askshell;" + event + "\x07"))
}

// Answer mirrors assistant.Answer on the local side.
type Answer struct {
	ReasoningSummary string    `json:"reasoning_summary,omitempty"`
	Answer           string    `json:"answer"`
	Commands         []Command `json:"commands"`
}

type Command struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Risk        string `json:"risk,omitempty"`
}

type helperText struct {
	thinking         string
	reasoningSummary string
	riskPrefix       string
	risks            map[string]string
	selectCommand    string
	selectAction     string
	invalidSelection string
}

func texts() helperText {
	if isChinese(os.Getenv("ASKSHELL_LANG")) {
		return helperText{
			thinking:         "AskShell 正在思考…",
			reasoningSummary: "分析摘要：",
			riskPrefix:       "   风险：",
			risks: map[string]string{
				"destructive": "可能删除或覆盖数据",
				"privileged":  "需要提权或影响系统状态",
			},
			selectCommand:    "选择命令 [1-%d]，或输入 n/否：",
			selectAction:     "操作：[y/是] 放入终端  [n/否] 拒绝：",
			invalidSelection: "无效选择。",
		}
	}
	return helperText{
		thinking:         "AskShell is thinking…",
		reasoningSummary: "Reasoning summary:",
		riskPrefix:       "   Risk: ",
		risks: map[string]string{
			"destructive": "may delete or overwrite data",
			"privileged":  "requires privileges or affects system state",
		},
		selectCommand:    "Select command [1-%d], or n/no to reject: ",
		selectAction:     "Action: [y/yes] put in terminal  [n/no] reject: ",
		invalidSelection: "Invalid selection.",
	}
}

func analyzeRequest(query string) map[string]any {
	shellName := os.Getenv("ASKSHELL_BACKEND")
	if shellName == "" {
		shellName = filepath.Base(os.Getenv("SHELL"))
	}
	cwd, _ := os.Getwd()
	return map[string]any{
		"type":  "analyze",
		"query": query,
		"shell": shellName,
		"cwd":   cwd,
		"os":    runtime.GOOS,
		"arch":  runtime.GOARCH,
	}
}

const fixQuery = "Diagnose the most recent failed command. Identify the exact erroneous fragment " +
	"when possible, briefly explain the cause, and suggest the corrected command."

func runAsk(args []string) error {
	flags := flag.NewFlagSet("ask", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	resultDir := flags.String("result-dir", "", "directory used to return the selected action")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *resultDir == "" {
		return errors.New("ask is an internal command")
	}
	if err := writeResult(*resultDir, "reject", ""); err != nil {
		return err
	}
	return promptToResult(*resultDir, strings.Join(flags.Args(), " "))
}

func runFix(args []string) error {
	flags := flag.NewFlagSet("fix", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	resultDir := flags.String("result-dir", "", "directory used to return the selected action")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *resultDir == "" || len(flags.Args()) != 0 {
		return errors.New("fix is an internal command")
	}
	if err := writeResult(*resultDir, "reject", ""); err != nil {
		return err
	}
	confirmed, err := confirmFailure()
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}
	return promptToResult(*resultDir, fixQuery)
}

func promptToResult(resultDir, query string) error {
	text := texts()
	if err := printlnTTY(text.thinking); err != nil {
		return err
	}
	response, err := rpc(analyzeRequest(query), 0)
	if err != nil {
		return err
	}
	var answer Answer
	raw, err := json.Marshal(response["answer"])
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return fmt.Errorf("decode assistant output: %w", err)
	}
	action, command := promptChoices(text, answer)
	return writeResult(resultDir, action, command)
}

// promptChoices renders the answer on /dev/tty and reads the user's picks.
// Unlike the local UI it stays line-based: the remote helper runs inside the
// target shell, so readline editing and redraws belong to that shell.
func promptChoices(text helperText, answer Answer) (string, string) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "reject", ""
	}
	defer func() {
		_ = tty.Close()
	}()
	out := func(format string, values ...any) {
		_, _ = fmt.Fprintf(tty, format, values...)
	}
	if answer.ReasoningSummary != "" {
		out("\n%s\n%s\n", text.reasoningSummary, answer.ReasoningSummary)
	}
	if answer.Answer != "" {
		out("\n%s\n", answer.Answer)
	}
	if len(answer.Commands) == 0 {
		return "reject", ""
	}
	out("\n")
	for index, candidate := range answer.Commands {
		out("%d. %s\n", index+1, candidate.Command)
		if candidate.Description != "" {
			out("   %s\n", candidate.Description)
		}
		if risk := text.risks[candidate.Risk]; risk != "" {
			out("%s%s\n", text.riskPrefix, risk)
		}
	}

	reader := bufio.NewReader(tty)
	selected := answer.Commands[0]
	if len(answer.Commands) > 1 {
		picked := false
		for !picked {
			out(text.selectCommand, len(answer.Commands))
			value := cleanChoice(reader)
			if isReject(value) {
				return "reject", ""
			}
			if index, parseErr := strconv.Atoi(value); parseErr == nil && index >= 1 && index <= len(answer.Commands) {
				selected = answer.Commands[index-1]
				picked = true
			} else {
				out("%s\n", text.invalidSelection)
			}
		}
	}
	for {
		out("%s", text.selectAction)
		value := cleanChoice(reader)
		if isAccept(value) {
			return "execute", selected.Command
		}
		if isReject(value) {
			return "reject", ""
		}
		out("%s\n", text.invalidSelection)
	}
}

// confirmFailure asks after a failed command whether to diagnose it. Click
// targets come from mouse reporting; Enter or d/y diagnoses, n or Ctrl-C
// skips the prompt.
func confirmFailure() (bool, error) {
	chinese := isChinese(os.Getenv("ASKSHELL_LANG"))
	diagnose, ignore, message := "Diagnose and fix", "Ignore", "The previous command failed."
	if chinese {
		diagnose, ignore, message = "诊断并修复", "忽略", "上一条命令执行失败。"
	}
	line := fmt.Sprintf("[D] %s    [N] %s", diagnose, ignore)
	diagnoseEnd := displayWidth("[D] "+diagnose) + 1
	ignoreStart := diagnoseEnd + 3

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = tty.Close()
	}()
	if _, err := fmt.Fprintf(tty, "\nAskShell: %s\n%s", message, line); err != nil {
		return false, err
	}
	state, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		if _, writeErr := fmt.Fprint(tty, ": "); writeErr != nil {
			return false, writeErr
		}
		value, scanErr := bufio.NewReader(tty).ReadString('\n')
		if scanErr != nil && value == "" {
			return false, scanErr
		}
		return isAccept(clean(value)), nil
	}
	defer func() {
		_, _ = fmt.Fprint(tty, "\x1b[?1000l\x1b[?1006l\r\n")
		_ = term.Restore(int(tty.Fd()), state)
	}()
	if _, err := fmt.Fprint(tty, "\x1b[?1000h\x1b[?1006h"); err != nil {
		return false, err
	}

	input := make([]byte, 0, 64)
	one := make([]byte, 1)
	for {
		count, readErr := tty.Read(one)
		if count > 0 {
			input = append(input, one[0])
			if len(input) > 64 {
				input = input[len(input)-64:]
			}
			if choice, ok := failureChoice(input, diagnoseEnd, ignoreStart); ok {
				return choice, nil
			}
			if sequenceComplete(input) {
				input = input[:0]
			}
		}
		if readErr != nil {
			return false, readErr
		}
	}
}

func failureChoice(input []byte, diagnoseEnd, ignoreStart int) (bool, bool) {
	if len(input) == 0 {
		return false, false
	}
	value := string(input)
	if start := strings.LastIndex(value, "\x1b[<"); start >= 0 {
		end := strings.IndexAny(value[start:], "Mm")
		if end < 0 {
			return false, false
		}
		parts := strings.Split(value[start+3:start+end], ";")
		if len(parts) != 3 || parts[0] != "0" || value[start+end] != 'M' {
			return false, false
		}
		column, err := strconv.Atoi(parts[1])
		if err != nil {
			return false, false
		}
		switch {
		case column >= 1 && column <= diagnoseEnd:
			return true, true
		case column >= ignoreStart:
			return false, true
		default:
			return false, false
		}
	}
	if strings.ContainsRune(value, '\x1b') {
		return false, false
	}
	switch input[len(input)-1] {
	case '\r', '\n', 'd', 'D', 'y', 'Y':
		return true, true
	case 'n', 'N', 3:
		return false, true
	default:
		return false, false
	}
}

// sequenceComplete reports whether the trailing escape sequence in input is
// finished and safe to discard.
func sequenceComplete(input []byte) bool {
	start := strings.LastIndex(string(input), "\x1b")
	if start < 0 || start == len(input)-1 {
		return false
	}
	sequence := input[start:]
	if sequence[1] != '[' {
		return true
	}
	if len(sequence) < 3 {
		return false
	}
	if sequence[2] == '<' {
		last := sequence[len(sequence)-1]
		return last == 'M' || last == 'm'
	}
	last := sequence[len(sequence)-1]
	return last >= 0x40 && last <= 0x7e
}

func writeResult(dir, action, command string) error {
	if err := os.WriteFile(filepath.Join(dir, "action"), []byte(action+"\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "command"), []byte(command), 0o600)
}

func printlnTTY(value string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = tty.Close()
	}()
	_, err = fmt.Fprintln(tty, value)
	return err
}

func cleanChoice(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return clean(line)
}

func clean(value string) string {
	value = strings.ReplaceAll(value, "\x1b[200~", "")
	value = strings.ReplaceAll(value, "\x1b[201~", "")
	value = strings.ReplaceAll(value, "^[[200~", "")
	value = strings.ReplaceAll(value, "^[[201~", "")
	return strings.ToLower(strings.TrimSpace(value))
}

func isAccept(value string) bool {
	switch value {
	case "y", "yes", "是", "确认", "放行":
		return true
	default:
		return false
	}
}

func isReject(value string) bool {
	switch value {
	case "n", "no", "否", "拒绝":
		return true
	default:
		return false
	}
}

func displayWidth(value string) int {
	width := 0
	for _, current := range value {
		if unicode.Is(unicode.Han, current) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func isChinese(language string) bool {
	language = strings.ToLower(strings.TrimSpace(language))
	return language == "zh" || strings.HasPrefix(language, "zh-") ||
		strings.HasPrefix(language, "zh_")
}

// runShell execs the remote user's shell with AskShell hooks: the generated
// rc sources the user's own rc first, then the integration script pointing
// back at this helper.
func runShell(_ []string) error {
	shellPath := os.Getenv("SHELL")
	backend := filepath.Base(shellPath)
	self, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		self = os.Args[0]
	}

	runtimeDir := os.Getenv("ASKSHELL_REMOTE_DIR")
	if runtimeDir == "" {
		created, err := os.MkdirTemp("", "askshell-")
		if err != nil {
			return err
		}
		runtimeDir = created
	} else {
		if err := os.MkdirAll(filepath.Join(runtimeDir, "rc"), 0o700); err != nil {
			return err
		}
	}
	defer func() {
		_ = os.RemoveAll(runtimeDir)
	}()

	rcDir := filepath.Join(runtimeDir, "rc")
	rcPath, rcContent, envKey, err := startupConfig(backend, rcDir, self)
	if err != nil {
		return err
	}
	if err := os.WriteFile(rcPath, []byte(rcContent), 0o600); err != nil {
		return err
	}
	command := exec.Command(shellPath, "-i")
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = append(os.Environ(), envKey+"="+rcDir)
	return command.Run()
}

// startupConfig renders the temporary rc. The user's own rc is sourced after
// resetting the startup-dir variable, so user config sees the same
// ZDOTDIR/XDG_CONFIG_HOME it would see without AskShell.
func startupConfig(backend, rcDir, self string) (rcPath, content, envKey string, err error) {
	switch backend {
	case "zsh":
		home := os.Getenv("ZDOTDIR")
		if home == "" {
			home = os.Getenv("HOME")
		}
		parts := []string{
			"unset ZDOTDIR",
			sourceIfExists(filepath.Join(home, ".zshrc")),
			`typeset -gx ASKSHELL_SESSION_ID="$ASKSHELL_CONTROL_SESSION_ID"`,
			"unset ASKSHELL_ZSH_INTEGRATION",
			zshIntegration(self),
		}
		return filepath.Join(rcDir, ".zshrc"), strings.Join(parts, "\n"), "ZDOTDIR", nil
	case "fish":
		home := os.Getenv("XDG_CONFIG_HOME")
		if home == "" {
			home = filepath.Join(os.Getenv("HOME"), ".config")
		}
		parts := []string{
			"set -e XDG_CONFIG_HOME",
			sourceIfExistsFish(filepath.Join(home, "fish", "config.fish")),
			`set -gx ASKSHELL_SESSION_ID "$ASKSHELL_CONTROL_SESSION_ID"`,
			"set -e ASKSHELL_FISH_INTEGRATION",
			fishIntegration(self),
		}
		if err := os.MkdirAll(filepath.Join(rcDir, "fish"), 0o700); err != nil {
			return "", "", "", err
		}
		return filepath.Join(rcDir, "fish", "config.fish"), strings.Join(parts, "\n"), "XDG_CONFIG_HOME", nil
	default:
		return "", "", "", fmt.Errorf("remote shell %q is not supported; use zsh or fish", backend)
	}
}

// sourceIfExists references the user config only when it already exists, so
// shells never see a source error on hosts without one.
func sourceIfExists(path string) string {
	quoted := quote(path)
	return fmt.Sprintf("[[ -f %s ]] && source %s", quoted, quoted)
}

func sourceIfExistsFish(path string) string {
	quoted := quote(path)
	return fmt.Sprintf("test -f %s; and source %s", quoted, quoted)
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
