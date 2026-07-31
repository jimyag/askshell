package ui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/term"
)

const (
	enableMouse  = "\x1b[?1000h\x1b[?1006h"
	disableMouse = "\x1b[?1000l\x1b[?1006l"
)

func ConfirmFailure(tty *os.File, language string) (bool, error) {
	chinese := isChinese(language)
	diagnose := "Diagnose and fix"
	ignore := "Ignore"
	if chinese {
		diagnose = "诊断并修复"
		ignore = "忽略"
	}
	line := fmt.Sprintf("[D] %s    [N] %s", diagnose, ignore)
	if _, err := fmt.Fprintf(tty, "\nAskShell: %s\n%s", failureMessage(chinese), line); err != nil {
		return false, err
	}

	state, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		if _, writeErr := fmt.Fprint(tty, ": "); writeErr != nil {
			return false, writeErr
		}
		return readFailureLine(tty)
	}
	defer func() {
		_, _ = fmt.Fprint(tty, disableMouse+"\r\n")
		_ = term.Restore(int(tty.Fd()), state)
	}()
	if _, err := fmt.Fprint(tty, enableMouse); err != nil {
		return false, err
	}

	diagnoseEnd := displayWidth("[D] "+diagnose) + 1
	ignoreStart := diagnoseEnd + displayWidth("   ")
	var input []byte
	buffer := make([]byte, 1)
	for {
		count, readErr := tty.Read(buffer)
		if count > 0 {
			input = append(input, buffer[0])
			if choice, ok := failureChoice(input, diagnoseEnd, ignoreStart); ok {
				return choice, nil
			}
			if len(input) > 64 {
				input = input[len(input)-64:]
			}
		}
		if readErr != nil {
			return false, readErr
		}
	}
}

func failureMessage(chinese bool) string {
	if chinese {
		return "上一条命令执行失败。"
	}
	return "The previous command failed."
}

func readFailureLine(reader io.Reader) (bool, error) {
	var value string
	if _, err := fmt.Fscanln(reader, &value); err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "d", "y", "yes", "是", "确认":
		return true, nil
	default:
		return false, nil
	}
}

func failureChoice(input []byte, diagnoseEnd, ignoreStart int) (bool, bool) {
	if len(input) == 0 {
		return false, false
	}
	switch input[len(input)-1] {
	case '\r', '\n', 'd', 'D', 'y', 'Y':
		return true, true
	case 'n', 'N', 3:
		return false, true
	}

	value := string(input)
	start := strings.LastIndex(value, "\x1b[<")
	if start < 0 {
		return false, false
	}
	end := strings.IndexAny(value[start:], "Mm")
	if end < 0 {
		return false, false
	}
	parts := strings.Split(value[start+3:start+end], ";")
	if len(parts) != 3 || parts[0] != "0" || value[start+end] != 'M' {
		return false, true
	}
	column, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, true
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
