package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestHelperPreservesRemoteEnvironmentAndRiskMetadata(t *testing.T) {
	for _, expected := range []string{
		`"os": platform.system().lower()`,
		`"arch": platform.machine().lower()`,
		`if original_zdotdir is None`,
		`typeset -gx ZDOTDIR=`,
		`if original_config_home is None`,
		`set -gx XDG_CONFIG_HOME`,
		`candidate.get("risk")`,
		`_askshell_normalize_question_buffer`,
	} {
		if !strings.Contains(helperScript, expected) {
			t.Fatalf("remote helper does not contain %q", expected)
		}
	}
}

func TestRemoteZshRestoresZDOTDIRBeforeSourcingUserConfig(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	root := t.TempDir()
	userZDOTDIR := filepath.Join(root, "user-zdot")
	if err := os.Mkdir(userZDOTDIR, 0o700); err != nil {
		t.Fatal(err)
	}
	rc := `print -r -- "RESTORED_ZDOTDIR=<$ZDOTDIR>"` + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(userZDOTDIR, ".zshrc"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "askshell-remote.py")
	if err := os.WriteFile(helper, []byte(helperScript), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, helper, "shell")
	command.Env = append(
		command.Environ(),
		"SHELL="+zsh,
		"ZDOTDIR="+userZDOTDIR,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run remote helper: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "RESTORED_ZDOTDIR=<"+userZDOTDIR+">") {
		t.Fatalf("user config saw the wrong ZDOTDIR:\n%s", output)
	}
}
