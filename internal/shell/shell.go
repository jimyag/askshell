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
	BackendFish = "fish"
)

func ResolveBackend(requested string) (string, error) {
	if requested == "" {
		requested = filepath.Base(os.Getenv("SHELL"))
	}
	switch requested {
	case BackendZsh, BackendBash, BackendFish:
		return requested, nil
	default:
		return "", fmt.Errorf("unsupported shell %q; use zsh, bash, or fish", requested)
	}
}

func Command(backend string) (*exec.Cmd, error) {
	switch backend {
	case BackendZsh:
		return ZshCommand()
	case BackendBash:
		return BashCommand()
	case BackendFish:
		return FishCommand()
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
	case BackendFish:
		return FishIntegration(binary), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", backend)
	}
}
