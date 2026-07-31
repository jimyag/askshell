package shell

import (
	"os/exec"
	"strings"
	"testing"
)

func TestZshIntegrationNormalizesLeadingQuestionMarkPastes(t *testing.T) {
	integration := ZshIntegration("/tmp/askshell")
	for _, expected := range []string{
		"function _askshell_normalize_question_buffer",
		`BUFFER="? ${BUFFER#\?}"`,
		"autoload -Uz add-zle-hook-widget",
		"add-zle-hook-widget line-pre-redraw _askshell_normalize_question_buffer",
		`"$_ASKSHELL_BIN" fix --result-dir "$result_dir"`,
		`if (( exit_code != 0 && recorded == 0 )) && [[ -n "${ASKSHELL_SOCKET:-}" ]]; then`,
	} {
		if !strings.Contains(integration, expected) {
			t.Fatalf("ZshIntegration() does not contain %q", expected)
		}
	}
}

func TestZshQuestionNormalizerAddsSpaceWithoutReplacingPasteWidget(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	script := `
function existing_bracketed_paste() {}
zle -N bracketed-paste existing_bracketed_paste
` + ZshIntegration("/tmp/askshell") + `
BUFFER=
CURSOR=6
BUFFER='?hello'
_askshell_normalize_question_buffer
print -r -- "NORMALIZED=<$BUFFER>"
zle -lL bracketed-paste
`
	command := exec.Command(zsh, "-f", "-i", "-c", script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run zsh integration: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "NORMALIZED=<? hello>") {
		t.Fatalf("paste was not normalized:\n%s", text)
	}
	if !strings.Contains(text, "zle -N bracketed-paste existing_bracketed_paste") {
		t.Fatalf("custom bracketed-paste widget was replaced:\n%s", text)
	}
}
