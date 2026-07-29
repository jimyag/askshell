package supervisor

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/jimyag/askshell/internal/control"
)

func TestRunPreservesChildExitStatus(t *testing.T) {
	session := &Session{
		ID:     "test-session",
		server: control.NewServer("", nil, nil),
	}
	command := exec.Command("sh", "-c", "exit 7")

	err := session.Run(command)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("Run() error = %v, want *exec.ExitError", err)
	}
	if exitError.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7", exitError.ExitCode())
	}
}
