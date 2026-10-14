package ui

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/jimyag/askshell/internal/assistant"
)

func TestReadChoiceStripsBracketedPaste(t *testing.T) {
	reader := bufioReader("\x1b[200~拒绝\x1b[201~\n")

	got, err := readChoice(reader)
	if err != nil {
		t.Fatal(err)
	}
	if got != "拒绝" {
		t.Fatalf("readChoice() = %q, want %q", got, "拒绝")
	}
}

func TestAskSingleAcceptsEmptyAsExecute(t *testing.T) {
	// 单条候选 + 回车 = 接受。
	for _, input := range []string{"\n", "y\n", "yes\n", "是\n", "确认\n"} {
		t.Run("input-"+strings.TrimSpace(input), func(t *testing.T) {
			var out bytes.Buffer
			got, action, err := askSingle(
				bufioReader(input), strings.NewReader(input), &out,
				assistant.Command{Command: "ls /tmp"},
				messagesFor("zh"), 0,
			)
			if err != nil {
				t.Fatal(err)
			}
			if action != ActionExecute || got.Command != "ls /tmp" {
				t.Fatalf("askSingle(%q) = (%v, %q), want (execute, ls /tmp)", input, action, got.Command)
			}
		})
	}
}

func TestAskSingleEditVariant(t *testing.T) {
	var out bytes.Buffer
	_, action, err := askSingle(
		bufioReader("e\n"), strings.NewReader("e\n"), &out,
		assistant.Command{Command: "ls"}, messagesFor("zh"), 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionEdit {
		t.Fatalf("action = %q, want edit", action)
	}
}

func TestAskSingleRejectsUnknownInput(t *testing.T) {
	var out bytes.Buffer
	_, action, err := askSingle(
		bufioReader("x\n"), strings.NewReader("x\n"), &out,
		assistant.Command{Command: "ls"}, messagesFor("zh"), 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionReject {
		t.Fatalf("action = %q, want reject", action)
	}
}

func TestAskChoiceDefaultsToOne(t *testing.T) {
	commands := []assistant.Command{{Command: "a"}, {Command: "b"}}
	var out bytes.Buffer
	got, action, err := askChoice(
		bufioReader("\n"), strings.NewReader("\n"), &out, commands, messagesFor("zh"), 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionExecute || got.Command != "a" {
		t.Fatalf("askChoice(Enter) = (%q, %q), want (execute, a)", action, got.Command)
	}
}

func TestAskChoiceEditPrefix(t *testing.T) {
	commands := []assistant.Command{{Command: "a"}, {Command: "b"}}
	for _, input := range []string{"e2\n", "E2\n", "编辑2\n"} {
		t.Run(strings.TrimSpace(input), func(t *testing.T) {
			var out bytes.Buffer
			got, action, err := askChoice(
				bufioReader(input), strings.NewReader(input), &out, commands, messagesFor("zh"), 0,
			)
			if err != nil {
				t.Fatal(err)
			}
			if action != ActionEdit || got.Command != "b" {
				t.Fatalf("askChoice(%q) = (%q, %q), want (edit, b)", input, action, got.Command)
			}
		})
	}
}

func TestAskChoiceInvalidThenValid(t *testing.T) {
	// 非法输入会再次询问。
	input := "x\n2\n"
	commands := []assistant.Command{{Command: "a"}, {Command: "b"}}
	var out bytes.Buffer
	got, action, err := askChoice(
		bufioReader(input), strings.NewReader(input), &out, commands, messagesFor("zh"), 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionExecute || got.Command != "b" {
		t.Fatalf("askChoice() = (%q, %q), want (execute, b)", action, got.Command)
	}
	if !strings.Contains(out.String(), "无效选择") {
		t.Fatalf("expected invalid-selection notice, got %q", out.String())
	}
}

func TestPickFromEnv(t *testing.T) {
	tests := []struct {
		raw   string
		index int
		mode  int
		ok    bool
	}{
		{"1", 0, 0, true},
		{"2", 1, 0, true},
		{"y1", 0, 0, true},
		{"e2", 1, 1, true},
		{"E3", 2, 1, true},
		{"x1", 0, -1, true},
		{"0", 0, 0, false},
		{"99", 0, 0, false},
		{"", 0, 0, false},
		{"abc", 0, 0, false},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			index, mode, ok := pickFromEnv(test.raw, 3)
			if index != test.index || mode != test.mode || ok != test.ok {
				t.Fatalf("pickFromEnv(%q) = (%d,%d,%v), want (%d,%d,%v)",
					test.raw, index, mode, ok, test.index, test.mode, test.ok)
			}
		})
	}
}

func TestPickConfirmTimeoutParsesGoDuration(t *testing.T) {
	d, err := pickConfirmTimeout("30s")
	if err != nil {
		t.Fatal(err)
	}
	if d.Seconds() != 30 {
		t.Fatalf("got %v, want 30s", d)
	}
	if _, err := pickConfirmTimeout("xx"); err == nil {
		t.Fatal("expected error for invalid duration")
	}
	zero, err := pickConfirmTimeout("")
	if err != nil {
		t.Fatal(err)
	}
	if zero != 0 {
		t.Fatalf("empty -> %v, want 0", zero)
	}
}

func TestMessagesDefaultToEnglish(t *testing.T) {
	got := messagesFor("")
	if got.thinking != "AskShell is thinking…" {
		t.Fatalf("thinking = %q", got.thinking)
	}
}

func TestMessagesSupportChineseLocales(t *testing.T) {
	for _, language := range []string{"zh", "zh-CN", "zh_TW"} {
		t.Run(language, func(t *testing.T) {
			got := messagesFor(language)
			if got.thinking != "AskShell 正在思考…" {
				t.Fatalf("thinking = %q", got.thinking)
			}
			if !strings.Contains(got.selectCommand, "%d") {
				t.Fatalf("selectCommand missing placeholder: %q", got.selectCommand)
			}
		})
	}
}

func TestRiskTextUsesStructuredRiskLevel(t *testing.T) {
	text := messagesFor("en")
	if got := riskText(assistant.RiskDestructive, text); got != "may delete or overwrite data" {
		t.Fatalf("destructive risk = %q", got)
	}
	if got := riskText(assistant.RiskPrivileged, text); got != "requires privileges or affects system state" {
		t.Fatalf("privileged risk = %q", got)
	}
}

func bufioReader(input string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(input))
}
