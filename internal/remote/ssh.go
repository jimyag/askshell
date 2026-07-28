package remote

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jimyag/askshell/internal/supervisor"
)

func RunSSH(args []string, activeSession *supervisor.Session) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: askshell ssh [ssh options] host")
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("find ssh: %w", err)
	}

	remoteDir := "/tmp/askshell-" + activeSession.ID
	helperPath := remoteDir + "/askshell-remote"
	if err := deploy(sshPath, args, remoteDir, helperPath); err != nil {
		return err
	}

	forward := remoteDir + "/control.sock:" + activeSession.Socket
	remoteCommand := strings.Join([]string{
		"env",
		"ASKSHELL_SOCKET=" + shellQuote(remoteDir+"/control.sock"),
		"ASKSHELL_SESSION_ID=" + shellQuote(activeSession.ID),
		"ASKSHELL_REMOTE_DIR=" + shellQuote(remoteDir),
		"ASKSHELL_LANG=" + shellQuote(os.Getenv("ASKSHELL_LANG")),
		shellQuote(helperPath),
		"shell",
	}, " ")
	sshArgs := []string{
		"-tt",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StreamLocalBindUnlink=yes",
		"-R", forward,
	}
	sshArgs = append(sshArgs, args...)
	sshArgs = append(sshArgs, remoteCommand)
	//nolint:gosec // arguments are passed directly to OpenSSH without a local shell.
	command := exec.Command(sshPath, sshArgs...)
	if err := activeSession.Run(command); err != nil {
		cleanup(sshPath, args, remoteDir)
		return err
	}
	return nil
}

func deploy(sshPath string, args []string, remoteDir, helperPath string) error {
	commandText := strings.Join([]string{
		"umask 077;",
		"mkdir -p --", shellQuote(remoteDir), "&&",
		"base64 -d >", shellQuote(helperPath), "&&",
		"chmod 700", shellQuote(helperPath),
	}, " ")
	sshArgs := append(append([]string(nil), args...), commandText)
	//nolint:gosec // arguments are passed directly to OpenSSH without a local shell.
	command := exec.Command(sshPath, sshArgs...)
	command.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(helperScript)))
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		cleanup(sshPath, args, remoteDir)
		return fmt.Errorf("deploy temporary remote helper: %w", err)
	}
	return nil
}

func cleanup(sshPath string, args []string, remoteDir string) {
	cleanupCommand := "rm -rf -- " + shellQuote(remoteDir)
	sshArgs := append(append([]string(nil), args...), cleanupCommand)
	//nolint:gosec // remoteDir is generated locally and shell-quoted.
	command := exec.Command(sshPath, sshArgs...)
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	_ = command.Run()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
