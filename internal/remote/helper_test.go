package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHelperScriptIsValidPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	path := filepath.Join(t.TempDir(), "askshell-remote.py")
	if err := os.WriteFile(path, []byte(helperScript), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-m", "py_compile", path)
	command.Env = append(command.Environ(), "PYTHONPYCACHEPREFIX="+t.TempDir())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("python helper syntax: %v\n%s", err, output)
	}
}
