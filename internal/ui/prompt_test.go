package ui

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
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

func TestSelectActionAcceptsYesAndNo(t *testing.T) {
	tests := []struct {
		input string
		want  Action
	}{
		{input: "y\n", want: ActionExecute},
		{input: "yes\n", want: ActionExecute},
		{input: "是\n", want: ActionExecute},
		{input: "确认\n", want: ActionExecute},
		{input: "放行\n", want: ActionExecute},
		{input: "n\n", want: ActionReject},
		{input: "no\n", want: ActionReject},
		{input: "否\n", want: ActionReject},
		{input: "拒绝\n", want: ActionReject},
	}

	for _, test := range tests {
		t.Run(strings.TrimSpace(test.input), func(t *testing.T) {
			var out bytes.Buffer
			got, err := selectAction(bufioReader(test.input), &out, messagesFor("zh"))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("selectAction() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMessagesDefaultToEnglish(t *testing.T) {
	got := messagesFor("")
	if got.thinking != "AskShell is thinking…" {
		t.Fatalf("thinking = %q", got.thinking)
	}
	if got.selectAction != "Action: [y/yes] put in terminal  [n/no] reject: " {
		t.Fatalf("selectAction = %q", got.selectAction)
	}
}

func TestMessagesSupportChineseLocales(t *testing.T) {
	for _, language := range []string{"zh", "zh-CN", "zh_TW"} {
		t.Run(language, func(t *testing.T) {
			got := messagesFor(language)
			if got.thinking != "AskShell 正在思考…" {
				t.Fatalf("thinking = %q", got.thinking)
			}
			if got.selectAction != "操作：[y/是] 放入终端  [n/否] 拒绝：" {
				t.Fatalf("selectAction = %q", got.selectAction)
			}
		})
	}
}

func bufioReader(input string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(input))
}
