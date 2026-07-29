package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/jimyag/askshell/internal/provider"
)

func TestAnalyzeEnvironmentUsesCallerEnvironmentAndClassifiesRisk(t *testing.T) {
	generator := &captureGenerator{
		result: provider.Result{
			Text: `{
				"reasoning_summary": "",
				"answer": "remove the file",
				"commands": [{"command": "rm obsolete.txt", "description": "remove it"}]
			}`,
		},
	}
	service := &Service{generator: generator}

	answer, err := service.AnalyzeEnvironment(context.Background(), "clean up", Environment{
		OS:    "linux",
		Arch:  "x86_64",
		Shell: "zsh",
		CWD:   "/srv/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generator.request.Input, "OS: linux/x86_64") {
		t.Fatalf("input does not contain remote environment: %s", generator.request.Input)
	}
	if got := answer.Commands[0].Risk; got != RiskDestructive {
		t.Fatalf("risk = %q, want %q", got, RiskDestructive)
	}
}

type captureGenerator struct {
	request provider.Request
	result  provider.Result
}

func (g *captureGenerator) Generate(_ context.Context, request provider.Request) (provider.Result, error) {
	g.request = request
	return g.result, nil
}
