package ui

import "testing"

func TestFailureChoiceSupportsKeyboard(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "d", want: true},
		{input: "Y", want: true},
		{input: "\r", want: true},
		{input: "n", want: false},
		{input: "\x03", want: false},
	}
	for _, test := range tests {
		got, complete := failureChoice([]byte(test.input), 20, 24)
		if !complete || got != test.want {
			t.Fatalf("failureChoice(%q) = (%v, %v), want (%v, true)", test.input, got, complete, test.want)
		}
	}
}

func TestFailureChoiceSupportsMouse(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "\x1b[<0;8;12M", want: true},
		{input: "\x1b[<0;28;12M", want: false},
	}
	for _, test := range tests {
		got, complete := failureChoice([]byte(test.input), 20, 24)
		if !complete || got != test.want {
			t.Fatalf("failureChoice(%q) = (%v, %v), want (%v, true)", test.input, got, complete, test.want)
		}
	}
}

func TestFailureChoiceIgnoresTerminalSequences(t *testing.T) {
	for _, input := range []string{"\x1b[D", "\x1bd", "\x1b[<0;8;12m"} {
		if _, complete := failureChoice([]byte(input), 20, 24); complete {
			t.Fatalf("failureChoice(%q) completed unexpectedly", input)
		}
		if !terminalSequenceComplete([]byte(input)) {
			t.Fatalf("terminalSequenceComplete(%q) = false, want true", input)
		}
	}
}

func TestDisplayWidthCountsChineseCharacters(t *testing.T) {
	if got := displayWidth("[D] 诊断并修复"); got != 14 {
		t.Fatalf("displayWidth() = %d, want 14", got)
	}
}
