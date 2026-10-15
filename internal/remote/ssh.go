package remote

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jimyag/askshell/internal/supervisor"
)

type remotePlatform struct {
	os   string
	arch string
}

func RunSSH(args []string, activeSession *supervisor.Session) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: askshell ssh [ssh options] host")
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("find ssh: %w", err)
	}

	platform, err := probe(sshPath, args)
	if err != nil {
		return err
	}
	helper, ok := helperBinary(platform)
	if !ok {
		return fmt.Errorf(
			"remote %s/%s has no prebuilt askshell helper (supported: linux/x86_64, linux/aarch64)",
			platform.os, platform.arch,
		)
	}

	remoteDir := "/tmp/askshell-" + activeSession.ID
	helperPath := remoteDir + "/askshell-remote"
	if err := deploy(sshPath, args, remoteDir, helperPath, helper); err != nil {
		return err
	}

	forward := remoteDir + "/control.sock:" + activeSession.Socket
	environment := []string{
		"ASKSHELL_SOCKET=" + remoteDir + "/control.sock",
		"ASKSHELL_SESSION_ID=" + activeSession.ID,
		"ASKSHELL_REMOTE_DIR=" + remoteDir,
	}
	if language := os.Getenv("ASKSHELL_LANG"); language != "" {
		environment = append(environment, "ASKSHELL_LANG="+language)
	}
	quoted := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		quoted = append(quoted, shellQuote(entry))
	}
	quoted = append(quoted, shellQuote(helperPath), "shell")
	remoteCommand := "env " + strings.Join(quoted, " ")

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

// probe detects the remote platform with a short BatchMode ssh. BatchMode
// keeps key/passphrase prompts out of the probe; a host needing interactive
// auth still fails later in the supervised connection with ssh's own error.
func probe(sshPath string, args []string) (remotePlatform, error) {
	sshArgs := append([]string{"-T", "-o", "BatchMode=yes"}, controlArgs(args)...)
	sshArgs = append(sshArgs, "uname -sm")
	//nolint:gosec // arguments are passed directly to OpenSSH without a local shell.
	command := exec.Command(sshPath, sshArgs...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return remotePlatform{}, fmt.Errorf("detect remote platform: %w", err)
	}
	fields := strings.Fields(output.String())
	if len(fields) < 2 {
		return remotePlatform{}, fmt.Errorf("detect remote platform: unexpected uname output %q", output.String())
	}
	return remotePlatform{
		os:   strings.ToLower(fields[0]),
		arch: strings.ToLower(fields[1]),
	}, nil
}

// controlArgs strips options that only make sense on an interactive
// connection. probe/deploy/cleanup run short non-interactive commands: a
// forced pty (-t/-tt from the user's ssh options) would process the streamed
// helper binary through the terminal line discipline and corrupt it.
func controlArgs(args []string) []string {
	cleaned := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-t", arg == "-tt", arg == "-T" || strings.HasPrefix(arg, "-tt"):
			continue
		case arg == "-o" && index+1 < len(args):
			cleaned = append(cleaned, arg, args[index+1])
			index++
		default:
			cleaned = append(cleaned, arg)
		}
	}
	return cleaned
}

const transferChunk = 768 * 1024

// deploy streams the helper binary to the remote over one ssh connection.
// `cat` is the only remote command required, so hosts without scp/sftp or
// base64 still work; the transfer is chunked so `ssh` stays responsive and
// memory stays flat.
func deploy(sshPath string, args []string, remoteDir, helperPath string, helper []byte) error {
	commandText := strings.Join([]string{
		"umask 077;",
		"mkdir -p --", shellQuote(remoteDir), "&&",
		"cat >", shellQuote(helperPath), "&&",
		"chmod 700", shellQuote(helperPath),
	}, " ")
	sshArgs := append([]string{"-T"}, controlArgs(args)...)
	sshArgs = append(sshArgs, commandText)
	//nolint:gosec // arguments are passed directly to OpenSSH without a local shell.
	command := exec.Command(sshPath, sshArgs...)
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("open deploy stream: %w", err)
	}
	command.Stdout = io.Discard
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		cleanup(sshPath, args, remoteDir)
		return fmt.Errorf("deploy temporary remote helper: %w", err)
	}
	writeErr := func() error {
		defer func() {
			_ = stdin.Close()
		}()
		for offset := 0; offset < len(helper); offset += transferChunk {
			end := min(offset+transferChunk, len(helper))
			if _, err := stdin.Write(helper[offset:end]); err != nil {
				return err
			}
		}
		return nil
	}()
	if err := command.Wait(); err != nil {
		cleanup(sshPath, args, remoteDir)
		return fmt.Errorf("deploy temporary remote helper: %w", err)
	}
	if writeErr != nil {
		cleanup(sshPath, args, remoteDir)
		return fmt.Errorf("deploy temporary remote helper: %w", writeErr)
	}
	return nil
}

func cleanup(sshPath string, args []string, remoteDir string) {
	cleanupCommand := "rm -rf -- " + shellQuote(remoteDir)
	sshArgs := append([]string{"-T"}, controlArgs(args)...)
	sshArgs = append(sshArgs, cleanupCommand)
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
