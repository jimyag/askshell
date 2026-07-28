package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	BackendZsh  = "zsh"
	BackendBash = "bash"
)

func ResolveBackend(requested string) (string, error) {
	if requested == "" {
		requested = filepath.Base(os.Getenv("SHELL"))
	}
	switch requested {
	case BackendZsh, BackendBash:
		return requested, nil
	default:
		return "", fmt.Errorf("unsupported shell %q; use zsh or bash", requested)
	}
}

func Command(backend string) (*exec.Cmd, error) {
	switch backend {
	case BackendZsh:
		return ZshCommand()
	case BackendBash:
		return BashCommand()
	default:
		return nil, fmt.Errorf("unsupported shell %q", backend)
	}
}

func Integration(binary, backend string) (string, error) {
	switch backend {
	case BackendZsh:
		return ZshIntegration(binary), nil
	case BackendBash:
		return BashIntegration(binary), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", backend)
	}
}
