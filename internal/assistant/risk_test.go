package assistant

import "testing"

func TestClassifyRisk(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    Risk
	}{
		{name: "remove", command: "rm file.txt", want: RiskDestructive},
		{name: "recursive remove", command: "find . -type f | xargs rm -f", want: RiskDestructive},
		{name: "overwrite redirect", command: "printf value > config", want: RiskDestructive},
		{name: "append redirect", command: "printf value >> log", want: RiskNone},
		{name: "quoted redirect", command: `printf '%s' '>'`, want: RiskNone},
		{name: "copy may overwrite", command: "cp source target", want: RiskDestructive},
		{name: "sudo", command: "sudo systemctl restart app", want: RiskPrivileged},
		{name: "plain command", command: "ps aux | head", want: RiskNone},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyRisk(test.command); got != test.want {
				t.Fatalf("ClassifyRisk(%q) = %q, want %q", test.command, got, test.want)
			}
		})
	}
}
