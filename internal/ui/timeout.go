package ui

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// pickConfirmTimeout parses ASKSHELL_TIMEOUT; empty means no timeout.
func pickConfirmTimeout(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if duration < 0 {
		duration = 0
	}
	return duration, nil
}

// readStringTimeout reads one line from reader, waiting at most timeout.
// Returns "" with nil error when the deadline passes with no input.
// reader and file must refer to the same descriptor — bufio only buffers
// one line at a time here because ask/fix/answer each use a fresh reader.
func readStringTimeout(reader *bufio.Reader, file *os.File, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", nil
		}
		chunk := max(min(remaining, time.Second), time.Millisecond)
		//nolint:gosec // file descriptors fit comfortably in int32.
		fds := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
		if _, err := unix.Poll(fds, int(chunk.Milliseconds())); err != nil {
			if err == unix.EINTR {
				continue
			}
			return "", err
		}
		if fds[0].Revents&unix.POLLIN != 0 || fds[0].Revents&unix.POLLHUP != 0 || fds[0].Revents&unix.POLLERR != 0 {
			return reader.ReadString('\n')
		}
	}
}

// pickFromEnv parses ASKSHELL_PICK-style preset selections. It returns
// (index, mode) where mode is:
//
//	-1  reject (x)
//	 0  auto    (no prefix / bare number) — treat as "place into terminal"
//	 1  edit    (e prefix)
//
// Empty or invalid values return ok=false, falling back to interactive.
func pickFromEnv(raw string, total int) (index int, mode int, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, false
	}
	mode = 0
	switch raw[0] {
	case 'y', 'Y':
		raw = raw[1:]
	case 'e', 'E':
		mode = 1
		raw = raw[1:]
	case 'x', 'X':
		mode = -1
		raw = raw[1:]
	}
	number, err := strconv.Atoi(raw)
	if err != nil || number < 1 || number > total {
		return 0, 0, false
	}
	return number - 1, mode, true
}
