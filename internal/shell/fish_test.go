package shell

import (
	"strings"
	"testing"
)

func TestResolveBackendSupportsFish(t *testing.T) {
	got, err := ResolveBackend("fish")
	if err != nil {
		t.Fatal(err)
	}
	if got != BackendFish {
		t.Fatalf("ResolveBackend(fish) = %q, want %q", got, BackendFish)
	}
}

func TestFishIntegrationIncludesFullModeHooks(t *testing.T) {
	integration := FishIntegration("/tmp/ask'shell")
	for _, expected := range []string{
		`set -g _ASKSHELL_BIN '/tmp/ask\'shell'`,
		"--on-event fish_preexec",
		"--on-event fish_postexec",
		"commandline -r --",
		"function fish_command_not_found",
		`"$_ASKSHELL_BIN" fix --result-dir "$result_dir"`,
		`if test $exit_code -ne 0; and test -n "$ASKSHELL_SOCKET"`,
	} {
		if !strings.Contains(integration, expected) {
			t.Fatalf("FishIntegration() does not contain %q", expected)
		}
	}
}
