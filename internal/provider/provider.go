package provider

import (
	"context"
	"fmt"

	"github.com/jimyag/askshell/internal/config"
)

type Request struct {
	Instructions     string
	Input            string
	Schema           map[string]any
	ReasoningSummary bool
}

type Result struct {
	Text             string
	ReasoningSummary string
}

type Generator interface {
	Generate(context.Context, Request) (Result, error)
}

func New(cfg config.Config) (Generator, error) {
	switch cfg.Provider {
	case config.ProviderOpenAIResponses:
		return newOpenAIResponses(cfg), nil
	case config.ProviderOpenAIChat:
		return newOpenAIChat(cfg), nil
	case config.ProviderAnthropic:
		return newAnthropic(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}
